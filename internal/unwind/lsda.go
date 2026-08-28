package unwind

import (
	"encoding/binary"
	"fmt"
)

type CallSite struct {
	Start      uint64
	Length     uint64
	LandingPad uint64
	Action     uint64
}

type LSDA struct {
	LPStart          uint64
	TypeTableBase    *uint64
	TypeEncoding     byte
	CallSiteEncoding byte
	CallSites        []CallSite
	ActionChains     map[uint64][]ActionRecord
	TypeInfos        map[uint64]TypeInfo
	ActionTable      []byte
	TypeIndexTable   []byte
}

type ActionRecord struct {
	Offset      uint64
	TypeFilter  int64
	Next        int64
	FilterTypes []uint64
}

type TypeInfo struct {
	Index    uint64
	Address  uint64
	Indirect bool
}

func ParseLSDA(data []byte, address, function uint64, order binary.ByteOrder, pointerSize int) (*LSDA, error) {
	offset := 0
	if len(data) < 3 {
		return nil, fmt.Errorf("LSDA header is truncated")
	}
	lsda := &LSDA{LPStart: function, TypeEncoding: PEOmit, ActionChains: map[uint64][]ActionRecord{}, TypeInfos: map[uint64]TypeInfo{}}
	lpEncoding := data[offset]
	offset++
	if lpEncoding != PEOmit {
		value, err := DecodePointer(data, &offset, lpEncoding, order, pointerSize, Bases{Field: address, Function: function})
		if err != nil {
			return nil, err
		}
		lsda.LPStart = value
	}
	typeEncoding := data[offset]
	offset++
	lsda.TypeEncoding = typeEncoding
	typeTableOffset := -1
	if typeEncoding != PEOmit {
		typeOffset, err := readULEB(data, &offset)
		if err != nil || typeOffset > uint64(len(data)-offset) {
			return nil, fmt.Errorf("invalid LSDA type-table offset")
		}
		if typeOffset > uint64(len(data)-offset) {
			return nil, fmt.Errorf("LSDA type table exceeds input")
		}
		typeTableOffset = offset + int(typeOffset)
		value := address + uint64(typeTableOffset)
		lsda.TypeTableBase = &value
	}
	if offset >= len(data) {
		return nil, fmt.Errorf("missing LSDA call-site encoding")
	}
	callEncoding := data[offset]
	offset++
	lsda.CallSiteEncoding = callEncoding
	if callEncoding == PEOmit || callEncoding&0x70 != 0 {
		return nil, fmt.Errorf("unsupported LSDA call-site encoding 0x%x", callEncoding)
	}
	callLength, err := readULEB(data, &offset)
	if err != nil || callLength > uint64(len(data)-offset) {
		return nil, fmt.Errorf("invalid LSDA call-site table length")
	}
	end := offset + int(callLength)
	actionTableStart := end
	for offset < end {
		start, err := DecodePointer(data[:end], &offset, callEncoding, order, pointerSize, Bases{})
		if err != nil {
			return nil, err
		}
		length, err := DecodePointer(data[:end], &offset, callEncoding&0x0f, order, pointerSize, Bases{})
		if err != nil {
			return nil, err
		}
		landing, err := DecodePointer(data[:end], &offset, callEncoding&0x0f, order, pointerSize, Bases{})
		if err != nil {
			return nil, err
		}
		action, err := readULEB(data[:end], &offset)
		if err != nil {
			return nil, err
		}
		lsda.CallSites = append(lsda.CallSites, CallSite{Start: lsda.LPStart + start, Length: length, LandingPad: func() uint64 {
			if landing == 0 {
				return 0
			}
			return lsda.LPStart + landing
		}(), Action: action})
	}
	if offset != end {
		return nil, fmt.Errorf("LSDA call-site table is misaligned")
	}
	if err := parseLSDAMetadata(data, address, function, typeTableOffset, actionTableStart, lsda, order, pointerSize); err != nil {
		return nil, err
	}
	return lsda, nil
}

func parseLSDAMetadata(data []byte, address, function uint64, typeTableOffset, actionTableStart int, lsda *LSDA, order binary.ByteOrder, pointerSize int) error {
	maxTypeIndex := uint64(0)
	maxFilterEnd := typeTableOffset
	maxActionEnd := actionTableStart
	for _, site := range lsda.CallSites {
		if site.Action == 0 {
			continue
		}
		if _, ok := lsda.ActionChains[site.Action]; ok {
			continue
		}
		ptr := actionTableStart + int(site.Action) - 1
		visited := map[int]bool{}
		for {
			if ptr < actionTableStart || ptr >= len(data) || visited[ptr] {
				return fmt.Errorf("LSDA action chain 0x%x is out of range or cyclic", site.Action)
			}
			visited[ptr] = true
			recordOffset := uint64(ptr - actionTableStart + 1)
			filter, err := readSLEB(data, &ptr)
			if err != nil {
				return fmt.Errorf("decode LSDA action 0x%x filter: %w", recordOffset, err)
			}
			self := ptr
			next, err := readSLEB(data, &ptr)
			if err != nil {
				return fmt.Errorf("decode LSDA action 0x%x next: %w", recordOffset, err)
			}
			if ptr > maxActionEnd {
				maxActionEnd = ptr
			}
			record := ActionRecord{Offset: recordOffset, TypeFilter: filter, Next: next}
			switch {
			case filter > 0:
				if uint64(filter) > maxTypeIndex {
					maxTypeIndex = uint64(filter)
				}
			case filter < 0:
				if typeTableOffset < 0 {
					return fmt.Errorf("LSDA filter action has no type table")
				}
				filterPtr := typeTableOffset - int(filter) - 1
				for {
					index, err := readULEB(data, &filterPtr)
					if err != nil {
						return fmt.Errorf("decode LSDA filter action 0x%x: %w", recordOffset, err)
					}
					if index == 0 {
						break
					}
					record.FilterTypes = append(record.FilterTypes, index)
					if index > maxTypeIndex {
						maxTypeIndex = index
					}
				}
				if filterPtr > maxFilterEnd {
					maxFilterEnd = filterPtr
				}
			}
			lsda.ActionChains[site.Action] = append(lsda.ActionChains[site.Action], record)
			if next == 0 {
				break
			}
			nextPtr := int64(self) + next
			if nextPtr < int64(actionTableStart) || nextPtr >= int64(len(data)) {
				return fmt.Errorf("LSDA action 0x%x next offset is out of range", recordOffset)
			}
			ptr = int(nextPtr)
		}
	}

	if maxTypeIndex == 0 {
		if maxActionEnd > len(data) {
			return fmt.Errorf("LSDA action table exceeds input")
		}
		lsda.ActionTable = append([]byte(nil), data[actionTableStart:maxActionEnd]...)
		return nil
	}
	if typeTableOffset < 0 {
		return fmt.Errorf("LSDA catch actions have no type table")
	}
	typeSize, err := fixedEncodingSize(lsda.TypeEncoding, pointerSize)
	if err != nil {
		return err
	}
	typeTableStart := typeTableOffset - int(maxTypeIndex)*typeSize
	if typeTableStart < maxActionEnd || typeTableStart < 0 {
		return fmt.Errorf("LSDA action and type tables overlap")
	}
	lsda.ActionTable = append([]byte(nil), data[actionTableStart:typeTableStart]...)
	for index := uint64(1); index <= maxTypeIndex; index++ {
		entry := typeTableOffset - int(index)*typeSize
		entryEnd := entry + typeSize
		if entry < 0 || entryEnd > len(data) {
			return fmt.Errorf("LSDA type index %d exceeds input", index)
		}
		allZero := true
		for _, b := range data[entry:entryEnd] {
			allZero = allZero && b == 0
		}
		value := uint64(0)
		if !allZero {
			decodeOffset := entry
			value, err = DecodePointer(data, &decodeOffset, lsda.TypeEncoding&^PEIndirect, order, pointerSize, Bases{Field: address, Function: function})
			if err != nil || decodeOffset != entryEnd {
				return fmt.Errorf("decode LSDA type index %d: %w", index, err)
			}
		}
		lsda.TypeInfos[index] = TypeInfo{Index: index, Address: value, Indirect: lsda.TypeEncoding&PEIndirect != 0}
	}
	if maxFilterEnd > typeTableOffset {
		if maxFilterEnd > len(data) {
			return fmt.Errorf("LSDA type-index table exceeds input")
		}
		lsda.TypeIndexTable = append([]byte(nil), data[typeTableOffset:maxFilterEnd]...)
	}
	return nil
}

func fixedEncodingSize(encoding byte, pointerSize int) (int, error) {
	switch encoding & 0x0f {
	case PEAbsptr:
		if pointerSize == 4 || pointerSize == 8 {
			return pointerSize, nil
		}
	case PEUdata2, PESdata2:
		return 2, nil
	case PEUdata4, PESdata4:
		return 4, nil
	case PEUdata8, PESdata8:
		return 8, nil
	case PEUleb128, PESleb128:
		return 0, fmt.Errorf("variable-width LSDA type encoding 0x%x is unsupported", encoding)
	}
	return 0, fmt.Errorf("unsupported LSDA type encoding 0x%x", encoding)
}

type MappedCallSite struct {
	VMStart      uint32
	VMLength     uint32
	VMLandingPad uint32
	Action       uint64
}

func MapCallSites(lsda *LSDA, mapPC func(uint64) (uint32, bool)) ([]MappedCallSite, error) {
	if lsda == nil || mapPC == nil {
		return nil, fmt.Errorf("LSDA and PC mapper are required")
	}
	result := make([]MappedCallSite, 0, len(lsda.CallSites))
	for _, site := range lsda.CallSites {
		start, ok := mapPC(site.Start)
		if !ok {
			return nil, fmt.Errorf("call-site PC 0x%x has no VM mapping", site.Start)
		}
		end, ok := mapPC(site.Start + site.Length)
		if !ok || end < start {
			return nil, fmt.Errorf("call-site end 0x%x has no ordered VM mapping", site.Start+site.Length)
		}
		var landing uint32
		if site.LandingPad != 0 {
			landing, ok = mapPC(site.LandingPad)
			if !ok {
				return nil, fmt.Errorf("landing pad 0x%x has no VM mapping", site.LandingPad)
			}
		}
		result = append(result, MappedCallSite{VMStart: start, VMLength: end - start, VMLandingPad: landing, Action: site.Action})
	}
	return result, nil
}
