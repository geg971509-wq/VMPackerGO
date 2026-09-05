// Package macho contains the deliberately narrow iOS Mach-O backend.
//
// The Android backend is ELF-specific and is kept separate.  This package
// currently accepts thin arm64 MH_DYLIB images for device use.  It moves
// selected, position-independent functions into a new executable segment and
// patches their old entry with an AArch64 branch.  Functions containing
// PC-relative data/control-flow are rejected until the relocation-aware VM
// runtime for those cases is available; silently relocating such code would
// produce a dylib that loads but behaves incorrectly.
package macho

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/abi"
	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
)

const (
	magic64              = 0xfeedfacf
	cpuTypeARM64         = 0x0100000c
	cpuSubtypeARM64      = 0
	mhDylib              = 0x6
	lcSegment64          = 0x19
	lcSymtab             = 0x2
	lcCodeSignature      = 0x1d
	lcDyldInfo           = 0x22
	lcDyldInfoOnly       = 0x80000022
	lcFunctionStarts     = 0x26
	lcDataInCode         = 0x29
	lcEncryptionInfo64   = 0x2c
	lcVersionMinMacOSX   = 0x24
	lcVersionMinIPhoneOS = 0x25
	lcBuildVersion       = 0x32
	lcDyldExportsTrie    = 0x80000033
	lcDyldChainedFixups  = 0x80000034
	header64Size         = 32
	segment64Size        = 72
	section64Size        = 80
	pageSize             = 0x4000
	maxCommands          = 4096
	platformMacOS        = 1
	platformIPhoneOS     = 2
	platformIPhoneSim    = 7
)

type Section struct {
	Seg, Name          string
	Addr, Size, Offset uint64
	Flags              uint32
}
type Segment struct {
	Name                              string
	VMAddr, VMSize, FileOff, FileSize uint64
	MaxProt, InitProt                 uint32
	Sections                          []Section
}
type Symbol struct {
	Name       string
	Addr, Size uint64
	Section    uint8
}
type Image struct {
	Data                          []byte
	CPUType, CPUSubtype, FileType uint32
	NCommands, SizeOfCommands     uint32
	Segments                      []Segment
	Symbols                       []Symbol
	HasCodeSignature              bool
	// Metadata flags are retained so the writer can make an explicit
	// fail-closed decision.  These records contain address-bearing data which
	// needs a dyld-aware rewrite once transformed code can execute from the new
	// segment.  Silently carrying stale metadata would produce a loadable but
	// observably incorrect dylib.
	HasFunctionStarts bool
	HasDyldInfo       bool
	HasExportsTrie    bool
	HasChainedFixups  bool
	HasDataInCode     bool
	HasCompactUnwind  bool
	HasUnwindInfo     bool
	HasEHFrame        bool
	HasGCCExceptTab   bool
	HasObjCMetadata   bool
	HasSwiftMetadata  bool
	HasPlatform       bool
	Platform          uint32
}
type SelectionRequest struct {
	Source, Selector, Name string
	Address, End           uint64
	ABI                    abi.Signature
}
type FunctionFact struct {
	Source, Selector, Name, Section, SymbolSource string
	Address, End, Size                            uint64
	Instructions, Translated, Bytecode            int
}
type Analysis struct {
	TargetKind            string
	Warnings, Limitations []string
	Selections            []Selection
}
type Selection struct {
	Source, Selector, Name string
	Address, End, Offset   uint64
	Section, SymbolSource  string
	ABI                    abi.Signature
}
type Result struct {
	Artifact                                         []byte
	TargetKind, DevelopmentStrategy, RuntimeStrategy string
	Functions                                        []FunctionFact
	AnalysisLimitations, Warnings                    []string
}

func Parse(data []byte) (*Image, error) {
	if len(data) < header64Size || binary.LittleEndian.Uint32(data) != magic64 {
		return nil, fmt.Errorf("input is not a 64-bit little-endian Mach-O")
	}
	img := &Image{Data: append([]byte(nil), data...), CPUType: binary.LittleEndian.Uint32(data[4:]), CPUSubtype: binary.LittleEndian.Uint32(data[8:]), FileType: binary.LittleEndian.Uint32(data[12:]), NCommands: binary.LittleEndian.Uint32(data[16:]), SizeOfCommands: binary.LittleEndian.Uint32(data[20:])}
	if img.CPUType != cpuTypeARM64 || img.CPUSubtype != cpuSubtypeARM64 {
		return nil, fmt.Errorf("only thin arm64 device slices are supported (cpu=0x%x subtype=0x%x)", img.CPUType, img.CPUSubtype)
	}
	if img.FileType != mhDylib {
		return nil, fmt.Errorf("Mach-O file type 0x%x is not MH_DYLIB", img.FileType)
	}
	if img.NCommands == 0 || img.NCommands > maxCommands || img.SizeOfCommands > uint32(len(data)-header64Size) {
		return nil, fmt.Errorf("invalid Mach-O load-command table")
	}
	cmdEnd := uint64(header64Size) + uint64(img.SizeOfCommands)
	if cmdEnd > uint64(len(data)) {
		return nil, fmt.Errorf("load-command table exceeds file")
	}
	var textFound bool
	for i, off := uint32(0), uint64(header64Size); i < img.NCommands; i++ {
		if off+8 > cmdEnd {
			return nil, fmt.Errorf("load command %d is truncated", i)
		}
		kind, size := binary.LittleEndian.Uint32(data[off:]), binary.LittleEndian.Uint32(data[off+4:])
		if size < 8 || off+uint64(size) > cmdEnd {
			return nil, fmt.Errorf("load command %d has invalid size %d", i, size)
		}
		switch kind {
		case lcSegment64:
			if size < segment64Size {
				return nil, fmt.Errorf("LC_SEGMENT_64 is truncated")
			}
			seg := Segment{Name: cString(data[off+8 : off+24]), VMAddr: binary.LittleEndian.Uint64(data[off+24:]), VMSize: binary.LittleEndian.Uint64(data[off+32:]), FileOff: binary.LittleEndian.Uint64(data[off+40:]), FileSize: binary.LittleEndian.Uint64(data[off+48:]), MaxProt: binary.LittleEndian.Uint32(data[off+56:]), InitProt: binary.LittleEndian.Uint32(data[off+60:])}
			if seg.VMSize > math.MaxUint64-seg.VMAddr || seg.FileSize > math.MaxUint64-seg.FileOff {
				return nil, fmt.Errorf("segment %q range overflows", seg.Name)
			}
			nsects := binary.LittleEndian.Uint32(data[off+64:])
			secOff := off + segment64Size
			if uint64(secOff)+uint64(nsects)*section64Size > off+uint64(size) {
				return nil, fmt.Errorf("segment %q sections exceed command", seg.Name)
			}
			for j := uint32(0); j < nsects; j++ {
				p := secOff + uint64(j)*section64Size
				sec := Section{Name: cString(data[p : p+16]), Seg: cString(data[p+16 : p+32]), Addr: binary.LittleEndian.Uint64(data[p+32:]), Size: binary.LittleEndian.Uint64(data[p+40:]), Offset: uint64(binary.LittleEndian.Uint32(data[p+48:])), Flags: binary.LittleEndian.Uint32(data[p+64:])}
				zerofill := sec.Flags&0xff == 1 // S_ZEROFILL
				// Every section must fit inside its owning segment's virtual
				// range.  Without this check a forged __text section can pass
				// the file-range validation below while Analyze later computes
				// an offset outside the segment (or overflows the address).
				if sec.Addr < seg.VMAddr || sec.Addr-seg.VMAddr > seg.VMSize || sec.Size > seg.VMSize-(sec.Addr-seg.VMAddr) {
					return nil, fmt.Errorf("section %s,%s exceeds segment %q virtual range", sec.Seg, sec.Name, seg.Name)
				}
				if !zerofill && sec.Size > 0 {
					if sec.Offset > uint64(len(data)) || sec.Size > uint64(len(data))-sec.Offset {
						return nil, fmt.Errorf("section %s,%s exceeds file", sec.Seg, sec.Name)
					}
				}
				if !zerofill && (sec.Offset < seg.FileOff || sec.Offset-seg.FileOff > seg.FileSize || sec.Size > seg.FileSize-(sec.Offset-seg.FileOff)) {
					return nil, fmt.Errorf("section %s,%s exceeds segment %q file range", sec.Seg, sec.Name, seg.Name)
				}
				seg.Sections = append(seg.Sections, sec)
				if sec.Name == "__compact_unwind" {
					img.HasCompactUnwind = true
				}
				if sec.Name == "__unwind_info" {
					img.HasUnwindInfo = true
				}
				if sec.Name == "__eh_frame" {
					img.HasEHFrame = true
				}
				if sec.Name == "__gcc_except_tab" {
					img.HasGCCExceptTab = true
				}
				if strings.HasPrefix(sec.Name, "__objc_") {
					img.HasObjCMetadata = true
				}
				if strings.HasPrefix(sec.Name, "__swift5_") {
					img.HasSwiftMetadata = true
				}
				if seg.Name == "__TEXT" && sec.Name == "__text" {
					textFound = true
				}
			}
			if seg.FileSize > 0 && (seg.FileOff > uint64(len(data)) || seg.FileSize > uint64(len(data))-seg.FileOff) {
				return nil, fmt.Errorf("segment %q exceeds file", seg.Name)
			}
			img.Segments = append(img.Segments, seg)
		case lcCodeSignature:
			if size < 16 {
				return nil, fmt.Errorf("LC_CODE_SIGNATURE is truncated")
			}
			if err := validateLinkeditRange(data, binary.LittleEndian.Uint32(data[off+8:]), binary.LittleEndian.Uint32(data[off+12:]), "LC_CODE_SIGNATURE"); err != nil {
				return nil, err
			}
			img.HasCodeSignature = true
		case lcFunctionStarts:
			if size < 16 {
				return nil, fmt.Errorf("LC_FUNCTION_STARTS is truncated")
			}
			if err := validateLinkeditRange(data, binary.LittleEndian.Uint32(data[off+8:]), binary.LittleEndian.Uint32(data[off+12:]), "LC_FUNCTION_STARTS"); err != nil {
				return nil, err
			}
			img.HasFunctionStarts = true
		case lcDataInCode:
			if size < 16 {
				return nil, fmt.Errorf("LC_DATA_IN_CODE is truncated")
			}
			if err := validateLinkeditRange(data, binary.LittleEndian.Uint32(data[off+8:]), binary.LittleEndian.Uint32(data[off+12:]), "LC_DATA_IN_CODE"); err != nil {
				return nil, err
			}
			img.HasDataInCode = true
		case lcDyldExportsTrie, lcDyldChainedFixups:
			if size < 16 {
				return nil, fmt.Errorf("load command 0x%x is truncated", kind)
			}
			if err := validateLinkeditRange(data, binary.LittleEndian.Uint32(data[off+8:]), binary.LittleEndian.Uint32(data[off+12:]), fmt.Sprintf("load command 0x%x", kind)); err != nil {
				return nil, err
			}
			if kind == lcDyldExportsTrie {
				img.HasExportsTrie = true
			} else {
				img.HasChainedFixups = true
			}
		case lcDyldInfo, lcDyldInfoOnly:
			if size < 48 {
				return nil, fmt.Errorf("LC_DYLD_INFO is truncated")
			}
			// Every dyld-info substream is an offset/size pair into __LINKEDIT.
			// Validate all of them even when the size is zero; this keeps malformed
			// inputs from reaching a writer that might otherwise preserve stale
			// fixup data.
			for field := uint64(8); field < 48; field += 8 {
				if err := validateLinkeditRange(data, binary.LittleEndian.Uint32(data[off+field:]), binary.LittleEndian.Uint32(data[off+field+4:]), "LC_DYLD_INFO"); err != nil {
					return nil, err
				}
			}
			img.HasDyldInfo = true
		case lcVersionMinMacOSX:
			return nil, fmt.Errorf("macOS deployment target is not valid for an iOS dylib")
		case lcVersionMinIPhoneOS:
			if size < 16 {
				return nil, fmt.Errorf("LC_VERSION_MIN_IPHONEOS is truncated")
			}
			if img.HasPlatform && img.Platform != platformIPhoneOS {
				return nil, fmt.Errorf("conflicting Mach-O platform load commands")
			}
			img.HasPlatform = true
			img.Platform = platformIPhoneOS
		case lcBuildVersion:
			if size < 24 {
				return nil, fmt.Errorf("LC_BUILD_VERSION is truncated")
			}
			platform := binary.LittleEndian.Uint32(data[off+8:])
			ntools := binary.LittleEndian.Uint32(data[off+20:])
			if uint64(24)+uint64(ntools)*8 > uint64(size) {
				return nil, fmt.Errorf("LC_BUILD_VERSION tool table exceeds command")
			}
			if platform != platformIPhoneOS {
				if platform == platformMacOS {
					return nil, fmt.Errorf("macOS platform is not valid for an iOS dylib")
				}
				if platform == platformIPhoneSim {
					return nil, fmt.Errorf("iOS simulator platform is not valid for a device dylib")
				}
				return nil, fmt.Errorf("Mach-O platform %d is not valid for an iOS dylib", platform)
			}
			if img.HasPlatform && img.Platform != platform {
				return nil, fmt.Errorf("conflicting Mach-O platform load commands")
			}
			img.HasPlatform = true
			img.Platform = platform
		case lcEncryptionInfo64:
			if size < 24 {
				return nil, fmt.Errorf("LC_ENCRYPTION_INFO_64 is truncated")
			}
			if binary.LittleEndian.Uint32(data[off+16:]) != 0 {
				return nil, fmt.Errorf("encrypted Mach-O slices cannot be transformed")
			}
		case lcSymtab:
			if size < 24 {
				return nil, fmt.Errorf("LC_SYMTAB is truncated")
			}
			n := binary.LittleEndian.Uint32(data[off+12:])
			stroff := binary.LittleEndian.Uint32(data[off+16:])
			strsize := binary.LittleEndian.Uint32(data[off+20:])
			symoff := binary.LittleEndian.Uint32(data[off+8:])
			if uint64(stroff)+uint64(strsize) > uint64(len(data)) || uint64(symoff)+uint64(n)*16 > uint64(len(data)) {
				return nil, fmt.Errorf("LC_SYMTAB tables exceed file")
			}
			str := data[stroff : uint64(stroff)+uint64(strsize)]
			for j := uint32(0); j < n; j++ {
				p := uint64(symoff) + uint64(j)*16
				nameOff := binary.LittleEndian.Uint32(data[p:])
				if nameOff >= strsize {
					continue
				}
				end := bytes.IndexByte(str[nameOff:], 0)
				if end < 0 {
					end = len(str) - int(nameOff)
				}
				// Only defined section symbols can identify a movable function.  An
				// undefined/imported or STAB symbol may reuse the same name and
				// must never win selector resolution.
				typ := data[p+4] & 0x0e            // N_TYPE
				if typ != 0x0e || data[p+5] == 0 { // N_SECT, non-NO_SECT
					continue
				}
				img.Symbols = append(img.Symbols, Symbol{Name: string(str[nameOff : int(nameOff)+end]), Addr: binary.LittleEndian.Uint64(data[p+8:]), Section: data[p+5]})
			}
		}
		off += uint64(size)
	}
	for i := range img.Segments {
		for j := i + 1; j < len(img.Segments); j++ {
			a, b := img.Segments[i], img.Segments[j]
			if a.FileSize > 0 && b.FileSize > 0 && a.FileOff < b.FileOff+b.FileSize && b.FileOff < a.FileOff+a.FileSize {
				return nil, fmt.Errorf("Mach-O segments %q and %q overlap in the file", a.Name, b.Name)
			}
			if a.VMSize > 0 && b.VMSize > 0 && a.VMAddr < b.VMAddr+b.VMSize && b.VMAddr < a.VMAddr+a.VMSize {
				return nil, fmt.Errorf("Mach-O segments %q and %q overlap in virtual memory", a.Name, b.Name)
			}
		}
	}
	if !textFound {
		return nil, fmt.Errorf("Mach-O dylib has no __TEXT,__text section")
	}
	sort.Slice(img.Symbols, func(i, j int) bool { return img.Symbols[i].Addr < img.Symbols[j].Addr })
	return img, nil
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func validateLinkeditRange(data []byte, off, size uint32, label string) error {
	if size == 0 {
		return nil
	}
	end := uint64(off) + uint64(size)
	if end > uint64(len(data)) {
		return fmt.Errorf("%s data range 0x%x+0x%x exceeds file", label, off, size)
	}
	return nil
}

func Analyze(data []byte, reqs []SelectionRequest) (Analysis, error) {
	img, err := Parse(data)
	if err != nil {
		return Analysis{}, err
	}
	if err := validateTransformMetadata(img); err != nil {
		return Analysis{TargetKind: "ios-dylib", Limitations: iosLimitations()}, err
	}
	if len(reqs) == 0 {
		return Analysis{}, fmt.Errorf("at least one function selection is required")
	}
	a := Analysis{TargetKind: "ios-dylib", Limitations: iosLimitations()}
	text := img.textSection()
	if text.Size == 0 {
		return a, fmt.Errorf("__TEXT,__text is empty")
	}
	for _, r := range reqs {
		s := Selection{Source: r.Source, Selector: r.Selector, Name: r.Name, ABI: r.ABI}
		if r.Address != 0 {
			if r.End == 0 {
				return a, fmt.Errorf("function %q requires an explicit end address in iOS mode; a single instruction is not a safe function range", r.Name)
			}
			s.Address = r.Address
			s.End = r.End
		} else {
			var found *Symbol
			for i := range img.Symbols {
				if sameSymbolName(img.Symbols[i].Name, r.Name) {
					found = &img.Symbols[i]
					break
				}
			}
			if found == nil {
				return a, fmt.Errorf("function %q was not found in LC_SYMTAB", r.Name)
			}
			s.Address = found.Addr
			s.Name = found.Name
			s.SymbolSource = "symtab"
			s.End = found.Addr + found.Size
			if found.Size == 0 {
				s.End = text.Addr + text.Size
				for _, x := range img.Symbols {
					if x.Addr > found.Addr && x.Addr < s.End {
						s.End = x.Addr
					}
				}
			}
		}
		for _, previous := range a.Selections {
			if s.Address < previous.End && previous.Address < s.End {
				return a, fmt.Errorf("selected functions %q and %q overlap", previous.Name, s.Name)
			}
		}
		if s.Address < text.Addr || s.End > text.Addr+text.Size || s.Address%4 != 0 || s.End%4 != 0 || s.End <= s.Address {
			return a, fmt.Errorf("function %q range 0x%x-0x%x is outside __TEXT,__text or is not aligned", s.Name, s.Address, s.End)
		}
		s.Offset = text.Offset + (s.Address - text.Addr)
		if s.End-s.Address < 4 {
			return a, fmt.Errorf("function %q is shorter than one instruction", s.Name)
		}
		if err := validateVMInstructionPolicy(data, s); err != nil {
			return a, err
		}
		if err := validateMovable(data, s); err != nil {
			return a, err
		}
		s.Section = "__text"
		a.Selections = append(a.Selections, s)
	}
	return a, nil
}

func sameSymbolName(a, b string) bool {
	return a == b || strings.TrimPrefix(a, "_") == strings.TrimPrefix(b, "_")
}

func validateVMInstructionPolicy(data []byte, s Selection) error {
	decoder := arm64.NewDecoder()
	for off := s.Offset; off < s.Offset+s.End-s.Address; off += 4 {
		raw := binary.LittleEndian.Uint32(data[off : off+4])
		inst := decoder.Decode(raw, int(off-s.Offset))
		if err := arm64.ValidateInstruction(inst); err != nil {
			return fmt.Errorf("function %q contains unsupported VM instruction %s at file offset 0x%x: %w", s.Name, arm64.OpName(arm64.Op(inst.Op)), off, err)
		}
	}
	return nil
}

func iosLimitations() []string {
	return []string{
		"thin arm64 device dylib only",
		"selected functions with PC-relative instructions, indirect fixups, ObjC/Swift metadata, compact unwind or exceptions are rejected until relocation-aware iOS VM runtime support is enabled",
		"existing dyld exports/rebase/bind, LC_FUNCTION_STARTS and data-in-code metadata are preserved because original entry addresses remain stable; chained fixups are rejected until the new segment is added to their segment table",
	}
}

func validateTransformMetadata(img *Image) error {
	var present []string
	if img.HasCompactUnwind || img.HasUnwindInfo || img.HasEHFrame || img.HasGCCExceptTab {
		present = append(present, "__compact_unwind/exception metadata")
	}
	if img.HasObjCMetadata {
		present = append(present, "Objective-C metadata")
	}
	if img.HasSwiftMetadata {
		present = append(present, "Swift metadata")
	}
	if img.HasChainedFixups {
		present = append(present, "dyld chained fixups")
	}
	if len(present) == 0 {
		return nil
	}
	return fmt.Errorf("Mach-O contains address-bearing metadata requiring a relocation-aware writer: %s", strings.Join(present, ", "))
}

func (img *Image) textSection() Section {
	for _, s := range img.Segments {
		for _, x := range s.Sections {
			if s.Name == "__TEXT" && x.Name == "__text" {
				return x
			}
		}
	}
	return Section{}
}
func validateMovable(data []byte, s Selection) error {
	d := arm64.NewDecoder()
	for off := s.Offset; off < s.Offset+s.End-s.Address; off += 4 {
		op := arm64.Op(d.Decode(binary.LittleEndian.Uint32(data[off:off+4]), int(off-s.Offset)).Op)
		switch op {
		case arm64.ADR, arm64.ADRP, arm64.LDR_LIT, arm64.B, arm64.BL, arm64.B_COND, arm64.CBZ, arm64.CBNZ, arm64.TBZ, arm64.TBNZ, arm64.BR, arm64.BLR:
			return fmt.Errorf("function %q contains relocation-sensitive %s at file offset 0x%x", s.Name, arm64.OpName(op), off)
		}
	}
	return nil
}

func Process(data []byte, reqs []SelectionRequest) (Result, error) {
	a, err := Analyze(data, reqs)
	r := Result{TargetKind: "ios-dylib", DevelopmentStrategy: "macho-relocation-only", RuntimeStrategy: "ios-arm64-relocated-entry"}
	if err != nil {
		r.AnalysisLimitations = a.Limitations
		r.Warnings = a.Warnings
		return r, err
	}
	out, err := rewrite(data, a.Selections)
	if err != nil {
		return r, err
	}
	r.Artifact = out
	for _, s := range a.Selections {
		r.Functions = append(r.Functions, FunctionFact{Source: s.Source, Selector: s.Selector, Name: s.Name, Address: s.Address, End: s.End, Size: s.End - s.Address, Section: s.Section, SymbolSource: s.SymbolSource, Instructions: int((s.End - s.Address) / 4)})
	}
	r.Warnings = append(r.Warnings,
		"iOS output is structural relocation only; no VM bytecode, encrypted token payload, or Darwin interpreter is injected",
		"output is unsigned and must be re-signed by the final iOS app/package signing step",
	)
	r.AnalysisLimitations = a.Limitations
	return r, nil
}

func rewrite(data []byte, sels []Selection) ([]byte, error) {
	img, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := validateTransformMetadata(img); err != nil {
		return nil, err
	}
	const cmdSize = segment64Size + section64Size
	if uint64(img.SizeOfCommands)+cmdSize > math.MaxUint32 {
		return nil, fmt.Errorf("Mach-O load-command table size overflows 32-bit field")
	}
	if uint64(header64Size)+uint64(img.SizeOfCommands)+cmdSize > firstFileOffset(img) {
		return nil, fmt.Errorf("Mach-O headerpad is insufficient for the __VMPACK segment load command")
	}
	out := append([]byte(nil), data...)
	appendOff := align(uint64(len(out)), pageSize)
	if appendOff > uint64(math.MaxInt) {
		return nil, fmt.Errorf("output offset overflow")
	}
	if appendOff > uint64(len(out)) {
		out = append(out, make([]byte, appendOff-uint64(len(out)))...)
	}
	maxVA := uint64(0)
	for _, s := range img.Segments {
		if s.VMSize > math.MaxUint64-s.VMAddr {
			return nil, fmt.Errorf("Mach-O virtual layout overflows")
		}
		if e := s.VMAddr + s.VMSize; e > maxVA {
			maxVA = e
		}
	}
	packVA := align(maxVA, pageSize)
	if appendOff > math.MaxUint32 {
		return nil, fmt.Errorf("Mach-O output exceeds 32-bit section-offset limit")
	}
	if packVA > math.MaxInt64 {
		return nil, fmt.Errorf("Mach-O virtual layout exceeds branch arithmetic range")
	}
	payload := make([]byte, 0)
	for _, s := range sels {
		payload = append(payload, data[s.Offset:s.Offset+s.End-s.Address]...)
	}
	segFileSize := align(uint64(len(payload)), pageSize)
	out = append(out, payload...)
	if segFileSize > uint64(len(payload)) {
		out = append(out, make([]byte, segFileSize-uint64(len(payload)))...)
	}
	if err := patchLoadCommands(out, img, packVA, appendOff, segFileSize); err != nil {
		return nil, err
	}
	cursor := uint64(0)
	for _, s := range sels {
		target := packVA + cursor
		if err := patchBranch(out, s.Offset, s.Address, target, s.End-s.Address); err != nil {
			return nil, err
		}
		cursor += s.End - s.Address
	}
	return out, nil
}
func firstFileOffset(img *Image) uint64 {
	// A __TEXT segment commonly has FileOff==0 because it contains the Mach-O
	// header itself.  Its FileOff is therefore not the first byte available for
	// an extra load command; the first file-backed section is the real
	// headerpad boundary.  Ignoring that section can overwrite __text when a
	// binary was linked without enough header padding.
	v := uint64(len(img.Data))
	for _, seg := range img.Segments {
		if seg.FileSize > 0 && seg.FileOff > 0 && seg.FileOff < v {
			v = seg.FileOff
		}
		for _, sec := range seg.Sections {
			if sec.Size == 0 || sec.Offset == 0 {
				continue
			}
			if sec.Offset < v {
				v = sec.Offset
			}
		}
	}
	return v
}

func align(v, a uint64) uint64 {
	if v%(a) == 0 {
		return v
	}
	add := a - v%a
	if v > math.MaxUint64-add {
		return math.MaxUint64
	}
	return v + add
}
func patchBranch(out []byte, off, pc, target, size uint64) error {
	delta := int64(target) - int64(pc)
	if delta%4 != 0 || delta/4 < -(1<<25) || delta/4 >= (1<<25) {
		return fmt.Errorf("function at 0x%x is outside direct branch range", pc)
	}
	if size < 4 {
		return fmt.Errorf("function at 0x%x is too short", pc)
	}
	imm := uint32(delta/4) & 0x03ffffff
	binary.LittleEndian.PutUint32(out[off:], 0x14000000|imm)
	for p := off + 4; p < off+size; p += 4 {
		binary.LittleEndian.PutUint32(out[p:], 0xd503201f)
	}
	return nil
}
func patchLoadCommands(out []byte, img *Image, va, off, size uint64) error {
	const cmdSize = segment64Size + section64Size
	n := img.NCommands
	pos := uint64(header64Size)
	var sig uint64
	for i := uint32(0); i < img.NCommands; i++ {
		kind := binary.LittleEndian.Uint32(out[pos:])
		sz := binary.LittleEndian.Uint32(out[pos+4:])
		if kind == lcCodeSignature {
			sig = pos
		}
		pos += uint64(sz)
	}
	if sig != 0 {
		// A stale CodeDirectory must never be advertised as valid after the
		// text and load-command table change.  Keep the command shape (so all
		// following commands retain their offsets) but clear its blob range;
		// signing tools will replace it during the final app/package signing
		// step.
		binary.LittleEndian.PutUint32(out[sig+8:], 0)
		binary.LittleEndian.PutUint32(out[sig+12:], 0)
	}
	insert := uint64(header64Size) + uint64(img.SizeOfCommands)
	binary.LittleEndian.PutUint32(out[16:], n+1)
	binary.LittleEndian.PutUint32(out[20:], img.SizeOfCommands+uint32(segment64Size+section64Size))
	cmd := out[insert : insert+cmdSize]
	for i := range cmd {
		cmd[i] = 0
	}
	binary.LittleEndian.PutUint32(cmd, lcSegment64)
	binary.LittleEndian.PutUint32(cmd[4:], cmdSize)
	copy(cmd[8:], []byte("__VMPACK"))
	binary.LittleEndian.PutUint64(cmd[24:], va)
	binary.LittleEndian.PutUint64(cmd[32:], size)
	binary.LittleEndian.PutUint64(cmd[40:], off)
	binary.LittleEndian.PutUint64(cmd[48:], size)
	binary.LittleEndian.PutUint32(cmd[56:], 5)
	binary.LittleEndian.PutUint32(cmd[60:], 5)
	binary.LittleEndian.PutUint32(cmd[64:], 1)
	sec := cmd[segment64Size:]
	copy(sec, []byte("__vmtext"))
	copy(sec[16:], []byte("__VMPACK"))
	binary.LittleEndian.PutUint64(sec[32:], va)
	binary.LittleEndian.PutUint64(sec[40:], size)
	binary.LittleEndian.PutUint32(sec[48:], uint32(off))
	// section_64.align is log2 alignment.  The VM text contains AArch64
	// instructions and must advertise at least four-byte alignment to tools
	// consuming the rewritten Mach-O.
	binary.LittleEndian.PutUint32(sec[52:], 2)
	binary.LittleEndian.PutUint32(sec[64:], 0x80000400)
	return nil
}

func PrintInfo(data []byte, name string, out interface{ Write([]byte) (int, error) }) error {
	img, err := Parse(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Mach-O iOS dylib: %s\n", name)
	fmt.Fprintf(out, "CPU: arm64  load commands: %d\n", img.NCommands)
	for _, s := range img.Segments {
		fmt.Fprintf(out, "segment %s vmaddr=0x%x vmsize=0x%x fileoff=0x%x filesize=0x%x\n", s.Name, s.VMAddr, s.VMSize, s.FileOff, s.FileSize)
	}
	return nil
}

func (s Selection) String() string { return fmt.Sprintf("%s 0x%x-0x%x", s.Name, s.Address, s.End) }
