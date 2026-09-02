package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

type fixtureSymbol struct {
	name      string
	addr      uint64
	size      uint64
	undefined bool
}

type fixtureRelocation struct {
	name   string
	offset uint64
	type_  elf.R_AARCH64
	plt    bool
}

type fixtureOptions struct {
	fileType elf.Type
	interp   bool
	dynamic  bool
	code     []uint32
	symtab   []fixtureSymbol
	dynsym   []fixtureSymbol
	relocs   []fixtureRelocation
}

type elfFixture struct {
	data       []byte
	phoff      int
	shoff      int
	sectionOff map[string]int
}

func buildELFFixture(options fixtureOptions) elfFixture {
	if options.fileType == 0 {
		options.fileType = elf.ET_DYN
	}
	if options.code == nil {
		options.code = []uint32{0xD503201F, 0xD503201F, 0xD65F03C0}
	}
	if options.fileType == elf.ET_DYN && !options.dynamic {
		options.dynamic = true
	}

	const (
		phoff    = 64
		codeOff  = 0x200
		loadVA   = 0x1000
		codeAddr = loadVA + codeOff
	)
	var pltRelocations []fixtureRelocation
	for _, relocation := range options.relocs {
		if relocation.plt {
			pltRelocations = append(pltRelocations, relocation)
		}
	}
	phnum := 1
	if len(pltRelocations) != 0 {
		phnum++
	}
	if options.dynamic {
		phnum++
	}
	if options.interp {
		phnum++
	}

	data := make([]byte, codeOff+len(options.code)*4)
	copy(data[:16], []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	bo := binary.LittleEndian
	bo.PutUint16(data[16:18], uint16(options.fileType))
	bo.PutUint16(data[18:20], uint16(elf.EM_AARCH64))
	bo.PutUint32(data[20:24], uint32(elf.EV_CURRENT))
	bo.PutUint64(data[32:40], phoff)
	bo.PutUint16(data[52:54], elf64HeaderSize)
	bo.PutUint16(data[54:56], elf64ProgramSize)
	bo.PutUint16(data[56:58], uint16(phnum))
	for i, instruction := range options.code {
		bo.PutUint32(data[codeOff+i*4:], instruction)
	}

	align := func(value, boundary int) int { return (value + boundary - 1) &^ (boundary - 1) }
	appendBytes := func(value []byte, boundary int) int {
		off := align(len(data), boundary)
		data = append(data, make([]byte, off-len(data))...)
		data = append(data, value...)
		return off
	}
	makeStringTable := func(symbols []fixtureSymbol) ([]byte, map[string]uint32) {
		table := []byte{0}
		offsets := make(map[string]uint32)
		for _, symbol := range symbols {
			if _, exists := offsets[symbol.name]; exists {
				continue
			}
			offsets[symbol.name] = uint32(len(table))
			table = append(table, symbol.name...)
			table = append(table, 0)
		}
		return table, offsets
	}
	makeSymbols := func(symbols []fixtureSymbol, names map[string]uint32) []byte {
		table := make([]byte, 24)
		for _, symbol := range symbols {
			entry := make([]byte, 24)
			bo.PutUint32(entry[0:4], names[symbol.name])
			entry[4] = byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC)
			if !symbol.undefined {
				bo.PutUint16(entry[6:8], 1)
			}
			value := symbol.addr
			if value == 0 && !symbol.undefined {
				value = codeAddr
			}
			bo.PutUint64(entry[8:16], value)
			bo.PutUint64(entry[16:24], symbol.size)
			table = append(table, entry...)
		}
		return table
	}

	type section struct {
		name      string
		type_     elf.SectionType
		flags     elf.SectionFlag
		addr      uint64
		off       uint64
		size      uint64
		linkName  string
		infoName  string
		info      uint32
		align     uint64
		entrySize uint64
	}
	sections := []section{{}, {name: ".text", type_: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR, addr: codeAddr, off: codeOff, size: uint64(len(options.code) * 4), align: 4}}
	var gotAddr uint64
	var gotOff int
	if len(pltRelocations) != 0 {
		const pltOff = 0x300
		if len(data) > pltOff {
			panic("fixture code overlaps fixed PLT")
		}
		data = append(data, make([]byte, pltOff-len(data))...)
		pltData := make([]byte, 32+len(pltRelocations)*16)
		gotOff = align(pltOff+len(pltData), 8)
		gotAddr = loadVA + uint64(gotOff)
		for off := 0; off < 32; off += 4 {
			bo.PutUint32(pltData[off:], 0xD503201F)
		}
		for off := 32; off < len(pltData); off += 16 {
			entryAddr := loadVA + pltOff + uint64(off)
			gotSlot := gotAddr + uint64((off-32)/16)*8
			pages := (int64(gotSlot&^0xfff) - int64(entryAddr&^0xfff)) >> 12
			imm21 := uint32(pages) & 0x1fffff
			pageOffset := uint32(gotSlot & 0xfff)
			bo.PutUint32(pltData[off:], 0x90000010|(imm21&3)<<29|(imm21>>2)<<5)
			bo.PutUint32(pltData[off+4:], 0xF9400211|(pageOffset/8)<<10)
			bo.PutUint32(pltData[off+8:], 0x91000210|pageOffset<<10)
			bo.PutUint32(pltData[off+12:], 0xD61F0220)
		}
		data = append(data, pltData...)
		appendBytes(make([]byte, len(pltRelocations)*8), 8)
		sections = append(sections,
			section{name: ".plt", type_: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR, addr: loadVA + pltOff, off: pltOff, size: uint64(len(pltData)), align: 16},
			section{name: ".got.plt", type_: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC | elf.SHF_WRITE, addr: gotAddr, off: uint64(gotOff), size: uint64(len(pltRelocations) * 8), align: 8},
		)
	}
	if options.symtab != nil {
		stringsData, names := makeStringTable(options.symtab)
		strOff := appendBytes(stringsData, 1)
		symData := makeSymbols(options.symtab, names)
		symOff := appendBytes(symData, 8)
		sections = append(sections,
			section{name: ".strtab", type_: elf.SHT_STRTAB, off: uint64(strOff), size: uint64(len(stringsData)), align: 1},
			section{name: ".symtab", type_: elf.SHT_SYMTAB, off: uint64(symOff), size: uint64(len(symData)), linkName: ".strtab", info: 1, align: 8, entrySize: 24},
		)
	}
	dynSymbolIndex := make(map[string]uint32)
	if options.dynsym != nil {
		stringsData, names := makeStringTable(options.dynsym)
		for i, symbol := range options.dynsym {
			dynSymbolIndex[symbol.name] = uint32(i + 1)
		}
		strOff := appendBytes(stringsData, 1)
		symData := makeSymbols(options.dynsym, names)
		symOff := appendBytes(symData, 8)
		sections = append(sections,
			section{name: ".dynstr", type_: elf.SHT_STRTAB, flags: elf.SHF_ALLOC, off: uint64(strOff), size: uint64(len(stringsData)), align: 1},
			section{name: ".dynsym", type_: elf.SHT_DYNSYM, flags: elf.SHF_ALLOC, off: uint64(symOff), size: uint64(len(symData)), linkName: ".dynstr", info: 1, align: 8, entrySize: 24},
		)
	}
	var directData, pltData []byte
	for _, relocation := range options.relocs {
		entry := make([]byte, 24)
		offset := relocation.offset
		if relocation.plt {
			offset = gotAddr + uint64(len(pltData)/24)*8
		}
		bo.PutUint64(entry[0:8], offset)
		bo.PutUint64(entry[8:16], uint64(dynSymbolIndex[relocation.name])<<32|uint64(relocation.type_))
		if relocation.plt {
			pltData = append(pltData, entry...)
		} else {
			directData = append(directData, entry...)
		}
	}
	if len(directData) != 0 {
		off := appendBytes(directData, 8)
		sections = append(sections, section{name: ".rela.text", type_: elf.SHT_RELA, flags: elf.SHF_INFO_LINK, off: uint64(off), size: uint64(len(directData)), linkName: ".dynsym", infoName: ".text", align: 8, entrySize: 24})
	}
	if len(pltData) != 0 {
		off := appendBytes(pltData, 8)
		sections = append(sections, section{name: ".rela.plt", type_: elf.SHT_RELA, flags: elf.SHF_ALLOC | elf.SHF_INFO_LINK, off: uint64(off), size: uint64(len(pltData)), linkName: ".dynsym", infoName: ".got.plt", align: 8, entrySize: 24})
	}

	shstr := []byte{0}
	nameOffsets := make(map[string]uint32)
	for _, section := range sections[1:] {
		nameOffsets[section.name] = uint32(len(shstr))
		shstr = append(shstr, section.name...)
		shstr = append(shstr, 0)
	}
	nameOffsets[".shstrtab"] = uint32(len(shstr))
	shstr = append(shstr, []byte(".shstrtab\x00")...)
	shstrOff := appendBytes(shstr, 1)
	sections = append(sections, section{name: ".shstrtab", type_: elf.SHT_STRTAB, off: uint64(shstrOff), size: uint64(len(shstr)), align: 1})

	sectionIndex := make(map[string]uint32)
	for i, section := range sections {
		sectionIndex[section.name] = uint32(i)
	}
	shoff := align(len(data), 8)
	data = append(data, make([]byte, shoff-len(data)+len(sections)*elf64SectionSize)...)
	sectionOff := make(map[string]int)
	for i, section := range sections {
		off := shoff + i*elf64SectionSize
		sectionOff[section.name] = off
		bo.PutUint32(data[off:off+4], nameOffsets[section.name])
		bo.PutUint32(data[off+4:off+8], uint32(section.type_))
		bo.PutUint64(data[off+8:off+16], uint64(section.flags))
		bo.PutUint64(data[off+16:off+24], section.addr)
		bo.PutUint64(data[off+24:off+32], section.off)
		bo.PutUint64(data[off+32:off+40], section.size)
		bo.PutUint32(data[off+40:off+44], sectionIndex[section.linkName])
		info := section.info
		if section.infoName != "" {
			info = sectionIndex[section.infoName]
		}
		bo.PutUint32(data[off+44:off+48], info)
		bo.PutUint64(data[off+48:off+56], section.align)
		bo.PutUint64(data[off+56:off+64], section.entrySize)
	}
	bo.PutUint64(data[40:48], uint64(shoff))
	bo.PutUint16(data[58:60], elf64SectionSize)
	bo.PutUint16(data[60:62], uint16(len(sections)))
	bo.PutUint16(data[62:64], uint16(sectionIndex[".shstrtab"]))

	writeProgram := func(index int, type_ elf.ProgType, flags elf.ProgFlag, off, addr, filesz, memsz, alignment uint64) {
		entry := phoff + index*elf64ProgramSize
		bo.PutUint32(data[entry:entry+4], uint32(type_))
		bo.PutUint32(data[entry+4:entry+8], uint32(flags))
		bo.PutUint64(data[entry+8:entry+16], off)
		bo.PutUint64(data[entry+16:entry+24], addr)
		bo.PutUint64(data[entry+24:entry+32], addr)
		bo.PutUint64(data[entry+32:entry+40], filesz)
		bo.PutUint64(data[entry+40:entry+48], memsz)
		bo.PutUint64(data[entry+48:entry+56], alignment)
	}
	execSize := uint64(len(data))
	if len(pltRelocations) != 0 {
		execSize = uint64(gotOff)
	}
	writeProgram(0, elf.PT_LOAD, elf.PF_R|elf.PF_X, 0, loadVA, execSize, execSize, 0x1000)
	program := 1
	if len(pltRelocations) != 0 {
		gotSize := uint64(len(pltRelocations) * 8)
		writeProgram(program, elf.PT_LOAD, elf.PF_R|elf.PF_W, uint64(gotOff), gotAddr, gotSize, gotSize, 8)
		program++
	}
	if options.dynamic {
		writeProgram(program, elf.PT_DYNAMIC, elf.PF_R, 0x180, loadVA+0x180, 16, 16, 8)
		program++
	}
	if options.interp {
		copy(data[0x190:], []byte("/system/bin/linker64\x00"))
		writeProgram(program, elf.PT_INTERP, elf.PF_R, 0x190, loadVA+0x190, 21, 21, 1)
	}
	return elfFixture{data: data, phoff: phoff, shoff: shoff, sectionOff: sectionOff}
}

func analyzeFixture(t *testing.T, fixture elfFixture, selection SelectionRequest, mode string) (Analysis, error) {
	t.Helper()
	return Analyze(Request{Input: fixture.data, Mode: mode, Selections: []SelectionRequest{selection}})
}

func addressSelection(start, end uint64) SelectionRequest {
	spec := AddrSpec{Addr: start, End: end, Name: "entry"}
	return SelectionRequest{Source: "direct", Selector: "raw", Name: "entry", AddrSpec: &spec}
}

func TestAnalyzeModeClassification(t *testing.T) {
	const entry = 0x1200
	tests := []struct {
		name    string
		options fixtureOptions
		mode    string
		kind    TargetKind
		warning bool
	}{
		{name: "so-auto", options: fixtureOptions{dynamic: true}, mode: "auto", kind: TargetKindAndroidSO},
		{name: "pie-native", options: fixtureOptions{dynamic: true, interp: true}, mode: "native", kind: TargetKindAndroidPIE},
		{name: "exec", options: fixtureOptions{fileType: elf.ET_EXEC}, mode: "native", kind: TargetKindAndroidExec, warning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildELFFixture(test.options)
			analysis, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), test.mode)
			if err != nil || analysis.TargetKind != test.kind || (len(analysis.Warnings) != 0) != test.warning {
				t.Fatalf("kind=%q warnings=%v err=%v", analysis.TargetKind, analysis.Warnings, err)
			}
		})
	}
	so := buildELFFixture(fixtureOptions{dynamic: true})
	if _, err := analyzeFixture(t, so, addressSelection(entry, entry+12), "native"); err == nil {
		t.Fatal("native mode accepted a shared object")
	}
	pie := buildELFFixture(fixtureOptions{dynamic: true, interp: true})
	if _, err := analyzeFixture(t, pie, addressSelection(entry, entry+12), "so"); err == nil {
		t.Fatal("so mode accepted a PIE")
	}
}

func TestExtendedNumberingFailsClosedBeforeTransformation(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	bo := binary.LittleEndian
	phnum := bo.Uint16(fixture.data[56:58])
	shnum := bo.Uint16(fixture.data[60:62])
	shstrndx := bo.Uint16(fixture.data[62:64])
	cases := map[string]func([]byte){
		"phnum": func(data []byte) {
			bo.PutUint16(data[56:58], 0xffff)
			bo.PutUint32(data[fixture.shoff+44:fixture.shoff+48], uint32(phnum))
		},
		"shnum": func(data []byte) {
			bo.PutUint16(data[60:62], 0)
			bo.PutUint64(data[fixture.shoff+32:fixture.shoff+40], uint64(shnum))
		},
		"shstrndx": func(data []byte) {
			bo.PutUint16(data[62:64], uint16(elf.SHN_XINDEX))
			bo.PutUint32(data[fixture.shoff+40:fixture.shoff+44], uint32(shstrndx))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := append([]byte(nil), fixture.data...)
			mutate(input)
			before := append([]byte(nil), input...)
			req := Request{Input: input, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}}
			if _, err := Analyze(req); err == nil || !strings.Contains(err.Error(), "extended numbering") {
				t.Fatalf("Analyze err=%v", err)
			}
			result, err := Process(req)
			if err == nil || !strings.Contains(err.Error(), "extended numbering") || len(result.Artifact) != 0 {
				t.Fatalf("Process result=%+v err=%v", result, err)
			}
			if !bytes.Equal(input, before) {
				t.Fatal("Process mutated extended-numbering input")
			}
		})
	}
}

func TestAnalyzeAcceptsStalePTNULLFields(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	bo := binary.LittleEndian
	bo.PutUint16(fixture.data[56:58], 3)
	off := fixture.phoff + 2*elf64ProgramSize
	bo.PutUint32(fixture.data[off:off+4], uint32(elf.PT_NULL))
	bo.PutUint64(fixture.data[off+8:off+16], math.MaxUint64)
	bo.PutUint64(fixture.data[off+16:off+24], math.MaxUint64)
	bo.PutUint64(fixture.data[off+24:off+32], math.MaxUint64)
	bo.PutUint64(fixture.data[off+32:off+40], math.MaxUint64)
	bo.PutUint64(fixture.data[off+40:off+48], math.MaxUint64)
	bo.PutUint64(fixture.data[off+48:off+56], 3)
	if _, err := analyzeFixture(t, fixture, addressSelection(0x1200, 0x120c), "auto"); err != nil {
		t.Fatalf("stale PT_NULL fields: %v", err)
	}
}

func TestAnalyzeRejectsMalformedELFMetadata(t *testing.T) {
	valid := buildELFFixture(fixtureOptions{dynamic: true})
	mutations := map[string]func([]byte){
		"class":          func(data []byte) { data[elf.EI_CLASS] = byte(elf.ELFCLASS32) },
		"data":           func(data []byte) { data[elf.EI_DATA] = byte(elf.ELFDATA2MSB) },
		"machine":        func(data []byte) { binary.LittleEndian.PutUint16(data[18:20], uint16(elf.EM_X86_64)) },
		"type":           func(data []byte) { binary.LittleEndian.PutUint16(data[16:18], uint16(elf.ET_REL)) },
		"phoff-overflow": func(data []byte) { binary.LittleEndian.PutUint64(data[32:40], math.MaxUint64-8) },
		"phentsize":      func(data []byte) { binary.LittleEndian.PutUint16(data[54:56], 1) },
		"filesz-memsz": func(data []byte) {
			binary.LittleEndian.PutUint64(data[valid.phoff+32:], 0x400)
			binary.LittleEndian.PutUint64(data[valid.phoff+40:], 0x300)
		},
		"bad-align":          func(data []byte) { binary.LittleEndian.PutUint64(data[valid.phoff+48:], 3) },
		"bad-congruence":     func(data []byte) { binary.LittleEndian.PutUint64(data[valid.phoff+8:], 1) },
		"shoff-overflow":     func(data []byte) { binary.LittleEndian.PutUint64(data[40:48], math.MaxUint64-8) },
		"section-align":      func(data []byte) { binary.LittleEndian.PutUint64(data[valid.sectionOff[".text"]+48:], 3) },
		"section-file-range": func(data []byte) { binary.LittleEndian.PutUint64(data[valid.sectionOff[".text"]+32:], math.MaxUint64) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data := append([]byte(nil), valid.data...)
			mutate(data)
			if _, err := Analyze(Request{Input: data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}}); err == nil {
				t.Fatal("accepted malformed ELF")
			}
		})
	}
	for size := 0; size < elf64HeaderSize; size++ {
		if _, err := Analyze(Request{Input: valid.data[:size], Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}}); err == nil {
			t.Fatalf("accepted truncation at %d", size)
		}
	}
}

func TestValidateLoadOverlapRules(t *testing.T) {
	consistent := []loadMapping{
		{index: 0, off: 0, vaddr: 0x1000, filesz: 0x1000, memsz: 0x1000, flags: elf.PF_R, align: 0x1000},
		{index: 1, off: 0x800, vaddr: 0x1800, filesz: 0x1000, memsz: 0x1000, flags: elf.PF_R | elf.PF_X, align: 0x1000},
	}
	if err := validateLoadOverlaps(consistent); err != nil {
		t.Fatalf("rejected consistent page overlap: %v", err)
	}
	contradictory := append([]loadMapping(nil), consistent...)
	contradictory[1].vaddr = 0x2000
	if err := validateLoadOverlaps(contradictory); err == nil {
		t.Fatal("accepted contradictory file mapping")
	}
	writableExecutable := append([]loadMapping(nil), consistent...)
	writableExecutable[1].flags = elf.PF_R | elf.PF_W
	consistent[0].flags = elf.PF_R | elf.PF_X
	writableExecutable[0] = consistent[0]
	if err := validateLoadOverlaps(writableExecutable); err == nil {
		t.Fatal("accepted overlapping W+X permission semantics")
	}
}

func TestValidateLoadOverlapsMatchesPairwiseSemantics(t *testing.T) {
	reference := func(loads []loadMapping) error {
		for i := 0; i < len(loads); i++ {
			for j := i + 1; j < len(loads); j++ {
				a, b := loads[i], loads[j]
				memoryOverlap, memoryOK := intersectRanges(a.memoryRange(), b.memoryRange())
				fileOverlap, fileOK := intersectRanges(a.fileRange(), b.fileRange())
				if !memoryOK && !fileOK {
					continue
				}
				consistent := false
				if memoryOK {
					if memoryOverlap.end <= a.vaddr+a.filesz && memoryOverlap.end <= b.vaddr+b.filesz {
						aFile := a.off + (memoryOverlap.start - a.vaddr)
						bFile := b.off + (memoryOverlap.start - b.vaddr)
						consistent = aFile == bFile && !combinedWritableExecutable(a.flags, b.flags)
					}
				} else if fileOK {
					aVA := a.vaddr + (fileOverlap.start - a.off)
					bVA := b.vaddr + (fileOverlap.start - b.off)
					consistent = aVA == bVA && !combinedWritableExecutable(a.flags, b.flags)
				}
				if !consistent {
					return contradictoryLoadOverlap(a, b)
				}
			}
		}
		return nil
	}
	state := uint64(1)
	next := func(limit uint64) uint64 {
		state = state*6364136223846793005 + 1
		return state % limit
	}
	for iteration := 0; iteration < 1000; iteration++ {
		loads := make([]loadMapping, 2+next(10))
		for i := range loads {
			filesz := next(32)
			loads[i] = loadMapping{
				index: i, off: next(64), vaddr: next(64), filesz: filesz,
				memsz: filesz + next(16), flags: elf.ProgFlag(next(8)),
			}
		}
		want := reference(loads)
		got := validateLoadOverlaps(loads)
		if (want == nil) != (got == nil) {
			t.Fatalf("iteration %d loads=%+v reference=%v sweep=%v", iteration, loads, want, got)
		}
	}

	loads := make([]loadMapping, 20000)
	for i := range loads {
		start := uint64(i) * 0x2000
		loads[i] = loadMapping{index: i, off: start, vaddr: start + 0x1000, filesz: 0x1000, memsz: 0x1000, flags: elf.PF_R}
	}
	if err := validateLoadOverlaps(loads); err != nil {
		t.Fatalf("large non-overlapping table: %v", err)
	}
}

func TestAnalyzeSymbolTablesAndConflicts(t *testing.T) {
	const entry = 0x1200
	cases := []struct {
		name       string
		options    fixtureOptions
		wantSource string
		wantError  string
	}{
		{name: "symtab", options: fixtureOptions{dynamic: true, symtab: []fixtureSymbol{{name: "target", size: 12}}}, wantSource: "symtab"},
		{name: "dynsym", options: fixtureOptions{dynamic: true, dynsym: []fixtureSymbol{{name: "target", size: 12}}}, wantSource: "dynsym"},
		{name: "identical", options: fixtureOptions{dynamic: true, symtab: []fixtureSymbol{{name: "target", size: 12}}, dynsym: []fixtureSymbol{{name: "target", size: 12}}}, wantSource: "symtab"},
		{name: "conflict", options: fixtureOptions{dynamic: true, symtab: []fixtureSymbol{{name: "target", size: 12}}, dynsym: []fixtureSymbol{{name: "target", addr: entry + 4, size: 12}}}, wantError: "conflicting"},
		{name: "undefined", options: fixtureOptions{dynamic: true, dynsym: []fixtureSymbol{{name: "target", undefined: true}}}, wantError: "not found"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			analysis, err := analyzeFixture(t, buildELFFixture(test.options), SelectionRequest{Name: "target"}, "auto")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || len(analysis.Selections) != 1 || analysis.Selections[0].SymbolSource != test.wantSource {
				t.Fatalf("analysis=%+v err=%v", analysis, err)
			}
		})
	}
}

func TestAnalyzeRejectsDirectRecoveryCalls(t *testing.T) {
	const entry = 0x1200
	t.Run("defined-symbol", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{encodeBranch(0x94000000, entry, entry+16), 0xD503201F, 0xD65F03C0, 0xD503201F, 0xD65F03C0},
			symtab: []fixtureSymbol{{name: "setjmp@@LIBC", addr: entry + 16, size: 4}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), "setjmp@@LIBC") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("call26-relocation", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{0x94000000, 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{{name: "sigsetjmp@LIBC", undefined: true}},
			relocs: []fixtureRelocation{{name: "sigsetjmp@LIBC", offset: entry, type_: elf.R_AARCH64_CALL26}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), "sigsetjmp@LIBC") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("jump-slot-plt", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{encodeBranch(0x94000000, entry, 0x1320), 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{{name: "setjmp@LIBC", undefined: true}},
			relocs: []fixtureRelocation{{name: "setjmp@LIBC", type_: elf.R_AARCH64_JUMP_SLOT, plt: true}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), "setjmp@LIBC") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("benign-external", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{encodeBranch(0x94000000, entry, 0x1320), 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{{name: "benign_external", undefined: true}},
			relocs: []fixtureRelocation{{name: "benign_external", type_: elf.R_AARCH64_JUMP_SLOT, plt: true}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("plt-slot-mismatch", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code: []uint32{encodeBranch(0x94000000, entry, 0x1330), 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{
				{name: "setjmp", undefined: true},
				{name: "benign_external", undefined: true},
			},
			relocs: []fixtureRelocation{
				{name: "setjmp", type_: elf.R_AARCH64_JUMP_SLOT, plt: true},
				{name: "benign_external", type_: elf.R_AARCH64_JUMP_SLOT, plt: true},
			},
		})
		first := append([]byte(nil), fixture.data[0x320:0x330]...)
		copy(fixture.data[0x320:0x330], fixture.data[0x330:0x340])
		copy(fixture.data[0x330:0x340], first)
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), "does not resolve JUMP_SLOT") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("plt-header", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{encodeBranch(0x94000000, entry, 0x1300), 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{{name: "benign_external", undefined: true}},
			relocs: []fixtureRelocation{{name: "benign_external", type_: elf.R_AARCH64_JUMP_SLOT, plt: true}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), "unresolved relocation-backed PLT") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestAnalyzeRejectsMalformedPLTGOT(t *testing.T) {
	const entry = 0x1200
	base := buildELFFixture(fixtureOptions{
		code:   []uint32{encodeBranch(0x94000000, entry, 0x1320), 0xD503201F, 0xD65F03C0},
		dynsym: []fixtureSymbol{{name: "benign_external", undefined: true}},
		relocs: []fixtureRelocation{{name: "benign_external", type_: elf.R_AARCH64_JUMP_SLOT, plt: true}},
	})

	t.Run("coherent-unmapped-slot", func(t *testing.T) {
		data := append([]byte(nil), base.data...)
		const gotSlot = uint64(0x5000)
		binary.LittleEndian.PutUint64(data[base.sectionOff[".got.plt"]+16:], gotSlot)
		relocationOff := int(binary.LittleEndian.Uint64(data[base.sectionOff[".rela.plt"]+24:]))
		binary.LittleEndian.PutUint64(data[relocationOff:], gotSlot)
		pltEntry := data[0x320:0x330]
		pages := (int64(gotSlot&^0xfff) - int64(uint64(0x1320)&^0xfff)) >> 12
		imm21 := uint32(pages) & 0x1fffff
		pageOffset := uint32(gotSlot & 0xfff)
		binary.LittleEndian.PutUint32(pltEntry[0:], 0x90000010|(imm21&3)<<29|(imm21>>2)<<5)
		binary.LittleEndian.PutUint32(pltEntry[4:], 0xF9400211|(pageOffset/8)<<10)
		binary.LittleEndian.PutUint32(pltEntry[8:], 0x91000210|pageOffset<<10)
		if _, err := Analyze(Request{Input: data, Selections: []SelectionRequest{addressSelection(entry, entry+12)}}); err == nil || !strings.Contains(err.Error(), "file-backed PT_LOAD") {
			t.Fatalf("coherent unmapped GOT err=%v", err)
		}
	})

	t.Run("non-writable-load", func(t *testing.T) {
		data := append([]byte(nil), base.data...)
		binary.LittleEndian.PutUint32(data[base.phoff+elf64ProgramSize+4:], uint32(elf.PF_R))
		if _, err := Analyze(Request{Input: data, Selections: []SelectionRequest{addressSelection(entry, entry+12)}}); err == nil || !strings.Contains(err.Error(), "writable") {
			t.Fatalf("non-writable GOT load err=%v", err)
		}
	})

	t.Run("malformed-section-flags", func(t *testing.T) {
		tests := []struct {
			name  string
			flags elf.SectionFlag
		}{
			{name: "missing-write", flags: elf.SHF_ALLOC},
			{name: "extra-tls", flags: elf.SHF_ALLOC | elf.SHF_WRITE | elf.SHF_TLS},
			{name: "extra-merge", flags: elf.SHF_ALLOC | elf.SHF_WRITE | elf.SHF_MERGE},
			{name: "extra-strings", flags: elf.SHF_ALLOC | elf.SHF_WRITE | elf.SHF_STRINGS},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				data := append([]byte(nil), base.data...)
				binary.LittleEndian.PutUint64(data[base.sectionOff[".got.plt"]+8:], uint64(test.flags))
				before := append([]byte(nil), data...)
				req := Request{Input: data, Selections: []SelectionRequest{addressSelection(entry, entry+12)}}
				if _, err := Analyze(req); err == nil || !strings.Contains(err.Error(), "flags or alignment") {
					t.Fatalf("Analyze malformed GOT flags err=%v", err)
				}
				result, err := Process(req)
				if err == nil || !strings.Contains(err.Error(), "flags or alignment") || len(result.Artifact) != 0 {
					t.Fatalf("Process result=%+v err=%v", result, err)
				}
				if !bytes.Equal(data, before) {
					t.Fatal("malformed GOT flags mutated input")
				}
			})
		}
	})
}

func TestAnalyzeExactR29PLTRecoveryCall(t *testing.T) {
	const toolchain = "/opt/homebrew/share/android-ndk/toolchains/llvm/prebuilt/darwin-x86_64/bin/"
	clang, readelf := toolchain+"aarch64-linux-android29-clang", toolchain+"llvm-readelf"
	if _, err := os.Stat(clang); err != nil {
		t.Skipf("Android NDK r29 clang unavailable: %v", err)
	}
	dir, err := os.MkdirTemp("/tmp", "vmpacker-r29-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source := `#include <setjmp.h>
extern int benign_external(int);
__attribute__((visibility("default"), noinline)) int selected_recovery(void) { jmp_buf env; return setjmp(env); }
__attribute__((visibility("default"), noinline)) int selected_benign(int value) { return benign_external(value); }
`
	sourcePath, soPath := dir+"/fixture.c", dir+"/fixture.so"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(clang, "-shared", "-fPIC", "-O0", "-Wl,--build-id=none", "-o", soPath, sourcePath).CombinedOutput(); err != nil {
		t.Fatalf("r29 compile: %v\n%s", err, output)
	}
	output, err := exec.Command(readelf, "-SW", "-rW", soPath).CombinedOutput()
	if err != nil {
		t.Fatalf("llvm-readelf: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte(".rela.plt")) || !bytes.Contains(output, []byte("R_AARCH64_JUMP_SLOT")) {
		t.Fatalf("unexpected r29 relocation form:\n%s", output)
	}
	t.Logf("exact r29 relocation evidence:\n%s", output)
	input, err := os.ReadFile(soPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(Request{Input: input, Selections: []SelectionRequest{{Name: "selected_recovery"}}}); err == nil || !strings.Contains(err.Error(), `recovery API "setjmp" at 0x469C`) {
		t.Fatalf("selected_recovery err=%v", err)
	}
	if _, err := Analyze(Request{Input: input, Selections: []SelectionRequest{{Name: "selected_benign"}}}); err != nil {
		t.Fatalf("selected_benign: %v", err)
	}

	meta, err := parseELFMetadata(input, AndroidModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.file.Close()
	var plt, relaPLT sectionMetadata
	for _, section := range meta.sections {
		switch section.name {
		case ".plt":
			plt = section
		case ".rela.plt":
			relaPLT = section
		}
	}
	names, err := relocationSymbolNames(meta, relaPLT.link)
	if err != nil {
		t.Fatal(err)
	}
	var relocationNames []string
	setjmpEntry, benignEntry := -1, -1
	for entry, off := 0, relaPLT.off; off < relaPLT.off+relaPLT.size; entry, off = entry+1, off+relaPLT.entsize {
		info := binary.LittleEndian.Uint64(input[off+8 : off+16])
		name := names[uint32(info>>32)]
		relocationNames = append(relocationNames, name)
		switch name {
		case "setjmp":
			setjmpEntry = entry
		case "benign_external":
			benignEntry = entry
		}
	}
	const wantNames = "__cxa_finalize,__cxa_atexit,__register_atfork,setjmp,benign_external"
	if strings.Join(relocationNames, ",") != wantNames || setjmpEntry < 0 || benignEntry < 0 {
		t.Fatalf("unexpected exact r29 .rela.plt names: %v", relocationNames)
	}
	swapped := append([]byte(nil), input...)
	setjmpOff := int(plt.off) + 32 + setjmpEntry*16
	benignOff := int(plt.off) + 32 + benignEntry*16
	setjmpStub := append([]byte(nil), swapped[setjmpOff:setjmpOff+16]...)
	copy(swapped[setjmpOff:setjmpOff+16], swapped[benignOff:benignOff+16])
	copy(swapped[benignOff:benignOff+16], setjmpStub)
	if _, err := Analyze(Request{Input: swapped, Selections: []SelectionRequest{{Name: "selected_benign"}}}); err == nil || !strings.Contains(err.Error(), "does not resolve JUMP_SLOT") {
		t.Fatalf("swapped selected_benign err=%v", err)
	}
}

func TestAnalyzeRejectsJumpSlotOutsideValidatedPLT(t *testing.T) {
	const entry = 0x1200
	t.Run("renamed-plt-relocations", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			code:   []uint32{encodeBranch(0x94000000, entry, 0x1320), 0xD503201F, 0xD65F03C0},
			dynsym: []fixtureSymbol{{name: "benign_external", undefined: true}},
			relocs: []fixtureRelocation{{name: "benign_external", type_: elf.R_AARCH64_JUMP_SLOT, plt: true}},
		})
		shstrOff := int(binary.LittleEndian.Uint64(fixture.data[fixture.sectionOff[".shstrtab"]+24:]))
		nameOff := int(binary.LittleEndian.Uint32(fixture.data[fixture.sectionOff[".rela.plt"]:]))
		copy(fixture.data[shstrOff+nameOff:], ".rela.bad")
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), `relocation section ".rela.bad" contains JUMP_SLOT outside a validated PLT context`) {
			t.Fatalf("renamed PLT relocation err=%v", err)
		}
	})

	t.Run("non-plt-relocations", func(t *testing.T) {
		fixture := buildELFFixture(fixtureOptions{
			dynsym: []fixtureSymbol{{name: "benign_external", undefined: true}},
			relocs: []fixtureRelocation{{name: "benign_external", offset: entry, type_: elf.R_AARCH64_JUMP_SLOT}},
		})
		if _, err := analyzeFixture(t, fixture, addressSelection(entry, entry+12), "auto"); err == nil || !strings.Contains(err.Error(), `relocation section ".rela.text" contains JUMP_SLOT outside a validated PLT context`) {
			t.Fatalf("non-PLT JUMP_SLOT err=%v", err)
		}
	})
}

func TestAnalyzeRejectsMalformedRelocations(t *testing.T) {
	base := buildELFFixture(fixtureOptions{
		code:   []uint32{0x94000000, 0xD503201F, 0xD65F03C0},
		dynsym: []fixtureSymbol{{name: "setjmp", undefined: true}},
		relocs: []fixtureRelocation{{name: "setjmp", offset: 0x1200, type_: elf.R_AARCH64_CALL26}},
	})
	relocationHeader := base.sectionOff[".rela.text"]
	relocationOff := int(binary.LittleEndian.Uint64(base.data[relocationHeader+24 : relocationHeader+32]))
	cases := map[string]func([]byte){
		"entry-size": func(data []byte) { binary.LittleEndian.PutUint64(data[relocationHeader+56:], 8) },
		"symbol-index": func(data []byte) {
			binary.LittleEndian.PutUint64(data[relocationOff+8:], uint64(99)<<32|uint64(elf.R_AARCH64_CALL26))
		},
		"offset-bounds": func(data []byte) {
			binary.LittleEndian.PutUint64(data[relocationOff:], 0x9000)
		},
		"instruction-kind": func(data []byte) { binary.LittleEndian.PutUint32(data[0x200:], 0xD503201F) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := append([]byte(nil), base.data...)
			mutate(data)
			if _, err := Analyze(Request{Input: data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}}); err == nil {
				t.Fatal("accepted malformed relocation")
			}
		})
	}
}

func TestProcessRejectsExternalTailBeforeMutation(t *testing.T) {
	const entry = 0x1200
	cases := map[string]fixtureOptions{
		"forward-known": {
			code:   []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry+12), 0xD65F03C0},
			symtab: []fixtureSymbol{{name: "tail", addr: entry + 12, size: 4}},
		},
		"backward-known": {
			code:   []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry-4)},
			symtab: []fixtureSymbol{{name: "tail", addr: entry - 4, size: 4}},
		},
		"mapped-unknown": {
			code:   []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry+16), 0xD65F03C0, 0xD65F03C0},
			symtab: []fixtureSymbol{{name: "boundary", addr: entry + 12, size: 4}},
		},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			input := buildELFFixture(options).data
			before := append([]byte(nil), input...)
			result, err := Process(Request{Input: input, Selections: []SelectionRequest{addressSelection(entry, 0)}})
			if err == nil || !strings.Contains(err.Error(), "unsupported external unconditional branch") || len(result.Artifact) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if !bytes.Equal(input, before) {
				t.Fatal("Process mutated external-tail input")
			}
		})
	}
}

func TestAnalyzeZeroSizeSymbolUsesCFG(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, symtab: []fixtureSymbol{{name: "target"}}})
	analysis, err := analyzeFixture(t, fixture, SelectionRequest{Name: "target"}, "auto")
	if err != nil || analysis.Selections[0].Address != 0x1200 || analysis.Selections[0].End != 0x120c {
		t.Fatalf("selection=%+v err=%v", analysis.Selections, err)
	}
}

func TestAmbiguousAdjacentSymbolStillBoundsCFG(t *testing.T) {
	const entry = 0x1200
	fixture := buildELFFixture(fixtureOptions{
		dynamic: true,
		code: []uint32{
			0xD503201F, 0xD503201F, 0xD65F03C0,
			0xD503201F, 0xD65F03C0,
		},
		symtab: []fixtureSymbol{{name: "target"}, {name: "adjacent", addr: entry + 12, size: 8}},
		dynsym: []fixtureSymbol{{name: "adjacent", addr: entry + 16, size: 4}},
	})
	analysis, err := analyzeFixture(t, fixture, SelectionRequest{Name: "target"}, "auto")
	if err != nil || len(analysis.Selections) != 1 || analysis.Selections[0].End != entry+12 {
		t.Fatalf("selection=%+v err=%v", analysis.Selections, err)
	}
	if _, err := analyzeFixture(t, fixture, SelectionRequest{Name: "adjacent"}, "auto"); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("ambiguous named lookup err=%v", err)
	}
}

func TestAnalyzeExplicitRanges(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: make([]uint32, 8)})
	for name, selection := range map[string]SelectionRequest{
		"unaligned-start": addressSelection(0x1201, 0x1210),
		"unaligned-end":   addressSelection(0x1200, 0x120d),
		"too-short":       addressSelection(0x1200, 0x1208),
		"out-of-map":      addressSelection(0x1200, 0x9000),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := analyzeFixture(t, fixture, selection, "auto"); err == nil {
				t.Fatal("accepted invalid range")
			}
		})
	}
	overlap := []SelectionRequest{addressSelection(0x1200, 0x1210), addressSelection(0x120c, 0x1220)}
	if _, err := Analyze(Request{Input: fixture.data, Selections: overlap}); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlap err=%v", err)
	}
}

func encodeBranch(op uint32, from, to uint64) uint32 {
	delta := int64(to) - int64(from)
	return op | uint32((delta>>2)&0x03ffffff)
}

func encodeConditional(from, to uint64) uint32 {
	delta := int64(to) - int64(from)
	return 0x54000000 | uint32((delta>>2)&0x7ffff)<<5
}

func TestCFGInference(t *testing.T) {
	const entry = 0x1200
	cases := []struct {
		name      string
		code      []uint32
		symbols   []fixtureSymbol
		wantEnd   uint64
		wantError string
	}{
		{name: "straightline", code: []uint32{0xD503201F, 0xD503201F, 0xD65F03C0}, wantEnd: entry + 12},
		{name: "multiple-returns", code: []uint32{encodeConditional(entry, entry+8), 0xD65F03C0, 0xD65F03C0}, wantEnd: entry + 12},
		{name: "loop", code: []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry)}, wantEnd: entry + 12},
		{name: "call-fallthrough", code: []uint32{encodeBranch(0x94000000, entry, entry+0x100), 0xD503201F, 0xD65F03C0}, wantEnd: entry + 12},
		{name: "tail-known-symbol", code: []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry+12), 0xD65F03C0}, symbols: []fixtureSymbol{{name: "next", addr: entry + 12, size: 4}}, wantError: "unsupported external unconditional branch"},
		{name: "backward-tail-known-symbol", code: []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry-4)}, symbols: []fixtureSymbol{{name: "previous", addr: entry - 4, size: 4}}, wantError: "unsupported external unconditional branch"},
		{name: "unconditional-mapped-unknown", code: []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, entry+16), 0xD65F03C0, 0xD65F03C0}, symbols: []fixtureSymbol{{name: "boundary", addr: entry + 12, size: 4}}, wantError: "unsupported external unconditional branch"},
		{name: "unconditional-unmapped", code: []uint32{0xD503201F, 0xD503201F, encodeBranch(0x14000000, entry+8, 0x9000)}, wantError: "unsupported external unconditional branch"},
		{name: "indirect", code: []uint32{0xD503201F, 0xD503201F, 0xD61F0000}, wantError: "explicit START-END required"},
		{name: "gap", code: []uint32{encodeBranch(0x14000000, entry, entry+8), 0xD503201F, 0xD65F03C0}, wantError: "gap"},
		{name: "unknown", code: []uint32{0xD503201F, 0xD503201F, 0xffffffff}, wantError: "unsupported"},
		{name: "conditional-out", code: []uint32{encodeConditional(entry, entry+0x100), 0xD503201F, 0xD65F03C0}, wantError: "unresolved"},
		{name: "absorbs-unknown", code: []uint32{encodeBranch(0x14000000, entry, entry+16), 0xD503201F, 0xD503201F, 0xD503201F, 0xD65F03C0}, wantError: "gap"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildELFFixture(fixtureOptions{dynamic: true, code: test.code, symtab: test.symbols})
			analysis, err := analyzeFixture(t, fixture, addressSelection(entry, 0), "auto")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || analysis.Selections[0].End != test.wantEnd {
				t.Fatalf("selection=%+v err=%v", analysis.Selections, err)
			}
		})
	}
}

func TestOverlapFailureRetainsPartialAnalysis(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: make([]uint32, 8)})
	selections := []SelectionRequest{addressSelection(0x1200, 0x1210), addressSelection(0x120c, 0x1220)}
	analysis, err := Analyze(Request{Input: fixture.data, Selections: selections})
	if err == nil || analysis.TargetKind != TargetKindAndroidSO || len(analysis.Limitations) == 0 || len(analysis.Selections) != 1 {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
	result, err := Process(Request{Input: fixture.data, Selections: selections})
	if err == nil || result.TargetKind != TargetKindAndroidSO || len(result.AnalysisLimitations) == 0 || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMissingRuntimeImageFailsBeforeMutation(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	input := append([]byte(nil), fixture.data...)
	before := append([]byte(nil), input...)
	result, err := Process(Request{
		Input: input, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)},
	})
	if err == nil || !strings.Contains(err.Error(), "runtime image") || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("Process mutated input without a runtime image")
	}
}

func TestValidatedRuntimeProducesRewrittenArtifact(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	input := append([]byte(nil), fixture.data...)
	before := append([]byte(nil), input...)
	opcodes := vm.IdentityOpcodeMap()
	result, err := Process(Request{
		Input: input, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)},
		Opcodes: opcodes, RuntimeImage: rewritePlanRuntimeImage(t, opcodes),
	})
	if err != nil || result.DevelopmentStrategy != "rewrite-artifact-ready" ||
		result.RuntimeStrategy != "ndk-r29-et-rel-validated" || result.OpcodeMapDigest == "" || len(result.Artifact) == 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("rewrite writer mutated input")
	}
	assertRewrittenArtifactParses(t, result.Artifact, result.TargetKind)
}

func TestAnalyzeLimitsAndMaliciousSymbolArithmetic(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	requests := make([]SelectionRequest, maxSelections)
	for i := range requests {
		requests[i] = addressSelection(0x1200, 0x120c)
	}
	if _, err := Analyze(Request{Input: fixture.data, Selections: requests}); err != nil {
		t.Fatalf("4096 selections: %v", err)
	}
	requests = append(requests, addressSelection(0x1200, 0x120c))
	if _, err := Analyze(Request{Input: fixture.data, Selections: requests}); err == nil {
		t.Fatal("accepted 4097 selections")
	}
	malicious := buildELFFixture(fixtureOptions{dynamic: true, symtab: []fixtureSymbol{{name: "bad", addr: math.MaxUint64 - 1, size: 8}}})
	if _, err := analyzeFixture(t, malicious, SelectionRequest{Name: "bad"}, "auto"); err == nil {
		t.Fatal("accepted overflowing symbol")
	}
}

func TestBytecodeBoundariesAndFailClosedHelpers(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	movImmWire := mustOpcodeWire(t, opcodes, vm.OpMovImm)
	jmpWire := mustOpcodeWire(t, opcodes, vm.OpJmp)
	unknownWire := findUnassignedWire(t, opcodes)

	if err := validateFinalBytecodeSize(64 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := validateFinalBytecodeSize(64*1024 + 1); err == nil {
		t.Fatal("accepted bytecode over 64 KiB")
	}
	if _, _, err := reverseInstructions([]byte{unknownWire}, 1, opcodes); err == nil {
		t.Fatal("reverse accepted unknown opcode")
	}
	if _, _, err := reverseInstructions([]byte{movImmWire}, 1, opcodes); err == nil {
		t.Fatal("reverse accepted truncated instruction")
	}
	if err := encryptOpcodes([]byte{unknownWire}, 1, 1, false, opcodes); err == nil {
		t.Fatal("encrypt accepted unknown opcode")
	}
	branch := []byte{jmpWire, 1, 0, 0, 0, 5}
	if err := (&Packer{opcodes: opcodes}).remapBranchTargets(branch, len(branch), map[int]int{}); err == nil {
		t.Fatal("remap accepted unresolved target")
	}
}

func TestBytecodeHelpersUseOpcodeMapAndPreserveNonOpcodes(t *testing.T) {
	opcodes, err := vm.NewOpcodeMap(bytes.NewReader(make([]byte, 4096)))
	if err != nil {
		t.Fatalf("NewOpcodeMap: %v", err)
	}
	movImmWire := mustOpcodeWire(t, opcodes, vm.OpMovImm)
	jmpWire := mustOpcodeWire(t, opcodes, vm.OpJmp)
	haltWire := mustOpcodeWire(t, opcodes, vm.OpHalt)

	forward := []byte{
		movImmWire, 0x01, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
		jmpWire, 0x00, 0x00, 0x00, 0x00,
		haltWire,
	}
	reversed, offsets, err := reverseInstructions(forward, len(forward), opcodes)
	if err != nil {
		t.Fatalf("reverseInstructions: %v", err)
	}
	wantBeforeRemap := []byte{
		haltWire, 1,
		jmpWire, 0x00, 0x00, 0x00, 0x00, 5,
		movImmWire, 0x01, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 10,
	}
	if !bytes.Equal(reversed, wantBeforeRemap) {
		t.Fatalf("reverse changed wire/operand/marker bytes:\n got % x\nwant % x", reversed, wantBeforeRemap)
	}

	if err := (&Packer{opcodes: opcodes}).remapBranchTargets(reversed, len(reversed), offsets); err != nil {
		t.Fatalf("remapBranchTargets: %v", err)
	}
	if got, want := binary.LittleEndian.Uint32(reversed[3:7]), uint32(offsets[0]); got != want {
		t.Fatalf("remapped target=%d, want %d", got, want)
	}
	if reversed[0] != haltWire || reversed[1] != 1 || reversed[2] != jmpWire || reversed[7] != 5 ||
		reversed[8] != movImmWire || reversed[18] != 10 {
		t.Fatalf("remap changed opcode or raw marker: % x", reversed)
	}
	if !bytes.Equal(reversed[9:18], forward[1:10]) {
		t.Fatalf("remap changed non-branch operands: got % x want % x", reversed[9:18], forward[1:10])
	}

	beforeEncrypt := append([]byte(nil), reversed...)
	const key = uint32(0x12345678)
	if err := encryptOpcodes(reversed, len(reversed), key, true, opcodes); err != nil {
		t.Fatalf("encryptOpcodes: %v", err)
	}
	opcodeOffsets := map[int]bool{0: true, 2: true, 8: true}
	for i := range reversed {
		if opcodeOffsets[i] {
			want := beforeEncrypt[i] ^ byte(key^(uint32(i)*0x9E3779B9))
			if reversed[i] != want {
				t.Fatalf("encrypted opcode[%d]=0x%02x, want 0x%02x", i, reversed[i], want)
			}
			continue
		}
		if reversed[i] != beforeEncrypt[i] {
			t.Fatalf("encrypt changed non-opcode byte[%d]: 0x%02x -> 0x%02x", i, beforeEncrypt[i], reversed[i])
		}
	}
}

func mustOpcodeWire(t *testing.T, opcodes vm.OpcodeMap, op vm.Opcode) byte {
	t.Helper()
	wire, err := opcodes.Wire(op)
	if err != nil {
		t.Fatalf("Wire(%d): %v", op, err)
	}
	return wire
}

func findUnassignedWire(t *testing.T, opcodes vm.OpcodeMap) byte {
	t.Helper()
	for i := 0; i < 256; i++ {
		wire := byte(i)
		if _, err := opcodes.Decode(wire); err != nil {
			return wire
		}
	}
	t.Fatal("opcode map assigned every wire value")
	return 0
}

func FuzzAnalyzeNoPanic(f *testing.F) {
	valid := buildELFFixture(fixtureOptions{dynamic: true})
	relocated := buildELFFixture(fixtureOptions{
		code:   []uint32{0x94000000, 0xD503201F, 0xD65F03C0},
		dynsym: []fixtureSymbol{{name: "setjmp", undefined: true}},
		relocs: []fixtureRelocation{{name: "setjmp", offset: 0x1200, type_: elf.R_AARCH64_CALL26}},
	})
	f.Add([]byte{})
	f.Add([]byte("not ELF"))
	f.Add(valid.data)
	f.Add(relocated.data)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = Analyze(Request{Input: data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}})
	})
}
