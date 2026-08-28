package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type functionSymbol struct {
	name        string
	addr        uint64
	size        uint64
	section     elf.SectionIndex
	sectionName string
	source      string
	sources     []string
}

type relocationSymbol struct {
	name    string
	gotSlot uint64
	err     error
}

type symbolIndex struct {
	byName          map[string][]functionSymbol
	byAddr          map[uint64][]functionSymbol
	starts          map[uint64]bool
	relocatedAt     map[uint64][]string
	relocatedTarget map[uint64][]string
	pltRanges       []fileRange
	ordered         []functionSymbol
	problems        map[string]error
	relocationErrAt map[uint64]error
	relocationErrTo map[uint64]error
}

func readFunctionSymbols(meta *elfMetadata) (*symbolIndex, error) {
	index := &symbolIndex{
		byName: make(map[string][]functionSymbol), byAddr: make(map[uint64][]functionSymbol),
		starts: make(map[uint64]bool), relocatedAt: make(map[uint64][]string),
		relocatedTarget: make(map[uint64][]string), problems: make(map[string]error),
		relocationErrAt: make(map[uint64]error), relocationErrTo: make(map[uint64]error),
	}
	if err := index.readTable(meta, "symtab", meta.file.Symbols); err != nil {
		return nil, err
	}
	if err := index.readTable(meta, "dynsym", meta.file.DynamicSymbols); err != nil {
		return nil, err
	}
	if err := index.readRelocations(meta); err != nil {
		return nil, err
	}
	for name, defs := range index.byName {
		merged, err := mergeSymbolDefinitions(name, defs)
		if err != nil {
			index.problems[name] = err
			delete(index.byName, name)
			continue
		}
		index.byName[name] = merged
	}
	sort.Slice(index.ordered, func(i, j int) bool {
		if index.ordered[i].addr != index.ordered[j].addr {
			return index.ordered[i].addr < index.ordered[j].addr
		}
		return index.ordered[i].name < index.ordered[j].name
	})
	return index, nil
}

func (index *symbolIndex) readTable(meta *elfMetadata, source string, read func() ([]elf.Symbol, error)) error {
	symbols, err := read()
	if errors.Is(err, elf.ErrNoSymbols) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s failed: %w", source, err)
	}
	for _, symbol := range symbols {
		if elf.ST_TYPE(symbol.Info) != elf.STT_FUNC || symbol.Section == elf.SHN_UNDEF {
			continue
		}
		definition := functionSymbol{
			name: symbol.Name, addr: symbol.Value, size: symbol.Size, section: symbol.Section,
			source: source, sources: []string{source},
		}
		sectionName, err := validateFunctionSymbol(meta, symbol)
		if err != nil {
			definition.sectionName = "invalid: " + err.Error()
		} else {
			definition.sectionName = sectionName
			index.byAddr[definition.addr] = append(index.byAddr[definition.addr], definition)
			index.starts[definition.addr] = true
			index.ordered = append(index.ordered, definition)
		}
		index.byName[symbol.Name] = append(index.byName[symbol.Name], definition)
	}
	return nil
}

func (index *symbolIndex) readRelocations(meta *elfMetadata) error {
	symbolTables := make(map[uint32][]string)
	var pltRelocations []relocationSymbol
	var lastPLTOffset uint64
	for _, section := range meta.sections {
		if section.type_ != elf.SHT_REL && section.type_ != elf.SHT_RELA || section.size == 0 {
			continue
		}
		target := meta.sections[section.info]
		isPLT := section.name == ".rela.plt" || section.name == ".rel.plt"
		if isPLT {
			if len(pltRelocations) != 0 {
				return fmt.Errorf("multiple PLT relocation sections are ambiguous")
			}
			gotPLTCount := 0
			for _, candidate := range meta.sections {
				if candidate.name == ".got.plt" {
					gotPLTCount++
				}
			}
			if gotPLTCount != 1 || target.name != ".got.plt" {
				return fmt.Errorf("relocation section %q must target exactly one .got.plt section", section.name)
			}
			wantFlags := elf.SHF_ALLOC | elf.SHF_WRITE
			if target.type_ != elf.SHT_PROGBITS || target.flags != wantFlags ||
				target.addralign < 8 || target.addr%target.addralign != 0 || target.off%target.addralign != 0 {
				return fmt.Errorf("relocation section %q target .got.plt has unsupported flags or alignment", section.name)
			}
			targetEnd, ok := checkedAdd(target.addr, target.size)
			if !ok {
				return fmt.Errorf("relocation section %q target .got.plt address range overflows", section.name)
			}
			mapping, ok := meta.fileBackedMapping(target.addr, targetEnd)
			mappedOff, mapped := mappingFileOffset(mapping, target.addr)
			if !ok || mapping.flags&elf.PF_W == 0 || mapping.flags&elf.PF_X != 0 || !mapped || mappedOff != target.off {
				return fmt.Errorf("relocation section %q target .got.plt is not in one writable, non-executable file-backed PT_LOAD", section.name)
			}
		}
		names := symbolTables[section.link]
		if names == nil {
			var err error
			names, err = relocationSymbolNames(meta, section.link)
			if err != nil {
				return fmt.Errorf("relocation section %q: %w", section.name, err)
			}
			symbolTables[section.link] = names
		}
		for entry, off := 0, section.off; off < section.off+section.size; entry, off = entry+1, off+section.entsize {
			rOffset := binary.LittleEndian.Uint64(meta.data[off : off+8])
			info := binary.LittleEndian.Uint64(meta.data[off+8 : off+16])
			symbol := uint32(info >> 32)
			kind := elf.R_AARCH64(uint32(info))
			if kind == elf.R_AARCH64_JUMP_SLOT && !isPLT {
				return fmt.Errorf("relocation section %q contains JUMP_SLOT outside a validated PLT context", section.name)
			}
			if uint64(symbol) >= uint64(len(names)) {
				return fmt.Errorf("relocation section %q entry %d has symbol index %d outside linked table", section.name, entry, symbol)
			}
			name := names[symbol]

			if isPLT {
				address, err := relocationAddress(target, rOffset, 8)
				if err != nil {
					return fmt.Errorf("relocation section %q entry %d: %w", section.name, entry, err)
				}
				if address%8 != 0 {
					return fmt.Errorf("relocation section %q entry %d has unaligned PLT slot 0x%X", section.name, entry, address)
				}
				if entry != 0 && address != lastPLTOffset+8 {
					return fmt.Errorf("relocation section %q entry %d has non-sequential PLT slot 0x%X", section.name, entry, address)
				}
				lastPLTOffset = address
				if kind != elf.R_AARCH64_JUMP_SLOT || name == "" {
					pltRelocations = append(pltRelocations, relocationSymbol{err: fmt.Errorf("unresolved PLT relocation %s with symbol index %d", kind, symbol)})
				} else {
					pltRelocations = append(pltRelocations, relocationSymbol{name: name, gotSlot: address})
				}
				continue
			}
			if kind != elf.R_AARCH64_CALL26 && kind != elf.R_AARCH64_JUMP26 {
				continue
			}
			address, err := relocationAddress(target, rOffset, 4)
			if err != nil {
				return fmt.Errorf("relocation section %q entry %d: %w", section.name, entry, err)
			}
			if address%4 != 0 {
				return fmt.Errorf("relocation section %q entry %d has unaligned call site 0x%X", section.name, entry, address)
			}
			mapping, ok := meta.executableMapping(address, address+4)
			if !ok {
				return fmt.Errorf("relocation section %q entry %d call site 0x%X is not executable", section.name, entry, address)
			}
			fileOff, ok := mappingFileOffset(mapping, address)
			if !ok || fileOff > uint64(len(meta.data))-4 {
				return fmt.Errorf("relocation section %q entry %d call site 0x%X is not file-backed", section.name, entry, address)
			}
			raw := binary.LittleEndian.Uint32(meta.data[fileOff : fileOff+4])
			if kind == elf.R_AARCH64_CALL26 && raw&0xfc000000 != 0x94000000 ||
				kind == elf.R_AARCH64_JUMP26 && raw&0xfc000000 != 0x14000000 {
				return fmt.Errorf("relocation section %q entry %d does not match the instruction at 0x%X", section.name, entry, address)
			}
			if len(index.relocatedAt[address]) != 0 || index.relocationErrAt[address] != nil {
				return fmt.Errorf("multiple direct relocations apply at 0x%X", address)
			}
			if name == "" {
				index.relocationErrAt[address] = fmt.Errorf("direct relocation at 0x%X has no named symbol", address)
				continue
			}
			index.relocatedAt[address] = []string{name}
		}
	}

	if len(pltRelocations) == 0 {
		return nil
	}
	return index.bindPLTRelocations(meta, pltRelocations)
}

func relocationSymbolNames(meta *elfMetadata, sectionIndex uint32) ([]string, error) {
	section := meta.sections[sectionIndex]
	if section.type_ != elf.SHT_SYMTAB && section.type_ != elf.SHT_DYNSYM {
		return nil, fmt.Errorf("sh_link section %d is not a symbol table", sectionIndex)
	}
	stringsSection := meta.sections[section.link]
	if stringsSection.type_ != elf.SHT_STRTAB {
		return nil, fmt.Errorf("symbol table %d does not link to a string table", sectionIndex)
	}
	symbolData := meta.data[section.off : section.off+section.size]
	stringsData := meta.data[stringsSection.off : stringsSection.off+stringsSection.size]
	names := make([]string, len(symbolData)/24)
	for i := range names {
		nameOffset := binary.LittleEndian.Uint32(symbolData[i*24 : i*24+4])
		if uint64(nameOffset) >= uint64(len(stringsData)) {
			return nil, fmt.Errorf("symbol table %d entry %d name offset is out of bounds", sectionIndex, i)
		}
		end := bytes.IndexByte(stringsData[nameOffset:], 0)
		if end < 0 {
			return nil, fmt.Errorf("symbol table %d entry %d name is not NUL-terminated", sectionIndex, i)
		}
		names[i] = string(stringsData[nameOffset : uint32(end)+nameOffset])
	}
	return names, nil
}

func relocationAddress(section sectionMetadata, offset, width uint64) (uint64, error) {
	sectionEnd, ok := checkedAdd(section.addr, section.size)
	if !ok {
		return 0, fmt.Errorf("target section address range overflows")
	}
	end, ok := checkedAdd(offset, width)
	if !ok || offset < section.addr || end > sectionEnd {
		return 0, fmt.Errorf("relocation offset 0x%X is outside target section %q", offset, section.name)
	}
	return offset, nil
}

func (index *symbolIndex) bindPLTRelocations(meta *elfMetadata, relocations []relocationSymbol) error {
	var plt sectionMetadata
	found := false
	for _, section := range meta.sections {
		switch section.name {
		case ".plt":
			if found {
				return fmt.Errorf("PLT relocations require exactly one executable .plt section")
			}
			plt, found = section, true
		case ".plt.sec":
			return fmt.Errorf("PLT section %q has unsupported layout", section.name)
		}
	}
	if !found {
		return fmt.Errorf("PLT relocations require exactly one executable .plt section")
	}
	if plt.flags&elf.SHF_EXECINSTR == 0 || plt.addr%16 != 0 || plt.size%16 != 0 {
		return fmt.Errorf("PLT section %q has unsupported executable layout", plt.name)
	}
	const headerSize = uint64(32)
	count := uint64(len(relocations))
	if count > (^uint64(0)-headerSize)/16 {
		return fmt.Errorf("PLT relocation count overflows section size")
	}
	wantSize := headerSize + count*16
	if plt.size != wantSize {
		return fmt.Errorf("PLT section %q size 0x%X does not match %d relocation entries", plt.name, plt.size, len(relocations))
	}
	pltEnd, ok := checkedAdd(plt.addr, plt.size)
	if !ok {
		return fmt.Errorf("PLT section %q address range overflows", plt.name)
	}
	if _, ok := meta.executableMapping(plt.addr, pltEnd); !ok {
		return fmt.Errorf("PLT section %q is not in one executable file-backed PT_LOAD", plt.name)
	}
	index.pltRanges = append(index.pltRanges, fileRange{start: plt.addr, end: pltEnd})
	for i, relocation := range relocations {
		address := plt.addr + headerSize + uint64(i)*16
		if relocation.err != nil {
			index.relocationErrTo[address] = relocation.err
			continue
		}
		entryEnd, ok := checkedAdd(address, 16)
		if !ok {
			index.relocationErrTo[address] = fmt.Errorf("PLT entry at 0x%X address range overflows", address)
			continue
		}
		mapping, ok := meta.executableMapping(address, entryEnd)
		if !ok {
			index.relocationErrTo[address] = fmt.Errorf("PLT entry at 0x%X has unsupported AArch64 layout", address)
			continue
		}
		off, ok := mappingFileOffset(mapping, address)
		if !ok || off > uint64(len(meta.data)) || uint64(len(meta.data))-off < 16 ||
			!isAArch64PLTEntry(meta.data[off:off+16], address, relocation.gotSlot) {
			index.relocationErrTo[address] = fmt.Errorf("PLT entry at 0x%X does not resolve JUMP_SLOT 0x%X", address, relocation.gotSlot)
			continue
		}
		index.relocatedTarget[address] = appendUnique(index.relocatedTarget[address], relocation.name)
	}
	return nil
}

func isAArch64PLTEntry(code []byte, address, gotSlot uint64) bool {
	if len(code) != 16 || address%4 != 0 || gotSlot%8 != 0 {
		return false
	}
	adrp := binary.LittleEndian.Uint32(code[0:4])
	ldr := binary.LittleEndian.Uint32(code[4:8])
	add := binary.LittleEndian.Uint32(code[8:12])
	br := binary.LittleEndian.Uint32(code[12:16])
	if adrp&0x9f00001f != 0x90000010 || ldr&0xffc003ff != 0xf9400211 ||
		add&0xffc003ff != 0x91000210 || br != 0xd61f0220 {
		return false
	}

	immediate := int64(((adrp>>5)&0x7ffff)<<2 | ((adrp >> 29) & 3))
	if immediate&(1<<20) != 0 {
		immediate -= 1 << 21
	}
	gotPage, ok := branchAddress(address&^uint64(0xfff), immediate<<12)
	ldrOffset := uint64((ldr>>10)&0xfff) * 8
	addOffset := uint64((add >> 10) & 0xfff)
	if !ok || ldrOffset != addOffset {
		return false
	}
	resolved, ok := checkedAdd(gotPage, addOffset)
	return ok && resolved == gotSlot
}

func validateFunctionSymbol(meta *elfMetadata, symbol elf.Symbol) (string, error) {
	sectionName := "__LOAD_X"
	if symbol.Section < elf.SHN_LORESERVE {
		if int(symbol.Section) >= len(meta.sections) {
			return "", fmt.Errorf("section index %d is out of range", symbol.Section)
		}
		sectionName = meta.sections[symbol.Section].name
	}
	size := symbol.Size
	if size == 0 {
		size = 4
	}
	end, ok := checkedAdd(symbol.Value, size)
	if !ok {
		return "", fmt.Errorf("address range overflows")
	}
	if _, ok := meta.executableMapping(symbol.Value, end); !ok {
		return "", fmt.Errorf("address range is not in one executable file-backed PT_LOAD")
	}
	return sectionName, nil
}

func mergeSymbolDefinitions(name string, defs []functionSymbol) ([]functionSymbol, error) {
	var merged []functionSymbol
	for _, definition := range defs {
		if strings.HasPrefix(definition.sectionName, "invalid: ") {
			return nil, fmt.Errorf("function %q has an invalid %s definition: %s", name, definition.source, strings.TrimPrefix(definition.sectionName, "invalid: "))
		}
		matched := false
		for i := range merged {
			if merged[i].addr != definition.addr || (merged[i].size != 0 && definition.size != 0 && merged[i].size != definition.size) ||
				(merged[i].section != elf.SHN_UNDEF && definition.section != elf.SHN_UNDEF && merged[i].section != definition.section) {
				continue
			}
			if merged[i].size == 0 {
				merged[i].size = definition.size
			}
			merged[i].sources = appendUnique(merged[i].sources, definition.source)
			if definition.source == "symtab" && merged[i].source != "symtab" {
				preferred := definition
				preferred.size = merged[i].size
				preferred.sources = merged[i].sources
				merged[i] = preferred
			}
			matched = true
			break
		}
		if !matched {
			merged = append(merged, definition)
		}
	}
	if len(merged) > 1 {
		return nil, fmt.Errorf("function %q has conflicting symbol definitions across symtab/dynsym", name)
	}
	if len(merged) == 0 && len(defs) != 0 {
		return nil, fmt.Errorf("function %q has no valid executable symbol definition (%s)", name, defs[0].sectionName)
	}
	return merged, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (index *symbolIndex) resolve(name string) (functionSymbol, error) {
	if err := index.problems[name]; err != nil {
		return functionSymbol{}, err
	}
	definitions := index.byName[name]
	if len(definitions) == 0 {
		return functionSymbol{}, fmt.Errorf("function %q not found in symtab or dynsym", name)
	}
	if len(definitions) != 1 {
		return functionSymbol{}, fmt.Errorf("function %q has ambiguous symbol definitions", name)
	}
	return definitions[0], nil
}

func (index *symbolIndex) directTransferNames(address, target uint64) ([]string, error) {
	if err := index.relocationErrAt[address]; err != nil {
		return nil, err
	}
	if err := index.relocationErrTo[target]; err != nil {
		return nil, err
	}
	at := index.relocatedAt[address]
	to := index.relocatedTarget[target]
	if len(at) > 1 || len(to) > 1 {
		return nil, fmt.Errorf("ambiguous relocation-backed direct transfer at 0x%X", address)
	}
	if len(at) == 1 && len(to) == 1 && baseSymbolName(at[0]) != baseSymbolName(to[0]) {
		return nil, fmt.Errorf("conflicting relocation-backed direct transfer at 0x%X", address)
	}
	if len(to) == 0 {
		for _, plt := range index.pltRanges {
			if target >= plt.start && target < plt.end {
				return nil, fmt.Errorf("direct transfer at 0x%X targets unresolved relocation-backed PLT address 0x%X", address, target)
			}
		}
	}
	names := append([]string(nil), at...)
	for _, name := range to {
		names = appendUnique(names, name)
	}
	for _, name := range index.namesAt(target) {
		names = appendUnique(names, name)
	}
	return names, nil
}

func (index *symbolIndex) nextStart(address, mappingEnd uint64) uint64 {
	limit := mappingEnd
	for start := range index.starts {
		if start > address && start < limit {
			limit = start
		}
	}
	return limit
}

func (index *symbolIndex) namesAt(address uint64) []string {
	definitions := index.byAddr[address]
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.name)
	}
	return names
}
