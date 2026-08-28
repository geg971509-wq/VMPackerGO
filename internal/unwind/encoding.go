package unwind

import (
	"encoding/binary"
	"fmt"
)

const (
	PEAbsptr   = 0x00
	PEUleb128  = 0x01
	PEUdata2   = 0x02
	PEUdata4   = 0x03
	PEUdata8   = 0x04
	PESleb128  = 0x09
	PESdata2   = 0x0a
	PESdata4   = 0x0b
	PESdata8   = 0x0c
	PEPcrel    = 0x10
	PETextrel  = 0x20
	PEDatarel  = 0x30
	PEFuncrel  = 0x40
	PEAligned  = 0x50
	PEIndirect = 0x80
	PEOmit     = 0xff
)

type Bases struct {
	Field    uint64
	Text     uint64
	Data     uint64
	Function uint64
}

func DecodePointer(data []byte, offset *int, encoding byte, order binary.ByteOrder, pointerSize int, bases Bases) (uint64, error) {
	if encoding == PEOmit {
		return 0, fmt.Errorf("omitted pointer has no value")
	}
	if encoding&PEIndirect != 0 {
		return 0, fmt.Errorf("indirect DW_EH_PE pointer requires target-memory access")
	}
	if encoding&0x70 == PEAligned {
		if pointerSize != 4 && pointerSize != 8 {
			return 0, fmt.Errorf("unsupported pointer size %d", pointerSize)
		}
		aligned := (*offset + pointerSize - 1) &^ (pointerSize - 1)
		if aligned < *offset || aligned > len(data) {
			return 0, fmt.Errorf("aligned pointer exceeds input")
		}
		*offset = aligned
		encoding = encoding&0x8f | PEAbsptr
	}
	start := *offset
	unsigned, signed, isSigned, err := decodeScalar(data, offset, encoding&0x0f, order, pointerSize)
	if err != nil {
		return 0, err
	}
	var value uint64
	if isSigned {
		value = uint64(signed)
	} else {
		value = unsigned
	}
	var base uint64
	switch encoding & 0x70 {
	case 0:
	case PEPcrel:
		base = bases.Field + uint64(start)
	case PETextrel:
		base = bases.Text
	case PEDatarel:
		base = bases.Data
	case PEFuncrel:
		base = bases.Function
	default:
		return 0, fmt.Errorf("unsupported DW_EH_PE application 0x%x", encoding&0x70)
	}
	result := base + value
	if base != 0 && result < base && (!isSigned || signed >= 0) {
		return 0, fmt.Errorf("DW_EH_PE pointer overflows")
	}
	return result, nil
}

func decodeScalar(data []byte, offset *int, format byte, order binary.ByteOrder, pointerSize int) (uint64, int64, bool, error) {
	read := func(size int) ([]byte, error) {
		if *offset < 0 || size < 0 || *offset > len(data)-size {
			return nil, fmt.Errorf("encoded pointer is truncated")
		}
		value := data[*offset : *offset+size]
		*offset += size
		return value, nil
	}
	switch format {
	case PEAbsptr:
		value, err := read(pointerSize)
		if err != nil {
			return 0, 0, false, err
		}
		if pointerSize == 4 {
			return uint64(order.Uint32(value)), 0, false, nil
		}
		if pointerSize == 8 {
			return order.Uint64(value), 0, false, nil
		}
	case PEUleb128:
		value, err := readULEB(data, offset)
		return value, 0, false, err
	case PESleb128:
		value, err := readSLEB(data, offset)
		return 0, value, true, err
	case PEUdata2, PEUdata4, PEUdata8, PESdata2, PESdata4, PESdata8:
		size := 1 << ((format & 7) - 1)
		value, err := read(size)
		if err != nil {
			return 0, 0, false, err
		}
		var unsigned uint64
		switch size {
		case 2:
			unsigned = uint64(order.Uint16(value))
		case 4:
			unsigned = uint64(order.Uint32(value))
		case 8:
			unsigned = order.Uint64(value)
		}
		if format&8 == 0 {
			return unsigned, 0, false, nil
		}
		shift := 64 - size*8
		return 0, int64(unsigned<<shift) >> shift, true, nil
	}
	return 0, 0, false, fmt.Errorf("unsupported DW_EH_PE format 0x%x", format)
}

func readULEB(data []byte, offset *int) (uint64, error) {
	var result uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if *offset >= len(data) {
			return 0, fmt.Errorf("truncated ULEB128")
		}
		value := data[*offset]
		*offset++
		if shift == 63 && value > 1 {
			return 0, fmt.Errorf("ULEB128 overflows")
		}
		result |= uint64(value&0x7f) << shift
		if value&0x80 == 0 {
			return result, nil
		}
	}
	return 0, fmt.Errorf("ULEB128 overflows")
}

func readSLEB(data []byte, offset *int) (int64, error) {
	var result uint64
	var value byte
	shift := uint(0)
	for ; shift < 64; shift += 7 {
		if *offset >= len(data) {
			return 0, fmt.Errorf("truncated SLEB128")
		}
		value = data[*offset]
		*offset++
		result |= uint64(value&0x7f) << shift
		if value&0x80 == 0 {
			shift += 7
			if shift < 64 && value&0x40 != 0 {
				result |= ^uint64(0) << shift
			}
			return int64(result), nil
		}
	}
	return 0, fmt.Errorf("SLEB128 overflows")
}
