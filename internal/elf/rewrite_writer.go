package elf

import (
	"bytes"
	stdelf "debug/elf"
	"encoding/binary"
	"fmt"
	"math"
)

func applyRewritePlan(input []byte, plan *RewritePlan) ([]byte, error) {
	finalSize, err := validateRewritePlanApplication(input, plan)
	if err != nil {
		return nil, err
	}
	tableData, err := materializeProgramHeaderTable(plan)
	if err != nil {
		return nil, err
	}

	artifact := make([]byte, int(finalSize))
	copy(artifact, input)
	for _, segment := range plan.segments {
		copy(artifact[segment.fileOffset:segment.fileOffset+segment.fileSize], segment.data)
	}
	phdr := plan.programHeaders
	copy(artifact[phdr.phoffAfter:phdr.phoffAfter+uint64(len(tableData))], tableData)
	binary.LittleEndian.PutUint64(artifact[32:40], phdr.phoffAfter)
	binary.LittleEndian.PutUint16(artifact[56:58], phdr.phnumAfter)
	for _, function := range plan.functions {
		copy(artifact[function.entryFileOffset:function.entryFileOffset+uint64(len(function.entryPatch))], function.entryPatch)
	}
	return artifact, nil
}

// materializeProgramHeaderTable serializes the already-approved RewritePlan
// into a fresh table. In particular, added PT_LOAD entries are synchronized
// from the final segment extents because the read-only runtime segment may
// have grown after initial placement to contain relocated PHDR bytes.
func materializeProgramHeaderTable(plan *RewritePlan) ([]byte, error) {
	if plan == nil {
		return nil, fmt.Errorf("rewrite plan is required")
	}
	phdr := plan.programHeaders
	table := append([]byte(nil), phdr.tableData...)
	if len(phdr.newLoads) != len(plan.segments) {
		return nil, fmt.Errorf("planned PT_LOAD count does not match runtime segments")
	}
	for i, mutation := range phdr.newLoads {
		if mutation.index < 0 || mutation.index >= int(phdr.phnumAfter) {
			return nil, fmt.Errorf("planned PT_LOAD index %d is outside program-header table", mutation.index)
		}
		segment := plan.segments[i]
		header := mutation.header
		if header.type_ != stdelf.PT_LOAD || header.flags != segment.flags {
			return nil, fmt.Errorf("planned PT_LOAD %d does not match runtime segment %d", mutation.index, i)
		}
		header.off = segment.fileOffset
		header.vaddr = segment.vaddr
		header.paddr = segment.vaddr
		header.filesz = segment.fileSize
		header.memsz = segment.memSize
		header.align = rewriteLoadAlignment
		if err := encodePlannedProgramHeaderAt(table, mutation.index, header); err != nil {
			return nil, err
		}
	}
	if phdr.phdrUpdate != nil {
		if err := encodePlannedProgramHeaderAt(table, phdr.phdrUpdate.index, phdr.phdrUpdate.header); err != nil {
			return nil, err
		}
	}
	if phdr.gnuEHFrameUpdate != nil {
		if err := encodePlannedProgramHeaderAt(table, phdr.gnuEHFrameUpdate.index, phdr.gnuEHFrameUpdate.header); err != nil {
			return nil, err
		}
	}
	return table, nil
}

func encodePlannedProgramHeaderAt(table []byte, index int, program plannedProgramHeader) error {
	if index < 0 {
		return fmt.Errorf("negative program-header index")
	}
	off, ok := checkedMul(uint64(index), elf64ProgramSize)
	if !ok || off > uint64(len(table)) || elf64ProgramSize > uint64(len(table))-off {
		return fmt.Errorf("program-header index %d exceeds table", index)
	}
	entry := table[off : off+elf64ProgramSize]
	binary.LittleEndian.PutUint32(entry[0:4], uint32(program.type_))
	binary.LittleEndian.PutUint32(entry[4:8], uint32(program.flags))
	binary.LittleEndian.PutUint64(entry[8:16], program.off)
	binary.LittleEndian.PutUint64(entry[16:24], program.vaddr)
	binary.LittleEndian.PutUint64(entry[24:32], program.paddr)
	binary.LittleEndian.PutUint64(entry[32:40], program.filesz)
	binary.LittleEndian.PutUint64(entry[40:48], program.memsz)
	binary.LittleEndian.PutUint64(entry[48:56], program.align)
	return nil
}

// validateAndroidLoadedProgramHeaders mirrors the Android linker64 PHDR
// preflight and loaded-PHDR invariant. The table must satisfy bionic's size
// and alignment limits, then be backed by one PT_LOAD in both file and
// virtual-address space with one affine p_offset -> p_vaddr mapping.
func validateAndroidLoadedProgramHeaders(artifact []byte) error {
	if len(artifact) < elf64HeaderSize {
		return fmt.Errorf("final ELF header is truncated")
	}
	bo := binary.LittleEndian
	phoff := bo.Uint64(artifact[32:40])
	phentsize := bo.Uint16(artifact[54:56])
	phnum := bo.Uint16(artifact[56:58])
	if phentsize != elf64ProgramSize || phnum == 0 {
		return fmt.Errorf("final program-header table is unavailable")
	}
	if uint64(phnum) > 65536/uint64(phentsize) {
		return fmt.Errorf("final program-header table exceeds Android 64KiB limit")
	}
	if phoff == 0 || phoff%8 != 0 {
		return fmt.Errorf("final program-header offset 0x%x is not Android-compatible", phoff)
	}
	tableSize, ok := checkedMul(uint64(phnum), uint64(phentsize))
	if !ok {
		return fmt.Errorf("final program-header table size overflows")
	}
	phend, ok := checkedAdd(phoff, tableSize)
	if !ok || phend > uint64(len(artifact)) {
		return fmt.Errorf("final program-header table is outside the artifact")
	}

	programs := make([]plannedProgramHeader, int(phnum))
	phdrIndex := -1
	for i := range programs {
		off := phoff + uint64(i)*uint64(phentsize)
		programs[i] = readPlannedProgramHeader(artifact, off)
		if programs[i].type_ == stdelf.PT_PHDR {
			if phdrIndex != -1 {
				return fmt.Errorf("final ELF has multiple PT_PHDR entries")
			}
			phdrIndex = i
		}
	}

	var loadedVA uint64
	if phdrIndex >= 0 {
		phdr := programs[phdrIndex]
		if phdr.off != phoff || phdr.filesz != tableSize || phdr.memsz != tableSize {
			return fmt.Errorf("final PT_PHDR does not exactly describe the program-header table")
		}
		loadedVA = phdr.vaddr
	} else {
		firstLoad := -1
		for i, program := range programs {
			if program.type_ == stdelf.PT_LOAD {
				firstLoad = i
				break
			}
		}
		if firstLoad < 0 || programs[firstLoad].off != 0 {
			return fmt.Errorf("final ELF has no loadable program-header discovery path")
		}
		loadedVA, ok = checkedAdd(programs[firstLoad].vaddr, phoff)
		if !ok {
			return fmt.Errorf("final loaded program-header address overflows")
		}
	}

	loadedEnd, ok := checkedAdd(loadedVA, tableSize)
	if !ok {
		return fmt.Errorf("final loaded program-header range overflows")
	}
	for _, load := range programs {
		if load.type_ != stdelf.PT_LOAD || phoff < load.off || loadedVA < load.vaddr {
			continue
		}
		fileEnd, okFile := checkedAdd(load.off, load.filesz)
		vaEnd, okVA := checkedAdd(load.vaddr, load.filesz)
		if !okFile || !okVA || phend > fileEnd || loadedEnd > vaEnd {
			continue
		}
		fileDelta := phoff - load.off
		vaDelta := loadedVA - load.vaddr
		if fileDelta == vaDelta {
			return nil
		}
	}
	return fmt.Errorf("final loaded program-header table is not covered by one PT_LOAD")
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
