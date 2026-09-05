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

	"github.com/geg971509-wq/VMPackerGO/internal/abi"
	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
)

const (
	magic64            = 0xfeedfacf
	cpuTypeARM64       = 0x0100000c
	cpuSubtypeARM64    = 0
	mhDylib            = 0x6
	lcSegment64        = 0x19
	lcSymtab           = 0x2
	lcCodeSignature    = 0x1d
	lcEncryptionInfo64 = 0x2c
	header64Size       = 32
	segment64Size      = 72
	section64Size      = 80
	pageSize           = 0x4000
	maxCommands        = 4096
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
				if !zerofill && (sec.Size > 0 && sec.Offset > uint64(len(data)) || sec.Size > uint64(len(data))-sec.Offset) {
					return nil, fmt.Errorf("section %s,%s exceeds file", sec.Seg, sec.Name)
				}
				seg.Sections = append(seg.Sections, sec)
				if seg.Name == "__TEXT" && sec.Name == "__text" {
					textFound = true
				}
			}
			if seg.FileSize > 0 && (seg.FileOff > uint64(len(data)) || seg.FileSize > uint64(len(data))-seg.FileOff) {
				return nil, fmt.Errorf("segment %q exceeds file", seg.Name)
			}
			img.Segments = append(img.Segments, seg)
		case lcCodeSignature:
			img.HasCodeSignature = true
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

func Analyze(data []byte, reqs []SelectionRequest) (Analysis, error) {
	img, err := Parse(data)
	if err != nil {
		return Analysis{}, err
	}
	if len(reqs) == 0 {
		return Analysis{}, fmt.Errorf("at least one function selection is required")
	}
	a := Analysis{TargetKind: "ios-dylib", Limitations: []string{"thin arm64 device dylib only", "selected functions with PC-relative instructions, indirect fixups, ObjC/Swift metadata or exceptions are rejected until relocation-aware iOS VM runtime support is enabled"}}
	text := img.textSection()
	if text.Size == 0 {
		return a, fmt.Errorf("__TEXT,__text is empty")
	}
	for _, r := range reqs {
		s := Selection{Source: r.Source, Selector: r.Selector, Name: r.Name, ABI: r.ABI}
		if r.Address != 0 {
			s.Address = r.Address
			s.End = r.End
		} else {
			var found *Symbol
			for i := range img.Symbols {
				if img.Symbols[i].Name == r.Name {
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
		if s.End == 0 {
			s.End = s.Address + 4
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
		if err := validateMovable(data, s); err != nil {
			return a, err
		}
		s.Section = "__text"
		a.Selections = append(a.Selections, s)
	}
	return a, nil
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
	r.Warnings = append(r.Warnings, "output is unsigned and must be re-signed by the final iOS app/package signing step")
	r.AnalysisLimitations = a.Limitations
	return r, nil
}

func rewrite(data []byte, sels []Selection) ([]byte, error) {
	img, err := Parse(data)
	if err != nil {
		return nil, err
	}
	const cmdSize = segment64Size + section64Size
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
	v := uint64(len(img.Data))
	for _, s := range img.Segments {
		if s.FileOff > 0 && s.FileOff < v {
			v = s.FileOff
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
