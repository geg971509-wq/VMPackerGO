package elf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

func applyRewritePlan(input []byte, plan *RewritePlan) ([]byte, error) {
	finalSize, err := validateRewritePlanApplication(input, plan)
	if err != nil {
		return nil, err
	}

	artifact := make([]byte, int(finalSize))
	copy(artifact, input)
	for _, segment := range plan.segments {
		copy(artifact[segment.fileOffset:segment.fileOffset+segment.fileSize], segment.data)
	}
	phdr := plan.programHeaders
	copy(artifact[phdr.phoffAfter:phdr.phoffAfter+uint64(len(phdr.tableData))], phdr.tableData)
	binary.LittleEndian.PutUint64(artifact[32:40], phdr.phoffAfter)
	binary.LittleEndian.PutUint16(artifact[56:58], phdr.phnumAfter)
	for _, function := range plan.functions {
		copy(artifact[function.entryFileOffset:function.entryFileOffset+uint64(len(function.entryPatch))], function.entryPatch)
	}
	return artifact, nil
}

func validateRewritePlanApplication(input []byte, plan *RewritePlan) (uint64, error) {
	if plan == nil {
		return 0, fmt.Errorf("rewrite plan is required")
	}
	if len(input) < elf64HeaderSize {
		return 0, fmt.Errorf("ELF header is truncated")
	}

	bo := binary.LittleEndian
	phdr := plan.programHeaders
	if bo.Uint16(input[54:56]) != elf64ProgramSize {
		return 0, fmt.Errorf("input program-header entry size changed after planning")
	}
	if got := bo.Uint64(input[32:40]); got != phdr.phoffBefore {
		return 0, fmt.Errorf("input program-header offset changed after planning")
	}
	if got := bo.Uint16(input[56:58]); got != phdr.phnumBefore {
		return 0, fmt.Errorf("input program-header count changed after planning")
	}
	if phdr.phnumAfter == 0 || phdr.phnumAfter < phdr.phnumBefore {
		return 0, fmt.Errorf("planned program-header count is invalid")
	}
	if phdr.phoffBefore < elf64HeaderSize || phdr.phoffAfter < elf64HeaderSize {
		return 0, fmt.Errorf("planned program-header table overlaps the ELF header")
	}

	beforeSize, ok := checkedMul(uint64(phdr.phnumBefore), elf64ProgramSize)
	if !ok {
		return 0, fmt.Errorf("input program-header table size overflows")
	}
	beforeEnd, ok := checkedAdd(phdr.phoffBefore, beforeSize)
	if !ok || beforeEnd > uint64(len(input)) {
		return 0, fmt.Errorf("input program-header table is out of bounds")
	}
	wantTableSize, ok := checkedMul(uint64(phdr.phnumAfter), elf64ProgramSize)
	if !ok || wantTableSize != uint64(len(phdr.tableData)) {
		return 0, fmt.Errorf("planned program-header table length is inconsistent")
	}
	phdrEnd, ok := checkedAdd(phdr.phoffAfter, wantTableSize)
	if !ok || phdrEnd > uint64(math.MaxInt) {
		return 0, fmt.Errorf("planned program-header table range exceeds the addressable buffer")
	}
	if phdr.relocated {
		if phdr.phoffAfter == phdr.phoffBefore {
			return 0, fmt.Errorf("relocated program-header table kept its original offset")
		}
	} else if phdr.phoffAfter != phdr.phoffBefore {
		return 0, fmt.Errorf("non-relocated program-header table changed offset")
	}

	shoff := bo.Uint64(input[40:48])
	shentsize := uint64(bo.Uint16(input[58:60]))
	shnum := uint64(bo.Uint16(input[60:62]))
	var shend uint64
	if shnum != 0 {
		sectionTableSize, ok := checkedMul(shentsize, shnum)
		if !ok {
			return 0, fmt.Errorf("section-header table size overflows")
		}
		shend, ok = checkedAdd(shoff, sectionTableSize)
		if !ok || shend > uint64(len(input)) {
			return 0, fmt.Errorf("section-header table is out of bounds")
		}
		if phdr.phoffAfter < shend && shoff < phdrEnd {
			return 0, fmt.Errorf("planned program-header table overlaps section headers")
		}
	}

	finalSize := uint64(len(input))
	previousEnd := finalSize
	for i, segment := range plan.segments {
		if segment.fileSize == 0 || segment.fileSize != uint64(len(segment.data)) || segment.memSize != segment.fileSize {
			return 0, fmt.Errorf("planned segment %d has inconsistent sizes", i)
		}
		if segment.fileOffset < previousEnd {
			return 0, fmt.Errorf("planned segment %d overlaps input or a previous segment", i)
		}
		end, ok := checkedAdd(segment.fileOffset, segment.fileSize)
		if !ok || end > uint64(math.MaxInt) {
			return 0, fmt.Errorf("planned segment %d range exceeds the addressable buffer", i)
		}
		previousEnd = end
		if end > finalSize {
			finalSize = end
		}
	}

	if phdr.relocated {
		containingSegments := 0
		for i, segment := range plan.segments {
			segmentEnd := segment.fileOffset + segment.fileSize
			if phdr.phoffAfter < segment.fileOffset || phdrEnd > segmentEnd {
				continue
			}
			containingSegments++
			offset := phdr.phoffAfter - segment.fileOffset
			if !bytes.Equal(segment.data[offset:offset+wantTableSize], phdr.tableData) {
				return 0, fmt.Errorf("planned segment %d disagrees with relocated program-header bytes", i)
			}
		}
		if containingSegments != 1 {
			return 0, fmt.Errorf("relocated program-header table is not covered by exactly one planned segment")
		}
	} else if phdrEnd > uint64(len(input)) {
		return 0, fmt.Errorf("in-place program-header table exceeds the original input")
	}
	if phdrEnd > finalSize {
		finalSize = phdrEnd
	}

	for i, function := range plan.functions {
		if len(function.entryPatch) == 0 {
			return 0, fmt.Errorf("function entry patch %d is empty", i)
		}
		end, ok := checkedAdd(function.entryFileOffset, uint64(len(function.entryPatch)))
		if !ok || end > uint64(len(input)) {
			return 0, fmt.Errorf("function entry patch %d exceeds the original input", i)
		}
		if function.entryFileOffset < elf64HeaderSize || function.entryFileOffset < beforeEnd && phdr.phoffBefore < end ||
			function.entryFileOffset < phdrEnd && phdr.phoffAfter < end || shnum != 0 && function.entryFileOffset < shend && shoff < end {
			return 0, fmt.Errorf("function entry patch %d overlaps ELF metadata", i)
		}
	}
	return finalSize, nil
}
