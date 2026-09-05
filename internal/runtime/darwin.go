package runtime

// Darwin support is intentionally independent from the Android ELF runtime.
// A Darwin runtime is compiled as a relocatable MH_OBJECT and is later linked
// into the target dylib by the Mach-O writer.  This file only owns toolchain
// invocation and object validation; it does not claim to provide VM semantics
// by itself.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	darwinMagic64       = 0xfeedfacf
	darwinCPUTypeARM64  = 0x0100000c
	darwinSubtypeARM64  = 0
	darwinMHObject      = 0x1
	darwinHeader64Size  = 32
	darwinLCSegment64   = 0x19
	darwinLCSymtab      = 0x2
	darwinSection64Size = 80
	darwinSegment64Size = 72
	darwinNList64Size   = 16
	darwinRelocInfoSize = 8
)

// DarwinSection describes one section in a relocatable Mach-O object.
type DarwinSection struct {
	Index                     uint32
	Name, Segment             string
	Addr, Size, Offset, Align uint64
	Flags                     uint32
	RelocOffset, RelocCount   uint32
	Data                      []byte
}

// DarwinSymbol is an nlist_64 record. Size is inferred from the next symbol
// in the same section because Mach-O nlist records do not carry a size field.
type DarwinSymbol struct {
	Index         uint32
	Name          string
	Type, Section uint8
	Desc          uint16
	Value, Size   uint64
}

// DarwinRelocation is a decoded relocation_info record. SymbolNum is an
// index into DarwinImage.Symbols when Extern is true. For non-external
// relocations SymbolNum is the section ordinal; addends remain in the target bytes.
type DarwinRelocation struct {
	SectionIndex uint32
	Address      int32
	SymbolNum    uint32
	PCRel        bool
	Length       uint8 // encoded width exponent: 2 means 4 bytes, 3 means 8 bytes
	Extern       bool
	Type         uint8
	Value        uint64
}

// DarwinImage is a validated, relocatable arm64 Darwin runtime object.
type DarwinImage struct {
	Object                        []byte
	CPUType, CPUSubtype, FileType uint32
	NCommands, SizeOfCommands     uint32
	Sections                      []DarwinSection
	Symbols                       []DarwinSymbol
	Relocations                   []DarwinRelocation
}

// DarwinBuildConfig describes an actual Apple-clang compile. Source must be
// supplied by the caller; no stub runtime is silently generated.
type DarwinBuildConfig struct {
	Clang            string
	SDK              string
	DeploymentTarget string
	// SourceName controls how Source is parsed by clang.  It must end in .c,
	// .m, .S or .s; the default is .S for the low-level assembly contract.
	// Keeping the language explicit prevents a future C interpreter from being
	// accidentally passed to clang as assembly.
	SourceName      string
	Source          []byte
	RequiredSymbols []string
}

// BuildDarwin compiles Source into a relocatable arm64 iOS MH_OBJECT and then
// validates the resulting object. It requires a Darwin-capable clang; this is
// expected to run on macOS/Xcode or a CI image that provides an Apple target.
func BuildDarwin(ctx context.Context, cfg DarwinBuildConfig) (*DarwinImage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(cfg.Source)) == 0 {
		return nil, fmt.Errorf("Darwin runtime source is required")
	}
	clang := cfg.Clang
	if clang == "" {
		clang = os.Getenv("VMPACKER_DARWIN_CLANG")
	}
	if clang == "" {
		var err error
		clang, err = exec.LookPath("clang")
		if err != nil {
			return nil, fmt.Errorf("Darwin clang is required: %w", err)
		}
	}
	if _, err := exec.LookPath(clang); err != nil && !filepath.IsAbs(clang) {
		return nil, fmt.Errorf("locate Darwin clang %q: %w", clang, err)
	}
	temp, err := os.MkdirTemp("", "vmpacker-darwin-runtime-")
	if err != nil {
		return nil, fmt.Errorf("create private Darwin runtime directory: %w", err)
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0700); err != nil {
		return nil, fmt.Errorf("secure Darwin runtime directory: %w", err)
	}
	sourceName := cfg.SourceName
	if sourceName == "" {
		sourceName = "runtime.S"
	}
	ext := strings.ToLower(filepath.Ext(sourceName))
	if ext != ".c" && ext != ".m" && ext != ".s" {
		return nil, fmt.Errorf("Darwin runtime source name must use .c, .m, .S or .s")
	}
	src := filepath.Join(temp, filepath.Base(sourceName))
	out := filepath.Join(temp, "runtime.o")
	if err := os.WriteFile(src, cfg.Source, 0600); err != nil {
		return nil, fmt.Errorf("write Darwin runtime source: %w", err)
	}
	target := "arm64-apple-ios"
	if cfg.DeploymentTarget != "" {
		target += cfg.DeploymentTarget
	}
	args := []string{"-target", target, "-c", "-fPIC", "-ffreestanding", "-fno-builtin", "-fno-stack-protector", "-fvisibility=hidden", "-nostdlib", "-o", out, src}
	if cfg.SDK != "" {
		args = append([]string{"-isysroot", cfg.SDK}, args...)
	}
	cmd := exec.CommandContext(ctx, clang, args...)
	cmd.Env = append(os.Environ(), "SDKROOT=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("compile Darwin runtime: %w: %s", err, strings.TrimSpace(string(output)))
	}
	object, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read Darwin runtime object: %w", err)
	}
	image, err := ParseDarwinObject(object)
	if err != nil {
		return nil, fmt.Errorf("validate Darwin runtime object: %w", err)
	}
	for _, name := range cfg.RequiredSymbols {
		if _, ok := image.Symbol(name); !ok {
			return nil, fmt.Errorf("Darwin runtime is missing required symbol %q", name)
		}
	}
	return image, nil
}

// ParseDarwinObject validates a little-endian arm64 MH_OBJECT, including all
// section and relocation ranges. Unsupported file formats fail closed.
func ParseDarwinObject(data []byte) (*DarwinImage, error) {
	if len(data) < darwinHeader64Size || binary.LittleEndian.Uint32(data) != darwinMagic64 {
		return nil, fmt.Errorf("input is not a 64-bit little-endian Mach-O object")
	}
	img := &DarwinImage{Object: append([]byte(nil), data...), CPUType: binary.LittleEndian.Uint32(data[4:]), CPUSubtype: binary.LittleEndian.Uint32(data[8:]), FileType: binary.LittleEndian.Uint32(data[12:]), NCommands: binary.LittleEndian.Uint32(data[16:]), SizeOfCommands: binary.LittleEndian.Uint32(data[20:])}
	if img.CPUType != darwinCPUTypeARM64 || img.CPUSubtype != darwinSubtypeARM64 {
		return nil, fmt.Errorf("Darwin runtime must target arm64 (cpu=0x%x subtype=0x%x)", img.CPUType, img.CPUSubtype)
	}
	if img.FileType != darwinMHObject {
		return nil, fmt.Errorf("Darwin runtime must be MH_OBJECT (type=0x%x)", img.FileType)
	}
	if img.NCommands == 0 || img.NCommands > 4096 || uint64(img.SizeOfCommands) > uint64(len(data)-darwinHeader64Size) {
		return nil, fmt.Errorf("invalid Mach-O object load-command table")
	}
	cmdEnd := uint64(darwinHeader64Size) + uint64(img.SizeOfCommands)
	var symoff, nsyms, stroff, strsize uint32
	for i, pos := uint32(0), uint64(darwinHeader64Size); i < img.NCommands; i++ {
		if pos+8 > cmdEnd {
			return nil, fmt.Errorf("load command %d is truncated", i)
		}
		kind, size := binary.LittleEndian.Uint32(data[pos:]), binary.LittleEndian.Uint32(data[pos+4:])
		if size < 8 || pos+uint64(size) > cmdEnd {
			return nil, fmt.Errorf("load command %d has invalid size %d", i, size)
		}
		switch kind {
		case darwinLCSegment64:
			if size < darwinSegment64Size {
				return nil, fmt.Errorf("LC_SEGMENT_64 is truncated")
			}
			nsects := binary.LittleEndian.Uint32(data[pos+64:])
			secPos := pos + darwinSegment64Size
			if uint64(nsects) > math.MaxUint64/uint64(darwinSection64Size) || secPos+uint64(nsects)*darwinSection64Size > pos+uint64(size) {
				return nil, fmt.Errorf("segment sections exceed command")
			}
			for j := uint32(0); j < nsects; j++ {
				p := secPos + uint64(j)*darwinSection64Size
				s := DarwinSection{Index: uint32(len(img.Sections) + 1), Name: darwinCString(data[p : p+16]), Segment: darwinCString(data[p+16 : p+32]), Addr: binary.LittleEndian.Uint64(data[p+32:]), Size: binary.LittleEndian.Uint64(data[p+40:]), Offset: uint64(binary.LittleEndian.Uint32(data[p+48:])), Align: uint64(binary.LittleEndian.Uint32(data[p+52:])), RelocOffset: binary.LittleEndian.Uint32(data[p+56:]), RelocCount: binary.LittleEndian.Uint32(data[p+60:]), Flags: binary.LittleEndian.Uint32(data[p+64:])}
				if s.Size > 0 && (s.Offset > uint64(len(data)) || s.Size > uint64(len(data))-s.Offset) {
					return nil, fmt.Errorf("section %s,%s exceeds object", s.Segment, s.Name)
				}
				if s.RelocCount > 0 && (uint64(s.RelocOffset) > uint64(len(data)) || uint64(s.RelocCount)*darwinRelocInfoSize > uint64(len(data))-uint64(s.RelocOffset)) {
					return nil, fmt.Errorf("section %s,%s relocations exceed object", s.Segment, s.Name)
				}
				if s.Size > 0 {
					s.Data = append([]byte(nil), data[s.Offset:s.Offset+s.Size]...)
				}
				img.Sections = append(img.Sections, s)
			}
		case darwinLCSymtab:
			if size < 24 {
				return nil, fmt.Errorf("LC_SYMTAB is truncated")
			}
			symoff, nsyms, stroff, strsize = binary.LittleEndian.Uint32(data[pos+8:]), binary.LittleEndian.Uint32(data[pos+12:]), binary.LittleEndian.Uint32(data[pos+16:]), binary.LittleEndian.Uint32(data[pos+20:])
			if uint64(symoff)+uint64(nsyms)*darwinNList64Size > uint64(len(data)) || uint64(stroff)+uint64(strsize) > uint64(len(data)) {
				return nil, fmt.Errorf("symbol tables exceed object")
			}
		}
		pos += uint64(size)
	}
	if len(img.Sections) == 0 {
		return nil, fmt.Errorf("Darwin runtime object has no sections")
	}
	if nsyms == 0 {
		return nil, fmt.Errorf("Darwin runtime object has no symbols")
	}
	str := data[stroff : uint64(stroff)+uint64(strsize)]
	for i := uint32(0); i < nsyms; i++ {
		p := uint64(symoff) + uint64(i)*darwinNList64Size
		nameOff := binary.LittleEndian.Uint32(data[p:])
		typ, sect := data[p+4], data[p+5]
		desc := binary.LittleEndian.Uint16(data[p+6:])
		value := binary.LittleEndian.Uint64(data[p+8:])
		name := ""
		if nameOff < strsize {
			tail := str[nameOff:]
			if end := bytes.IndexByte(tail, 0); end >= 0 {
				tail = tail[:end]
			}
			name = string(tail)
		}
		img.Symbols = append(img.Symbols, DarwinSymbol{Index: i, Name: name, Type: typ, Section: sect, Desc: desc, Value: value})
	}
	// Infer symbol extents within each section for consumers that need a range.
	for i := range img.Symbols {
		s := &img.Symbols[i]
		if s.Section == 0 || int(s.Section) > len(img.Sections) {
			continue
		}
		end := img.Sections[s.Section-1].Addr + img.Sections[s.Section-1].Size
		for j := range img.Symbols {
			if i != j && img.Symbols[j].Section == s.Section && img.Symbols[j].Value > s.Value && img.Symbols[j].Value < end && (s.Size == 0 || img.Symbols[j].Value < s.Value+s.Size) {
				end = img.Symbols[j].Value
			}
		}
		if end >= s.Value {
			s.Size = end - s.Value
		}
	}
	sort.SliceStable(img.Symbols, func(i, j int) bool { return img.Symbols[i].Index < img.Symbols[j].Index })
	for _, s := range img.Sections {
		for i := uint32(0); i < s.RelocCount; i++ {
			p := uint64(s.RelocOffset) + uint64(i)*darwinRelocInfoSize
			rawAddr := int32(binary.LittleEndian.Uint32(data[p:]))
			info := binary.LittleEndian.Uint32(data[p+4:])
			if rawAddr < 0 {
				return nil, fmt.Errorf("scattered relocations are not supported")
			}
			reloc := DarwinRelocation{SectionIndex: s.Index, Address: rawAddr, SymbolNum: info & 0x00ffffff, PCRel: info&0x01000000 != 0, Length: uint8((info >> 25) & 3), Extern: info&0x08000000 != 0, Type: uint8(info >> 28)}
			if reloc.Type > 11 {
				return nil, fmt.Errorf("section %s,%s uses unsupported arm64 relocation type %d", s.Segment, s.Name, reloc.Type)
			}
			if reloc.Length > 3 {
				return nil, fmt.Errorf("section %s,%s has invalid relocation width %d", s.Segment, s.Name, reloc.Length)
			}
			width := uint64(1) << reloc.Length
			if uint64(reloc.Address) > s.Size || width > s.Size-uint64(reloc.Address) {
				return nil, fmt.Errorf("section %s,%s relocation offset 0x%x exceeds section", s.Segment, s.Name, reloc.Address)
			}
			if reloc.Extern {
				if reloc.SymbolNum >= uint32(len(img.Symbols)) {
					return nil, fmt.Errorf("section %s,%s relocation references symbol %d outside symbol table", s.Segment, s.Name, reloc.SymbolNum)
				}
			} else if reloc.SymbolNum == 0 || reloc.SymbolNum > uint32(len(img.Sections)) {
				return nil, fmt.Errorf("section %s,%s relocation references section %d", s.Segment, s.Name, reloc.SymbolNum)
			}
			img.Relocations = append(img.Relocations, reloc)
		}
	}
	return img, nil
}

func darwinCString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func (img *DarwinImage) Symbol(name string) (DarwinSymbol, bool) {
	for _, s := range img.Symbols {
		if s.Section == 0 || s.Type&0x0e != 0x0e {
			continue
		}
		if s.Name == name || strings.TrimPrefix(s.Name, "_") == strings.TrimPrefix(name, "_") {
			return s, true
		}
	}
	return DarwinSymbol{}, false
}
