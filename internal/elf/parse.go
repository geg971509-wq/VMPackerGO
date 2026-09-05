package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	maxAnalyzedELFSize = 1 << 30
	maxELFTableEntries = 1 << 20
)

const (
	elf64HeaderSize  = 64
	elf64ProgramSize = 56
	elf64SectionSize = 64
)

type fileRange struct {
	start uint64
	end   uint64
}

type loadMapping struct {
	index  int
	off    uint64
	vaddr  uint64
	filesz uint64
	memsz  uint64
	flags  elf.ProgFlag
	align  uint64
}

func (m loadMapping) executable() bool { return m.flags&elf.PF_X != 0 }

func (m loadMapping) fileRange() fileRange   { return fileRange{m.off, m.off + m.filesz} }
func (m loadMapping) memoryRange() fileRange { return fileRange{m.vaddr, m.vaddr + m.memsz} }
func (m loadMapping) fileMemoryRange() fileRange {
	return fileRange{m.vaddr, m.vaddr + m.filesz}
}

type sectionMetadata struct {
	index     int
	name      string
	type_     elf.SectionType
	flags     elf.SectionFlag
	addr      uint64
	off       uint64
	size      uint64
	link      uint32
	info      uint32
	addralign uint64
	entsize   uint64
}

type elfMetadata struct {
	file        *elf.File
	data        []byte
	kind        TargetKind
	hasDynamic  bool
	hasExecLoad bool
	hasInterp   bool
	interpreter string
	hasNote     bool
	loads       []loadMapping
	sections    []sectionMetadata
	warnings    []string
}

func parseELFMetadata(input []byte, mode AndroidMode) (*elfMetadata, error) {
	if len(input) > maxAnalyzedELFSize {
		return nil, fmt.Errorf("input exceeds the 1 GiB analysis limit")
	}
	if len(input) < elf64HeaderSize {
		return nil, fmt.Errorf("ELF header is truncated: got %d bytes", len(input))
	}
	if !bytes.Equal(input[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return nil, fmt.Errorf("input is not an ELF file")
	}
	if input[elf.EI_CLASS] != byte(elf.ELFCLASS64) {
		return nil, fmt.Errorf("input must use ELFCLASS64")
	}
	if input[elf.EI_DATA] != byte(elf.ELFDATA2LSB) {
		return nil, fmt.Errorf("input must use little-endian ELF data")
	}
	if input[elf.EI_VERSION] != byte(elf.EV_CURRENT) {
		return nil, fmt.Errorf("unsupported ELF identification version %d", input[elf.EI_VERSION])
	}

	bo := binary.LittleEndian
	if elf.Machine(bo.Uint16(input[18:20])) != elf.EM_AARCH64 {
		return nil, fmt.Errorf("input must use EM_AARCH64")
	}
	fileType := elf.Type(bo.Uint16(input[16:18]))
	if fileType != elf.ET_DYN && fileType != elf.ET_EXEC {
		return nil, fmt.Errorf("unsupported ELF type %s; expected ET_DYN or ET_EXEC", fileType)
	}
	if bo.Uint32(input[20:24]) != uint32(elf.EV_CURRENT) {
		return nil, fmt.Errorf("unsupported ELF header version %d", bo.Uint32(input[20:24]))
	}
	if bo.Uint16(input[52:54]) != elf64HeaderSize {
		return nil, fmt.Errorf("invalid ELF64 e_ehsize=%d", bo.Uint16(input[52:54]))
	}

	phoff := bo.Uint64(input[32:40])
	shoff := bo.Uint64(input[40:48])
	phentsize := bo.Uint16(input[54:56])
	phnum := uint64(bo.Uint16(input[56:58]))
	shentsize := bo.Uint16(input[58:60])
	shnum := uint64(bo.Uint16(input[60:62]))
	shstrndx := uint64(bo.Uint16(input[62:64]))

	if phnum == uint64(0xffff) || (shnum == 0 && shoff != 0) || shstrndx == uint64(elf.SHN_XINDEX) {
		return nil, fmt.Errorf("ELF extended numbering is not supported for transformation")
	}

	if phnum == 0 {
		if phoff != 0 {
			return nil, fmt.Errorf("program header offset is nonzero with no program headers")
		}
	} else {
		if phoff == 0 || phentsize != elf64ProgramSize {
			return nil, fmt.Errorf("invalid ELF64 program header table metadata")
		}
		if err := validateTableRange("program header", phoff, uint64(phentsize), phnum, uint64(len(input))); err != nil {
			return nil, err
		}
	}
	if shnum == 0 {
		if shoff != 0 || shstrndx != uint64(elf.SHN_UNDEF) {
			return nil, fmt.Errorf("invalid sectionless ELF metadata")
		}
	} else {
		if shoff == 0 || shentsize != elf64SectionSize {
			return nil, fmt.Errorf("invalid ELF64 section header table metadata")
		}
		if err := validateTableRange("section header", shoff, uint64(shentsize), shnum, uint64(len(input))); err != nil {
			return nil, err
		}
		if shstrndx != uint64(elf.SHN_UNDEF) && shstrndx >= shnum {
			return nil, fmt.Errorf("section name string table index %d is out of range", shstrndx)
		}
	}
	if phnum > maxELFTableEntries || shnum > maxELFTableEntries || phnum > uint64(math.MaxInt) || shnum > uint64(math.MaxInt) {
		return nil, fmt.Errorf("ELF table count exceeds analysis limits")
	}

	meta := &elfMetadata{}
	validatedInput := input
	sanitizedPTNULL := false
	for i := uint64(0); i < phnum; i++ {
		off := phoff + i*uint64(phentsize)
		ph := input[off : off+elf64ProgramSize]
		progType := elf.ProgType(bo.Uint32(ph[0:4]))
		flags := elf.ProgFlag(bo.Uint32(ph[4:8]))
		fileOff := bo.Uint64(ph[8:16])
		vaddr := bo.Uint64(ph[16:24])
		paddr := bo.Uint64(ph[24:32])
		filesz := bo.Uint64(ph[32:40])
		memsz := bo.Uint64(ph[40:48])
		align := bo.Uint64(ph[48:56])

		if progType == elf.PT_NULL {
			if fileOff != 0 || vaddr != 0 || paddr != 0 || filesz != 0 || memsz != 0 || align != 0 {
				if !sanitizedPTNULL {
					validatedInput = append([]byte(nil), input...)
					sanitizedPTNULL = true
				}
				clear(validatedInput[off+8 : off+elf64ProgramSize])
			}
			continue
		}
		if _, ok := checkedAdd(vaddr, memsz); !ok {
			return nil, fmt.Errorf("program header %d virtual address range overflows", i)
		}
		if _, ok := checkedAdd(paddr, memsz); !ok {
			return nil, fmt.Errorf("program header %d physical address range overflows", i)
		}
		if fileOff > uint64(len(input)) {
			return nil, fmt.Errorf("program header %d file offset is out of bounds", i)
		}
		end, ok := checkedAdd(fileOff, filesz)
		if !ok || end > uint64(len(input)) {
			return nil, fmt.Errorf("program header %d file range is out of bounds", i)
		}
		if align > 1 && !isPowerOfTwo(align) {
			return nil, fmt.Errorf("program header %d p_align is not a power of two", i)
		}

		switch progType {
		case elf.PT_DYNAMIC:
			if filesz < 16 || filesz%16 != 0 {
				return nil, fmt.Errorf("PT_DYNAMIC %d has invalid ELF64 dynamic metadata size", i)
			}
			meta.hasDynamic = true
		case elf.PT_INTERP:
			if meta.hasInterp {
				return nil, fmt.Errorf("multiple PT_INTERP program headers are not supported")
			}
			if filesz < 2 || input[end-1] != 0 {
				return nil, fmt.Errorf("PT_INTERP %d is empty or not NUL-terminated", i)
			}
			meta.hasInterp = true
			meta.interpreter = string(input[int(fileOff) : int(end)-1])
		case elf.PT_NOTE:
			meta.hasNote = true
		}
		if progType != elf.PT_LOAD {
			continue
		}
		if filesz > memsz {
			return nil, fmt.Errorf("PT_LOAD %d has p_filesz greater than p_memsz", i)
		}
		if align > 1 && fileOff%align != vaddr%align {
			return nil, fmt.Errorf("PT_LOAD %d has incongruent file and virtual addresses", i)
		}
		mapping := loadMapping{index: int(i), off: fileOff, vaddr: vaddr, filesz: filesz, memsz: memsz, flags: flags, align: align}
		meta.loads = append(meta.loads, mapping)
		if mapping.executable() {
			meta.hasExecLoad = true
		}
	}
	if err := validateLoadOverlaps(meta.loads); err != nil {
		return nil, err
	}

	for i := uint64(0); i < shnum; i++ {
		off := shoff + i*uint64(shentsize)
		sh := input[off : off+elf64SectionSize]
		section := sectionMetadata{
			index: int(i), type_: elf.SectionType(bo.Uint32(sh[4:8])), flags: elf.SectionFlag(bo.Uint64(sh[8:16])),
			addr: bo.Uint64(sh[16:24]), off: bo.Uint64(sh[24:32]), size: bo.Uint64(sh[32:40]),
			link: bo.Uint32(sh[40:44]), info: bo.Uint32(sh[44:48]), addralign: bo.Uint64(sh[48:56]), entsize: bo.Uint64(sh[56:64]),
		}
		if section.addralign > 1 && !isPowerOfTwo(section.addralign) {
			return nil, fmt.Errorf("section %d sh_addralign is not a power of two", i)
		}
		if _, ok := checkedAdd(section.addr, section.size); !ok {
			return nil, fmt.Errorf("section %d address range overflows", i)
		}
		if section.type_ != elf.SHT_NOBITS && section.size != 0 {
			end, ok := checkedAdd(section.off, section.size)
			if !ok || end > uint64(len(input)) {
				return nil, fmt.Errorf("section %d file range is out of bounds", i)
			}
		}
		if section.link != 0 && uint64(section.link) >= shnum {
			return nil, fmt.Errorf("section %d sh_link index %d is out of range", i, section.link)
		}
		if section.flags&elf.SHF_INFO_LINK != 0 && uint64(section.info) >= shnum {
			return nil, fmt.Errorf("section %d sh_info index %d is out of range", i, section.info)
		}
		switch section.type_ {
		case elf.SHT_SYMTAB, elf.SHT_DYNSYM:
			if section.link == 0 {
				return nil, fmt.Errorf("symbol section %d has no linked string table", i)
			}
			if section.entsize != 24 || section.size%section.entsize != 0 {
				return nil, fmt.Errorf("symbol section %d has invalid entry size", i)
			}
		case elf.SHT_REL, elf.SHT_RELA:
			if uint64(section.info) >= shnum || section.size != 0 && section.link == 0 {
				return nil, fmt.Errorf("relocation section %d has invalid link/info indices", i)
			}
			wantEntrySize := uint64(16)
			if section.type_ == elf.SHT_RELA {
				wantEntrySize = 24
			}
			if section.entsize != wantEntrySize || section.size%section.entsize != 0 {
				return nil, fmt.Errorf("relocation section %d has invalid entry size", i)
			}
		case elf.SHT_DYNAMIC, elf.SHT_HASH, elf.SHT_GNU_HASH, elf.SHT_SYMTAB_SHNDX:
			if section.link == 0 {
				return nil, fmt.Errorf("section %d requires a linked section", i)
			}
		}
		meta.sections = append(meta.sections, section)
	}

	f, err := elf.NewFile(bytes.NewReader(validatedInput))
	if err != nil {
		return nil, fmt.Errorf("parsing validated ELF metadata failed: %w", err)
	}
	meta.file = f
	meta.data = validatedInput
	for i := range meta.sections {
		if i < len(f.Sections) {
			meta.sections[i].name = f.Sections[i].Name
		}
	}
	kind, warnings, err := classifyAnalyzedTarget(fileType, mode, meta.hasDynamic, meta.hasExecLoad, meta.hasInterp, meta.interpreter)
	if err != nil {
		f.Close()
		return nil, err
	}
	meta.kind = kind
	meta.warnings = warnings
	return meta, nil
}

func classifyAnalyzedTarget(fileType elf.Type, mode AndroidMode, hasDynamic, hasExecLoad, hasInterp bool, interpreter string) (TargetKind, []string, error) {
	if !hasExecLoad {
		return "", nil, fmt.Errorf("Android target requires an executable PT_LOAD segment")
	}
	if hasInterp && !isAndroidInterpreter(interpreter) {
		return "", nil, fmt.Errorf("PT_INTERP %q is not an Android linker", interpreter)
	}
	var kind TargetKind
	var warnings []string
	switch fileType {
	case elf.ET_DYN:
		if !hasDynamic {
			return "", nil, fmt.Errorf("ET_DYN Android target requires PT_DYNAMIC")
		}
		if hasInterp {
			kind = TargetKindAndroidPIE
		} else {
			kind = TargetKindAndroidSO
		}
	case elf.ET_EXEC:
		kind = TargetKindAndroidExec
		if !hasDynamic {
			warnings = append(warnings, "ET_EXEC input has no PT_DYNAMIC; static native executable analysis is development-only")
		}
	default:
		return "", nil, fmt.Errorf("unsupported Android ELF type %s", fileType)
	}

	switch mode {
	case AndroidModeAuto, "":
	case AndroidModeSO:
		if kind != TargetKindAndroidSO {
			return "", nil, fmt.Errorf("mode so requires an ET_DYN shared object with PT_DYNAMIC and no PT_INTERP, got %s", kind)
		}
	case AndroidModeNative:
		if kind == TargetKindAndroidSO {
			return "", nil, fmt.Errorf("mode native requires a PIE or ET_EXEC native executable")
		}
	default:
		return "", nil, fmt.Errorf("unsupported mode %q (supported: auto, so, native)", mode)
	}
	return kind, warnings, nil
}

func isAndroidInterpreter(path string) bool {
	return path == "/system/bin/linker" || path == "/system/bin/linker64"
}

func validateTableRange(name string, off, entsize, count, fileSize uint64) error {
	if entsize == 0 || count > math.MaxUint64/entsize {
		return fmt.Errorf("%s table size overflows", name)
	}
	end, ok := checkedAdd(off, entsize*count)
	if !ok || end > fileSize {
		return fmt.Errorf("%s table is out of bounds", name)
	}
	return nil
}

func validateLoadOverlaps(loads []loadMapping) error {
	if a, b, ok := overlappingBSS(loads); ok {
		return contradictoryLoadOverlap(a, b)
	}
	if a, b, ok := contradictoryMappedOverlap(loads, false); ok {
		return contradictoryLoadOverlap(a, b)
	}
	if a, b, ok := contradictoryMappedOverlap(loads, true); ok {
		return contradictoryLoadOverlap(a, b)
	}
	return nil
}

type loadInterval struct {
	start uint64
	end   uint64
	load  loadMapping
}

type loadDelta struct {
	negative bool
	value    uint64
}

type intervalMaximum struct {
	end   uint64
	index int
	set   bool
}

type deltaMaximum struct {
	delta loadDelta
	intervalMaximum
}

func overlappingBSS(loads []loadMapping) (loadMapping, loadMapping, bool) {
	memory := make([]loadInterval, 0, len(loads))
	bss := make([]loadInterval, 0, len(loads))
	byIndex := make(map[int]loadMapping, len(loads))
	for _, load := range loads {
		byIndex[load.index] = load
		if load.memsz != 0 {
			memory = append(memory, loadInterval{start: load.vaddr, end: load.vaddr + load.memsz, load: load})
		}
		if load.filesz < load.memsz {
			bss = append(bss, loadInterval{start: load.vaddr + load.filesz, end: load.vaddr + load.memsz, load: load})
		}
	}
	sort.Slice(memory, func(i, j int) bool { return memory[i].start < memory[j].start })
	sort.Slice(bss, func(i, j int) bool { return bss[i].end < bss[j].end })
	var first, second intervalMaximum
	next := 0
	for _, suffix := range bss {
		for next < len(memory) && memory[next].start < suffix.end {
			candidate := intervalMaximum{end: memory[next].end, index: memory[next].load.index, set: true}
			if !first.set || candidate.end > first.end {
				second, first = first, candidate
			} else if !second.set || candidate.end > second.end {
				second = candidate
			}
			next++
		}
		candidate := first
		if candidate.index == suffix.load.index {
			candidate = second
		}
		if candidate.set && candidate.end > suffix.start {
			return suffix.load, byIndex[candidate.index], true
		}
	}
	return loadMapping{}, loadMapping{}, false
}

func contradictoryMappedOverlap(loads []loadMapping, virtual bool) (loadMapping, loadMapping, bool) {
	intervals := make([]loadInterval, 0, len(loads))
	byIndex := make(map[int]loadMapping, len(loads))
	for _, load := range loads {
		byIndex[load.index] = load
		if load.filesz == 0 {
			continue
		}
		start := load.off
		if virtual {
			start = load.vaddr
		}
		intervals = append(intervals, loadInterval{start: start, end: start + load.filesz, load: load})
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].end < intervals[j].end
	})

	groups := make(map[loadDelta]intervalMaximum)
	var first, second deltaMaximum
	var writable, executable, writableExecutable intervalMaximum
	for _, interval := range intervals {
		delta := mappingDelta(interval.load)
		other := first
		if other.set && other.delta == delta {
			other = second
		}
		if other.set && other.end > interval.start {
			return interval.load, byIndex[other.index], true
		}
		if interval.load.flags&(elf.PF_W|elf.PF_X) == elf.PF_W|elf.PF_X && first.set && first.end > interval.start {
			return interval.load, byIndex[first.index], true
		}
		if interval.load.flags&elf.PF_W != 0 && executable.set && executable.end > interval.start {
			return interval.load, byIndex[executable.index], true
		}
		if interval.load.flags&elf.PF_X != 0 && writable.set && writable.end > interval.start {
			return interval.load, byIndex[writable.index], true
		}
		if interval.load.flags&(elf.PF_W|elf.PF_X) == 0 && writableExecutable.set && writableExecutable.end > interval.start {
			return interval.load, byIndex[writableExecutable.index], true
		}

		best := groups[delta]
		if !best.set || interval.end > best.end {
			best = intervalMaximum{end: interval.end, index: interval.load.index, set: true}
			groups[delta] = best
		}
		candidate := deltaMaximum{delta: delta, intervalMaximum: best}
		switch {
		case first.set && first.delta == delta:
			first = candidate
		case second.set && second.delta == delta:
			second = candidate
			if second.end > first.end {
				first, second = second, first
			}
		case !first.set || candidate.end > first.end:
			second, first = first, candidate
		case !second.set || candidate.end > second.end:
			second = candidate
		}
		if interval.load.flags&elf.PF_W != 0 && (!writable.set || interval.end > writable.end) {
			writable = intervalMaximum{end: interval.end, index: interval.load.index, set: true}
		}
		if interval.load.flags&elf.PF_X != 0 && (!executable.set || interval.end > executable.end) {
			executable = intervalMaximum{end: interval.end, index: interval.load.index, set: true}
		}
		if interval.load.flags&(elf.PF_W|elf.PF_X) == elf.PF_W|elf.PF_X && (!writableExecutable.set || interval.end > writableExecutable.end) {
			writableExecutable = intervalMaximum{end: interval.end, index: interval.load.index, set: true}
		}
	}
	return loadMapping{}, loadMapping{}, false
}

func mappingDelta(load loadMapping) loadDelta {
	if load.off >= load.vaddr {
		return loadDelta{value: load.off - load.vaddr}
	}
	return loadDelta{negative: true, value: load.vaddr - load.off}
}

func contradictoryLoadOverlap(a, b loadMapping) error {
	return fmt.Errorf("PT_LOAD %d and %d have contradictory overlapping mappings", a.index, b.index)
}

func combinedWritableExecutable(a, b elf.ProgFlag) bool {
	combined := a | b
	return combined&elf.PF_W != 0 && combined&elf.PF_X != 0
}

func intersectRanges(a, b fileRange) (fileRange, bool) {
	start := a.start
	if b.start > start {
		start = b.start
	}
	end := a.end
	if b.end < end {
		end = b.end
	}
	return fileRange{start, end}, start < end
}

func checkedAdd(a, b uint64) (uint64, bool) {
	if a > math.MaxUint64-b {
		return 0, false
	}
	return a + b, true
}

func isPowerOfTwo(value uint64) bool { return value != 0 && value&(value-1) == 0 }

func (m *elfMetadata) fileBackedMapping(start, end uint64) (loadMapping, bool) {
	if start >= end {
		return loadMapping{}, false
	}
	for _, mapping := range m.loads {
		fileEnd, ok := checkedAdd(mapping.vaddr, mapping.filesz)
		if ok && start >= mapping.vaddr && end <= fileEnd {
			return mapping, true
		}
	}
	return loadMapping{}, false
}

func (m *elfMetadata) executableMapping(start, end uint64) (loadMapping, bool) {
	if start >= end {
		return loadMapping{}, false
	}
	for _, mapping := range m.loads {
		if !mapping.executable() {
			continue
		}
		fileEnd := mapping.vaddr + mapping.filesz
		if start >= mapping.vaddr && end <= fileEnd {
			return mapping, true
		}
	}
	return loadMapping{}, false
}

func mappingFileOffset(mapping loadMapping, address uint64) (uint64, bool) {
	if address < mapping.vaddr || address >= mapping.vaddr+mapping.filesz {
		return 0, false
	}
	off, ok := checkedAdd(mapping.off, address-mapping.vaddr)
	return off, ok
}
