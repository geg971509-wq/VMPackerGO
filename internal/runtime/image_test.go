package runtime

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"debug/elf"

	"github.com/vmpacker/internal/vm"
)

func TestParseImageRetainsValidatedObjectState(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	object := buildRuntimeFixture(t, fixtureConfig{relocationType: elf.R_AARCH64_PREL32, features: 3})
	image, err := ParseImage(object, opcodes)
	if err != nil {
		t.Fatalf("ParseImage: %v", err)
	}
	if len(image.Object) != len(object) || len(image.Sections) != 10 || len(image.Symbols) != 5 || len(image.Relocations) != 3 {
		t.Fatalf("incomplete image: sections=%d symbols=%d relocations=%d object=%d", len(image.Sections), len(image.Symbols), len(image.Relocations), len(image.Object))
	}
	if !image.Sections[3].NOBITS || image.Sections[3].Size != 16 || len(image.EHFrame) == 0 || len(image.GNUPropertyNote) == 0 {
		t.Fatalf("section metadata was not retained: %+v", image.Sections[3])
	}
	if err := image.ValidateOpcodeMap(opcodes); err != nil {
		t.Fatalf("ValidateOpcodeMap: %v", err)
	}
	other, err := vm.NewOpcodeMap(bytes.NewReader(make([]byte, 8192)))
	if err != nil {
		t.Fatal(err)
	}
	if err := image.ValidateOpcodeMap(other); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mismatch error=%v", err)
	}
}

func TestParseImageFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		cfg  fixtureConfig
		want string
	}{
		{name: "unknown-relocation", cfg: fixtureConfig{relocationType: elf.R_AARCH64(0xffff), features: 3}, want: "unsupported relocation"},
		{name: "relocation-out-of-range", cfg: fixtureConfig{relocationType: elf.R_AARCH64_PREL32, relocationOffset: 9, features: 3}, want: "exceeds target"},
		{name: "missing-symbol", cfg: fixtureConfig{relocationType: elf.R_AARCH64_PREL32, features: 3, omitTokenSymbol: true}, want: "vm_entry_token"},
		{name: "missing-eh-frame", cfg: fixtureConfig{relocationType: elf.R_AARCH64_PREL32, features: 3, emptyEHFrame: true}, want: ".eh_frame"},
		{name: "bti-only", cfg: fixtureConfig{relocationType: elf.R_AARCH64_PREL32, features: 1}, want: "both BTI and PAC"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseImage(buildRuntimeFixture(t, test.cfg), vm.IdentityOpcodeMap())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want %q", err, test.want)
			}
		})
	}
}

type fixtureConfig struct {
	relocationType   elf.R_AARCH64
	relocationOffset uint64
	features         uint32
	omitTokenSymbol  bool
	emptyEHFrame     bool
}

func buildRuntimeFixture(t *testing.T, cfg fixtureConfig) []byte {
	t.Helper()
	const sectionCount = 10
	shstr := []byte("\x00.text.entry\x00.data.entry\x00.bss\x00.eh_frame\x00.note.gnu.property\x00.rela.eh_frame\x00.symtab\x00.strtab\x00.shstrtab\x00")
	nameOffset := func(name string) uint32 {
		index := strings.Index(string(shstr), name+"\x00")
		if index < 0 {
			t.Fatalf("missing section name %q", name)
		}
		return uint32(index)
	}
	strtab := []byte("\x00vm_entry\x00vm_entry_token\x00_token_table_va\x00vm_native_call\x00vm_atomic_native\x00")
	symbolName := func(name string) uint32 { return uint32(strings.Index(string(strtab), name+"\x00")) }

	note := make([]byte, 32)
	binary.LittleEndian.PutUint32(note[0:], 4)
	binary.LittleEndian.PutUint32(note[4:], 16)
	binary.LittleEndian.PutUint32(note[8:], 5)
	copy(note[12:], "GNU\x00")
	binary.LittleEndian.PutUint32(note[16:], 0xc0000000)
	binary.LittleEndian.PutUint32(note[20:], 4)
	binary.LittleEndian.PutUint32(note[24:], cfg.features)

	ehFrame := []byte{0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if cfg.emptyEHFrame {
		ehFrame = nil
	}
	rela := make([]byte, 72)
	binary.LittleEndian.PutUint64(rela[0:], cfg.relocationOffset)
	binary.LittleEndian.PutUint64(rela[8:], uint64(2)<<32|uint64(uint32(cfg.relocationType)))
	binary.LittleEndian.PutUint64(rela[24:], 4)
	binary.LittleEndian.PutUint64(rela[32:], uint64(4)<<32|uint64(uint32(elf.R_AARCH64_PREL32)))
	binary.LittleEndian.PutUint64(rela[48:], 8)
	binary.LittleEndian.PutUint64(rela[56:], uint64(5)<<32|uint64(uint32(elf.R_AARCH64_PREL32)))

	symtab := make([]byte, 6*24)
	putSymbol := func(index int, name uint32, info byte, section uint16, value, size uint64) {
		offset := index * 24
		binary.LittleEndian.PutUint32(symtab[offset:], name)
		symtab[offset+4] = info
		binary.LittleEndian.PutUint16(symtab[offset+6:], section)
		binary.LittleEndian.PutUint64(symtab[offset+8:], value)
		binary.LittleEndian.PutUint64(symtab[offset+16:], size)
	}
	putSymbol(1, symbolName("vm_entry"), 0x12, 1, 0, 4)
	if !cfg.omitTokenSymbol {
		putSymbol(2, symbolName("vm_entry_token"), 0x12, 1, 0, 4)
	}
	putSymbol(3, symbolName("_token_table_va"), 0x11, 2, 0, 8)
	putSymbol(4, symbolName("vm_native_call"), 0x12, 1, 0, 4)
	putSymbol(5, symbolName("vm_atomic_native"), 0x12, 1, 0, 4)

	type sectionData struct {
		name      string
		typeValue elf.SectionType
		flags     elf.SectionFlag
		align     uint64
		link      uint32
		info      uint32
		entrySize uint64
		data      []byte
		size      uint64
	}
	sections := []sectionData{
		{},
		{name: ".text.entry", typeValue: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC | elf.SHF_EXECINSTR, align: 4, data: []byte{0x5f, 0x24, 0x03, 0xd5}},
		{name: ".data.entry", typeValue: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC | elf.SHF_WRITE, align: 8, data: make([]byte, 8)},
		{name: ".bss", typeValue: elf.SHT_NOBITS, flags: elf.SHF_ALLOC | elf.SHF_WRITE, align: 8, size: 16},
		{name: ".eh_frame", typeValue: elf.SHT_PROGBITS, flags: elf.SHF_ALLOC, align: 8, data: ehFrame},
		{name: ".note.gnu.property", typeValue: elf.SHT_NOTE, flags: elf.SHF_ALLOC, align: 8, data: note},
		{name: ".rela.eh_frame", typeValue: elf.SHT_RELA, align: 8, link: 7, info: 4, entrySize: 24, data: rela},
		{name: ".symtab", typeValue: elf.SHT_SYMTAB, align: 8, link: 8, info: 1, entrySize: 24, data: symtab},
		{name: ".strtab", typeValue: elf.SHT_STRTAB, align: 1, data: strtab},
		{name: ".shstrtab", typeValue: elf.SHT_STRTAB, align: 1, data: shstr},
	}

	align := func(value, alignment uint64) uint64 {
		if alignment <= 1 {
			return value
		}
		return (value + alignment - 1) &^ (alignment - 1)
	}
	offsets := make([]uint64, sectionCount)
	cursor := uint64(64)
	for index := 1; index < sectionCount; index++ {
		if sections[index].typeValue == elf.SHT_NOBITS {
			continue
		}
		cursor = align(cursor, sections[index].align)
		offsets[index] = cursor
		cursor += uint64(len(sections[index].data))
	}
	sectionTable := align(cursor, 8)
	object := make([]byte, sectionTable+sectionCount*64)
	copy(object[:4], []byte{0x7f, 'E', 'L', 'F'})
	object[4], object[5], object[6] = byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(object[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(object[18:], uint16(elf.EM_AARCH64))
	binary.LittleEndian.PutUint32(object[20:], 1)
	binary.LittleEndian.PutUint64(object[40:], sectionTable)
	binary.LittleEndian.PutUint16(object[52:], 64)
	binary.LittleEndian.PutUint16(object[58:], 64)
	binary.LittleEndian.PutUint16(object[60:], sectionCount)
	binary.LittleEndian.PutUint16(object[62:], 9)
	for index := 1; index < sectionCount; index++ {
		section := sections[index]
		if section.typeValue != elf.SHT_NOBITS {
			copy(object[offsets[index]:], section.data)
		}
		header := object[sectionTable+uint64(index*64):]
		binary.LittleEndian.PutUint32(header[0:], nameOffset(section.name))
		binary.LittleEndian.PutUint32(header[4:], uint32(section.typeValue))
		binary.LittleEndian.PutUint64(header[8:], uint64(section.flags))
		binary.LittleEndian.PutUint64(header[24:], offsets[index])
		size := section.size
		if section.typeValue != elf.SHT_NOBITS {
			size = uint64(len(section.data))
		}
		binary.LittleEndian.PutUint64(header[32:], size)
		binary.LittleEndian.PutUint32(header[40:], section.link)
		binary.LittleEndian.PutUint32(header[44:], section.info)
		binary.LittleEndian.PutUint64(header[48:], section.align)
		binary.LittleEndian.PutUint64(header[56:], section.entrySize)
	}
	return object
}
