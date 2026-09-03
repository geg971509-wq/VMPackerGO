package runtime

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

var supportedRelocations = map[elf.R_AARCH64]bool{
	elf.R_AARCH64_ABS64:               true,
	elf.R_AARCH64_PREL32:              true,
	elf.R_AARCH64_LD_PREL_LO19:        true,
	elf.R_AARCH64_ADR_PREL_LO21:       true,
	elf.R_AARCH64_ADR_PREL_PG_HI21:    true,
	elf.R_AARCH64_ADR_PREL_PG_HI21_NC: true,
	elf.R_AARCH64_ADD_ABS_LO12_NC:     true,
	elf.R_AARCH64_JUMP26:              true,
	elf.R_AARCH64_CALL26:              true,
	elf.R_AARCH64_LDST64_ABS_LO12_NC:  true,
	elf.R_AARCH64_GOT_LD_PREL19:       true,
}

func ParseImage(object []byte, opcodes vm.OpcodeMap) (*Image, error) {
	if err := opcodes.Validate(); err != nil {
		return nil, fmt.Errorf("validate opcode map: %w", err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		return nil, fmt.Errorf("parse ELF: %w", err)
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Machine != elf.EM_AARCH64 || file.Type != elf.ET_REL {
		return nil, fmt.Errorf("runtime must be little-endian AArch64 ELF64 ET_REL")
	}

	digest, err := opcodes.Digest()
	if err != nil {
		return nil, err
	}
	image := &Image{Object: append([]byte(nil), object...), OpcodeMapDigest: hex.EncodeToString(digest[:])}
	ehFrameIndex := -1
	for index, section := range file.Sections {
		item := Section{
			Index: index, Name: section.Name, Type: section.Type, Flags: section.Flags,
			Alignment: section.Addralign, Size: section.Size, NOBITS: section.Type == elf.SHT_NOBITS,
		}
		if !item.NOBITS && section.Type != elf.SHT_NULL {
			data, err := section.Data()
			if err != nil {
				return nil, fmt.Errorf("read section %q", section.Name)
			}
			item.Data = append([]byte(nil), data...)
		}
		image.Sections = append(image.Sections, item)
		if section.Name == ".eh_frame" {
			ehFrameIndex = index
			image.EHFrame = append([]byte(nil), item.Data...)
		}
		if section.Name == ".note.gnu.property" {
			image.GNUPropertyNote = append([]byte(nil), item.Data...)
		}
	}
	if len(image.EHFrame) == 0 {
		return nil, fmt.Errorf("runtime is missing .eh_frame")
	}
	if ehFrameIndex <= 0 || image.Sections[ehFrameIndex].Flags&elf.SHF_ALLOC == 0 {
		return nil, fmt.Errorf("runtime .eh_frame must be allocatable")
	}
	if err := validateGNUProperty(image.GNUPropertyNote); err != nil {
		return nil, err
	}

	symbols, err := file.Symbols()
	if err != nil {
		return nil, fmt.Errorf("read runtime symbols: %w", err)
	}
	required := map[string]*Symbol{"vm_entry": nil, "vm_entry_token": nil, "vm_native_call": nil, "vm_atomic_native": nil, "_token_table_va": nil, "_image_file_va": nil, "_token_count": nil}
	for index, symbol := range symbols {
		item := Symbol{
			Index: uint32(index + 1), Name: symbol.Name, Info: symbol.Info, Other: symbol.Other,
			Section: symbol.Section, Value: symbol.Value, Size: symbol.Size,
		}
		image.Symbols = append(image.Symbols, item)
		if symbol.Section == elf.SHN_UNDEF && symbol.Name != "" {
			return nil, fmt.Errorf("runtime has unresolved symbol %q", symbol.Name)
		}
		if _, ok := required[symbol.Name]; ok && symbol.Section != elf.SHN_UNDEF {
			if required[symbol.Name] != nil {
				return nil, fmt.Errorf("runtime has duplicate required symbol %q", symbol.Name)
			}
			required[symbol.Name] = &image.Symbols[len(image.Symbols)-1]
		}
	}
	for name, symbol := range required {
		if symbol == nil {
			return nil, fmt.Errorf("runtime is missing required symbol %q", name)
		}
	}
	for _, name := range []string{"vm_entry", "vm_entry_token", "vm_native_call", "vm_atomic_native"} {
		symbol := required[name]
		if elf.ST_TYPE(symbol.Info) != elf.STT_FUNC || int(symbol.Section) <= 0 || int(symbol.Section) >= len(image.Sections) {
			return nil, fmt.Errorf("runtime symbol %q is not a defined function", name)
		}
		section := image.Sections[symbol.Section]
		if section.Flags&(elf.SHF_ALLOC|elf.SHF_EXECINSTR) != elf.SHF_ALLOC|elf.SHF_EXECINSTR {
			return nil, fmt.Errorf("runtime symbol %q is not in executable allocatable storage", name)
		}
	}
	for _, name := range []string{"_token_table_va", "_image_file_va", "_token_count"} {
		symbol := required[name]
		if elf.ST_TYPE(symbol.Info) != elf.STT_OBJECT || int(symbol.Section) <= 0 || int(symbol.Section) >= len(image.Sections) || symbol.Size < 8 {
			return nil, fmt.Errorf("runtime symbol %q is not a defined 8-byte object", name)
		}
		section := image.Sections[symbol.Section]
		if section.Flags&(elf.SHF_ALLOC|elf.SHF_WRITE) != elf.SHF_ALLOC|elf.SHF_WRITE || section.Flags&elf.SHF_EXECINSTR != 0 {
			return nil, fmt.Errorf("runtime symbol %q is not in writable non-executable allocatable storage", name)
		}
	}

	for index, section := range file.Sections {
		if section.Type != elf.SHT_RELA && section.Type != elf.SHT_REL {
			continue
		}
		if section.Info >= uint32(len(file.Sections)) || section.Link >= uint32(len(file.Sections)) {
			return nil, fmt.Errorf("relocation section %q has invalid links", section.Name)
		}
		if file.Sections[section.Link].Type != elf.SHT_SYMTAB {
			return nil, fmt.Errorf("relocation section %q does not reference the static symbol table", section.Name)
		}
		target := file.Sections[section.Info]
		data, err := section.Data()
		if err != nil {
			return nil, fmt.Errorf("read relocation section %q", section.Name)
		}
		entrySize := 16
		if section.Type == elf.SHT_RELA {
			entrySize = 24
		}
		if len(data)%entrySize != 0 {
			return nil, fmt.Errorf("relocation section %q has a partial entry", section.Name)
		}
		for offset := 0; offset < len(data); offset += entrySize {
			rOffset := binary.LittleEndian.Uint64(data[offset:])
			info := binary.LittleEndian.Uint64(data[offset+8:])
			symbolIndex := uint32(info >> 32)
			typeValue := elf.R_AARCH64(uint32(info))
			if !supportedRelocations[typeValue] {
				return nil, fmt.Errorf("runtime uses unsupported relocation %s", typeValue)
			}
			if symbolIndex > uint32(len(symbols)) {
				return nil, fmt.Errorf("relocation references symbol index %d outside the symbol table", symbolIndex)
			}
			width := uint64(4)
			if typeValue == elf.R_AARCH64_ABS64 {
				width = 8
			}
			if rOffset > target.Size || width > target.Size-rOffset {
				return nil, fmt.Errorf("relocation offset 0x%x exceeds target section %q", rOffset, target.Name)
			}
			var addend int64
			if section.Type == elf.SHT_RELA {
				addend = int64(binary.LittleEndian.Uint64(data[offset+16:]))
			}
			image.Relocations = append(image.Relocations, Relocation{
				SectionIndex: uint32(index), TargetIndex: section.Info, Offset: rOffset,
				Type: typeValue, SymbolIndex: symbolIndex, Addend: addend,
			})
		}
	}
	if err := validateRuntimeUnwind(image, ehFrameIndex); err != nil {
		return nil, err
	}
	return image, nil
}

func validateRuntimeUnwind(image *Image, ehFrameIndex int) error {
	var required []*Symbol
	for index := range image.Symbols {
		symbol := &image.Symbols[index]
		if symbol.Name == "vm_entry_token" || symbol.Name == "vm_native_call" || symbol.Name == "vm_atomic_native" || strings.HasPrefix(symbol.Name, "vm_svc_") || strings.HasPrefix(symbol.Name, "vm_exclusive_") || strings.HasPrefix(symbol.Name, "vm_fpsimd_") || (strings.HasPrefix(symbol.Name, "vm_invoke_") && elf.ST_TYPE(symbol.Info) == elf.STT_FUNC) {
			required = append(required, symbol)
		}
	}
	for _, symbol := range required {
		if !hasFDERelocation(image, ehFrameIndex, symbol) {
			return fmt.Errorf("runtime .eh_frame has no FDE relocation for %s", symbol.Name)
		}
	}
	return nil
}

func hasFDERelocation(image *Image, ehFrameIndex int, symbol *Symbol) bool {
	for _, relocation := range image.Relocations {
		if int(relocation.TargetIndex) != ehFrameIndex || relocation.Type != elf.R_AARCH64_PREL32 || relocation.SymbolIndex == 0 || relocation.SymbolIndex > uint32(len(image.Symbols)) {
			continue
		}
		target := image.Symbols[relocation.SymbolIndex-1]
		if target.Name == symbol.Name {
			return true
		}
		if elf.ST_TYPE(target.Info) == elf.STT_SECTION && target.Section == symbol.Section && relocation.Addend >= 0 && uint64(relocation.Addend) == symbol.Value {
			return true
		}
	}
	return false
}

func validateGNUProperty(note []byte) error {
	if len(note) == 0 {
		return fmt.Errorf("runtime is missing AArch64 GNU property note")
	}
	for offset := 0; offset+12 <= len(note); {
		namesz := uint64(binary.LittleEndian.Uint32(note[offset:]))
		descsz := uint64(binary.LittleEndian.Uint32(note[offset+4:]))
		typeValue := binary.LittleEndian.Uint32(note[offset+8:])
		nameStart := uint64(offset + 12)
		nameEnd, ok := checkedAdd(nameStart, namesz)
		if !ok {
			break
		}
		descStart, ok := alignUp(nameEnd, 4)
		if !ok {
			break
		}
		descEnd, ok := checkedAdd(descStart, descsz)
		if !ok || descEnd > uint64(len(note)) {
			break
		}
		if typeValue == 5 && namesz == 4 && bytes.Equal(note[nameStart:nameEnd], []byte("GNU\x00")) {
			desc := note[descStart:descEnd]
			for propertyOffset := uint64(0); propertyOffset+8 <= uint64(len(desc)); {
				propertyType := binary.LittleEndian.Uint32(desc[propertyOffset:])
				propertySize := uint64(binary.LittleEndian.Uint32(desc[propertyOffset+4:]))
				valueStart := propertyOffset + 8
				valueEnd, ok := checkedAdd(valueStart, propertySize)
				if !ok || valueEnd > uint64(len(desc)) {
					break
				}
				if propertyType == 0xc0000000 && propertySize == 4 {
					features := binary.LittleEndian.Uint32(desc[valueStart:valueEnd])
					if features&3 == 3 {
						return nil
					}
				}
				next, ok := alignUp(valueEnd, 8)
				if !ok || next <= propertyOffset {
					break
				}
				propertyOffset = next
			}
		}
		next, ok := alignUp(descEnd, 4)
		if !ok || next <= uint64(offset) || next > math.MaxInt {
			break
		}
		offset = int(next)
	}
	return fmt.Errorf("runtime GNU property note does not require both BTI and PAC")
}

func checkedAdd(a, b uint64) (uint64, bool) {
	if b > math.MaxUint64-a {
		return 0, false
	}
	return a + b, true
}

func alignUp(value, alignment uint64) (uint64, bool) {
	if alignment == 0 {
		return 0, false
	}
	mask := alignment - 1
	if value > math.MaxUint64-mask {
		return 0, false
	}
	return (value + mask) &^ mask, true
}
