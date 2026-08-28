package unwind

import (
	"encoding/binary"
	"fmt"
)

type CIE struct {
	Offset              uint64
	Version             byte
	Augmentation        string
	CodeAlignment       uint64
	DataAlignment       int64
	ReturnRegister      uint64
	FDEEncoding         byte
	LSDAEncoding        byte
	PersonalityEncoding byte
	Personality         *uint64
	Instructions        []byte
}

type FDE struct {
	Offset          uint64
	CIEOffset       uint64
	InitialLocation uint64
	AddressRange    uint64
	LSDA            *uint64
	Instructions    []byte
}

type Frame struct {
	CIEs map[uint64]*CIE
	FDEs []FDE
}

func ParseEHFrame(data []byte, sectionVA uint64, order binary.ByteOrder, pointerSize int) (*Frame, error) {
	frame := &Frame{CIEs: map[uint64]*CIE{}}
	for offset := 0; offset < len(data); {
		entryOffset := offset
		length, headerSize, err := frameLength(data, offset, order)
		if err != nil {
			return nil, err
		}
		if length == 0 {
			break
		}
		bodyStart := offset + headerSize
		if length > uint64(len(data)-bodyStart) {
			return nil, fmt.Errorf(".eh_frame entry at 0x%x is truncated", entryOffset)
		}
		entryEnd := bodyStart + int(length)
		idSize := 4
		if headerSize == 12 {
			idSize = 8
		}
		if bodyStart > entryEnd-idSize {
			return nil, fmt.Errorf(".eh_frame entry at 0x%x has no CIE identifier", entryOffset)
		}
		id := uint64(order.Uint32(data[bodyStart:]))
		if idSize == 8 {
			id = order.Uint64(data[bodyStart:])
		}
		contentStart := bodyStart + idSize
		if id == 0 {
			cie, err := parseCIE(data[contentStart:entryEnd], uint64(entryOffset), sectionVA+uint64(contentStart), order, pointerSize)
			if err != nil {
				return nil, fmt.Errorf("parse CIE at 0x%x: %w", entryOffset, err)
			}
			frame.CIEs[uint64(entryOffset)] = cie
		} else {
			idField := uint64(bodyStart)
			if id > idField {
				return nil, fmt.Errorf("FDE at 0x%x has an invalid CIE back-reference", entryOffset)
			}
			cieOffset := idField - id
			cie := frame.CIEs[cieOffset]
			if cie == nil {
				return nil, fmt.Errorf("FDE at 0x%x references missing CIE 0x%x", entryOffset, cieOffset)
			}
			fde, err := parseFDE(data[contentStart:entryEnd], uint64(entryOffset), cieOffset, sectionVA+uint64(contentStart), cie, order, pointerSize)
			if err != nil {
				return nil, fmt.Errorf("parse FDE at 0x%x: %w", entryOffset, err)
			}
			frame.FDEs = append(frame.FDEs, *fde)
		}
		offset = entryEnd
	}
	return frame, nil
}

func frameLength(data []byte, offset int, order binary.ByteOrder) (uint64, int, error) {
	if offset > len(data)-4 {
		return 0, 0, fmt.Errorf("truncated .eh_frame length")
	}
	short := order.Uint32(data[offset:])
	if short != 0xffffffff {
		return uint64(short), 4, nil
	}
	if offset > len(data)-12 {
		return 0, 0, fmt.Errorf("truncated extended .eh_frame length")
	}
	return order.Uint64(data[offset+4:]), 12, nil
}

func parseCIE(data []byte, entryOffset, fieldVA uint64, order binary.ByteOrder, pointerSize int) (*CIE, error) {
	offset := 0
	if len(data) == 0 {
		return nil, fmt.Errorf("empty CIE")
	}
	cie := &CIE{Offset: entryOffset, Version: data[offset], FDEEncoding: PEAbsptr, LSDAEncoding: PEOmit, PersonalityEncoding: PEOmit}
	offset++
	augmentation, err := readCString(data, &offset)
	if err != nil {
		return nil, err
	}
	cie.Augmentation = augmentation
	if cie.CodeAlignment, err = readULEB(data, &offset); err != nil {
		return nil, err
	}
	if cie.DataAlignment, err = readSLEB(data, &offset); err != nil {
		return nil, err
	}
	if cie.Version == 1 {
		if offset >= len(data) {
			return nil, fmt.Errorf("missing return register")
		}
		cie.ReturnRegister = uint64(data[offset])
		offset++
	} else if cie.ReturnRegister, err = readULEB(data, &offset); err != nil {
		return nil, err
	}
	if len(augmentation) > 0 && augmentation[0] == 'z' {
		augmentationLength, err := readULEB(data, &offset)
		if err != nil || augmentationLength > uint64(len(data)-offset) {
			return nil, fmt.Errorf("invalid CIE augmentation length")
		}
		augmentationEnd := offset + int(augmentationLength)
		for _, code := range augmentation[1:] {
			switch code {
			case 'R':
				if offset >= augmentationEnd {
					return nil, fmt.Errorf("missing FDE encoding")
				}
				cie.FDEEncoding = data[offset]
				offset++
			case 'L':
				if offset >= augmentationEnd {
					return nil, fmt.Errorf("missing LSDA encoding")
				}
				cie.LSDAEncoding = data[offset]
				offset++
			case 'P':
				if offset >= augmentationEnd {
					return nil, fmt.Errorf("missing personality encoding")
				}
				cie.PersonalityEncoding = data[offset]
				offset++
				value, err := DecodePointer(data[:augmentationEnd], &offset, cie.PersonalityEncoding, order, pointerSize, Bases{Field: fieldVA})
				if err != nil {
					return nil, err
				}
				cie.Personality = &value
			default:
				return nil, fmt.Errorf("unsupported CIE augmentation %q", code)
			}
		}
		if offset > augmentationEnd {
			return nil, fmt.Errorf("CIE augmentation overrun")
		}
		offset = augmentationEnd
	} else if augmentation != "" {
		return nil, fmt.Errorf("unsupported non-z augmentation %q", augmentation)
	}
	cie.Instructions = append([]byte(nil), data[offset:]...)
	return cie, nil
}

func parseFDE(data []byte, entryOffset, cieOffset, fieldVA uint64, cie *CIE, order binary.ByteOrder, pointerSize int) (*FDE, error) {
	offset := 0
	initial, err := DecodePointer(data, &offset, cie.FDEEncoding, order, pointerSize, Bases{Field: fieldVA})
	if err != nil {
		return nil, err
	}
	rangeEncoding := cie.FDEEncoding & 0x0f
	addressRange, err := DecodePointer(data, &offset, rangeEncoding, order, pointerSize, Bases{})
	if err != nil {
		return nil, err
	}
	fde := &FDE{Offset: entryOffset, CIEOffset: cieOffset, InitialLocation: initial, AddressRange: addressRange}
	if len(cie.Augmentation) > 0 && cie.Augmentation[0] == 'z' {
		length, err := readULEB(data, &offset)
		if err != nil || length > uint64(len(data)-offset) {
			return nil, fmt.Errorf("invalid FDE augmentation length")
		}
		end := offset + int(length)
		if cie.LSDAEncoding != PEOmit {
			value, err := DecodePointer(data[:end], &offset, cie.LSDAEncoding, order, pointerSize, Bases{Field: fieldVA, Function: initial})
			if err != nil {
				return nil, err
			}
			fde.LSDA = &value
		}
		if offset > end {
			return nil, fmt.Errorf("FDE augmentation overrun")
		}
		offset = end
	}
	fde.Instructions = append([]byte(nil), data[offset:]...)
	return fde, nil
}

func readCString(data []byte, offset *int) (string, error) {
	start := *offset
	for *offset < len(data) && data[*offset] != 0 {
		*offset++
	}
	if *offset >= len(data) {
		return "", fmt.Errorf("unterminated augmentation string")
	}
	value := string(data[start:*offset])
	*offset++
	return value, nil
}
