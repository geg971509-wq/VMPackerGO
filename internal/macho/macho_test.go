package macho

import (
	"encoding/binary"
	"strings"
	"testing"
)

func syntheticDylib(subtype uint32, withSignature bool) []byte {
	const segCmd = segment64Size + section64Size
	cmds := uint32(segCmd)
	if withSignature {
		cmds += 16
	}
	data := make([]byte, 0x5000)
	binary.LittleEndian.PutUint32(data[0:], magic64)
	binary.LittleEndian.PutUint32(data[4:], cpuTypeARM64)
	binary.LittleEndian.PutUint32(data[8:], subtype)
	binary.LittleEndian.PutUint32(data[12:], mhDylib)
	ncmds := uint32(1)
	if withSignature {
		ncmds++
	}
	binary.LittleEndian.PutUint32(data[16:], ncmds)
	binary.LittleEndian.PutUint32(data[20:], cmds)
	// __TEXT command
	p := uint64(header64Size)
	binary.LittleEndian.PutUint32(data[p:], lcSegment64)
	binary.LittleEndian.PutUint32(data[p+4:], segCmd)
	copy(data[p+8:p+24], []byte("__TEXT"))
	binary.LittleEndian.PutUint64(data[p+24:], 0x1000)
	binary.LittleEndian.PutUint64(data[p+32:], 0x4000)
	binary.LittleEndian.PutUint64(data[p+40:], 0x1000)
	binary.LittleEndian.PutUint64(data[p+48:], 0x1000)
	binary.LittleEndian.PutUint32(data[p+56:], 5)
	binary.LittleEndian.PutUint32(data[p+60:], 5)
	binary.LittleEndian.PutUint32(data[p+64:], 1)
	sec := p + segment64Size
	copy(data[sec:sec+16], []byte("__text"))
	copy(data[sec+16:sec+32], []byte("__TEXT"))
	binary.LittleEndian.PutUint64(data[sec+32:], 0x1000)
	binary.LittleEndian.PutUint64(data[sec+40:], 8)
	binary.LittleEndian.PutUint32(data[sec+48:], 0x1000)
	binary.LittleEndian.PutUint32(data[sec+64:], 0x80000400)
	// MOV W0,#1; RET
	binary.LittleEndian.PutUint32(data[0x1000:], 0x52800020)
	binary.LittleEndian.PutUint32(data[0x1004:], 0xd65f03c0)
	if withSignature {
		p += segCmd
		binary.LittleEndian.PutUint32(data[p:], lcCodeSignature)
		binary.LittleEndian.PutUint32(data[p+4:], 16)
		binary.LittleEndian.PutUint32(data[p+8:], 0x4800)
		binary.LittleEndian.PutUint32(data[p+12:], 0x100)
	}
	return data
}

func addBuildPlatform(in []byte, platform uint32) []byte {
	out := append([]byte(nil), in...)
	oldN := binary.LittleEndian.Uint32(out[16:])
	oldSize := binary.LittleEndian.Uint32(out[20:])
	const size = 24
	pos := uint64(header64Size) + uint64(oldSize)
	if pos+size > 0x1000 {
		panic("test fixture has no headerpad")
	}
	binary.LittleEndian.PutUint32(out[pos:], lcBuildVersion)
	binary.LittleEndian.PutUint32(out[pos+4:], size)
	binary.LittleEndian.PutUint32(out[pos+8:], platform)
	binary.LittleEndian.PutUint32(out[16:], oldN+1)
	binary.LittleEndian.PutUint32(out[20:], oldSize+size)
	return out
}

func addSymtabWithDuplicate(in []byte) []byte {
	out := append([]byte(nil), in...)
	oldN := binary.LittleEndian.Uint32(out[16:])
	oldSize := binary.LittleEndian.Uint32(out[20:])
	const cmdSize = 24
	pos := uint64(header64Size) + uint64(oldSize)
	if pos+cmdSize > 0x1000 {
		panic("test fixture has no headerpad")
	}
	const symoff = 0x2000
	const stroff = 0x2040
	const nsyms = 2
	binary.LittleEndian.PutUint32(out[pos:], lcSymtab)
	binary.LittleEndian.PutUint32(out[pos+4:], cmdSize)
	binary.LittleEndian.PutUint32(out[pos+8:], symoff)
	binary.LittleEndian.PutUint32(out[pos+12:], nsyms)
	binary.LittleEndian.PutUint32(out[pos+16:], stroff)
	binary.LittleEndian.PutUint32(out[pos+20:], 8)
	// First entry is an undefined import named _entry; second is the real
	// section definition. Selector resolution must ignore the first entry.
	binary.LittleEndian.PutUint32(out[symoff:], 1)
	binary.LittleEndian.PutUint32(out[symoff+16:], 1)
	out[symoff+4] = 0     // N_UNDF
	out[symoff+20] = 0x0e // N_SECT
	out[symoff+21] = 1    // section index
	binary.LittleEndian.PutUint64(out[symoff+24:], 0x1000)
	copy(out[stroff:], []byte{0, '_', 'e', 'n', 't', 'r', 'y', 0})
	binary.LittleEndian.PutUint32(out[16:], oldN+1)
	binary.LittleEndian.PutUint32(out[20:], oldSize+cmdSize)
	return out
}

func TestParseRejectsNonIOSBuildPlatforms(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform uint32
		want     string
	}{
		{name: "macOS", platform: platformMacOS, want: "macOS platform"},
		{name: "iOS simulator", platform: platformIPhoneSim, want: "simulator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(addBuildPlatform(syntheticDylib(cpuSubtypeARM64, false), tc.platform))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestSymtabSelectorIgnoresUndefinedDuplicate(t *testing.T) {
	in := addSymtabWithDuplicate(syntheticDylib(cpuSubtypeARM64, false))
	result, err := Process(in, []SelectionRequest{{Name: "_entry"}})
	if err != nil {
		t.Fatalf("Process error=%v", err)
	}
	if len(result.Artifact) == 0 {
		t.Fatal("Process returned empty artifact")
	}
}

func TestSymtabSelectorAcceptsDarwinUnderscoreConvention(t *testing.T) {
	in := addSymtabWithDuplicate(syntheticDylib(cpuSubtypeARM64, false))
	result, err := Process(in, []SelectionRequest{{Name: "entry"}})
	if err != nil {
		t.Fatalf("Process error=%v", err)
	}
	if len(result.Artifact) == 0 {
		t.Fatal("Process returned empty artifact")
	}
}

func TestRewriteRejectsInsufficientHeaderpadForTextAtLowOffset(t *testing.T) {
	in := syntheticDylib(cpuSubtypeARM64, false)
	// Move __text immediately after the existing command table, leaving no
	// room for the extra LC_SEGMENT_64 command.
	// Make __TEXT include the Mach-O header itself (FileOff == 0), as real
	// images often do, then place __text immediately after old commands.
	binary.LittleEndian.PutUint64(in[header64Size+40:], 0)
	sec := uint64(header64Size + segment64Size)
	binary.LittleEndian.PutUint32(in[sec+48:], 0xc8)
	binary.LittleEndian.PutUint32(in[0xc8:], 0x52800020)
	binary.LittleEndian.PutUint32(in[0xcc:], 0xd65f03c0)
	_, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}})
	if err == nil || !strings.Contains(err.Error(), "headerpad") {
		t.Fatalf("Process error=%v, want headerpad failure", err)
	}
}

func TestParseAndRelocateThinArm64Dylib(t *testing.T) {
	in := syntheticDylib(cpuSubtypeARM64, false)
	img, err := Parse(in)
	if err != nil || len(img.Segments) != 1 {
		t.Fatalf("parse: img=%#v err=%v", img, err)
	}
	r, err := Process(in, []SelectionRequest{{Source: "direct", Selector: "0x1000-0x1008", Address: 0x1000, End: 0x1008}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Artifact) <= len(in) || r.TargetKind != "ios-dylib" {
		t.Fatalf("unexpected result: size=%d kind=%q", len(r.Artifact), r.TargetKind)
	}
	out, err := Parse(r.Artifact)
	if err != nil {
		t.Fatalf("rewritten parse: %v", err)
	}
	if len(out.Segments) != 2 || out.Segments[1].Name != "__VMPACK" {
		t.Fatalf("segments=%#v", out.Segments)
	}
	newCmd := 32 + binary.LittleEndian.Uint32(r.Artifact[20:]) - uint32(segment64Size+section64Size)
	if got := binary.LittleEndian.Uint32(r.Artifact[newCmd+segment64Size+52:]); got != 2 {
		t.Fatalf("__VMPACK section alignment=%d, want log2(4)=2", got)
	}
	if got := binary.LittleEndian.Uint32(r.Artifact[0x1000:]); got&0xfc000000 != 0x14000000 {
		t.Fatalf("entry is not B: 0x%x", got)
	}
}

func TestParseRejectsArm64e(t *testing.T) {
	if _, err := Parse(syntheticDylib(2, false)); err == nil {
		t.Fatal("arm64e unexpectedly accepted")
	}
}

func TestProcessRequiresExplicitRangeForAddressSelection(t *testing.T) {
	in := syntheticDylib(cpuSubtypeARM64, false)
	_, err := Process(in, []SelectionRequest{{Address: 0x1000}})
	if err == nil || !strings.Contains(err.Error(), "explicit end address") {
		t.Fatalf("Process error=%v", err)
	}
}

func TestProcessRejectsInstructionsOutsideVMPolicy(t *testing.T) {
	in := syntheticDylib(cpuSubtypeARM64, false)
	// HLT #0 occupies the first instruction of the selected function.
	binary.LittleEndian.PutUint32(in[0x1000:], 0xd4400000)
	if _, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}}); err == nil || !strings.Contains(err.Error(), "unsupported VM instruction") {
		t.Fatalf("Process error=%v", err)
	}
}

func TestRewriteInvalidatesCodeSignatureWithoutBreakingCommands(t *testing.T) {
	in := syntheticDylib(cpuSubtypeARM64, true)
	r, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(r.Artifact); err != nil {
		t.Fatalf("rewritten signed image no longer parses: %v", err)
	}
	// The original signature command remains present but points at no blob.
	if got := binary.LittleEndian.Uint32(r.Artifact[32+uint64(segSizeForTest())+8:]); got != 0 {
		t.Fatalf("signature data offset=%d", got)
	}
}

func segSizeForTest() int { return segment64Size + section64Size }

func addMetadataCommand(in []byte, kind uint32, dataOff, dataSize uint32) []byte {
	out := append([]byte(nil), in...)
	oldN := binary.LittleEndian.Uint32(out[16:])
	oldSize := binary.LittleEndian.Uint32(out[20:])
	sz := uint32(16)
	if kind == lcDyldInfo || kind == lcDyldInfoOnly {
		sz = 48
	}
	pos := uint64(header64Size) + uint64(oldSize)
	if pos+uint64(sz) > 0x1000 {
		panic("test fixture has no headerpad")
	}
	binary.LittleEndian.PutUint32(out[pos:], kind)
	binary.LittleEndian.PutUint32(out[pos+4:], sz)
	if kind == lcDyldInfo || kind == lcDyldInfoOnly {
		// One non-empty export stream is enough to exercise all dyld-info
		// substream validation while leaving the fixture bytes deterministic.
		binary.LittleEndian.PutUint32(out[pos+40:], dataOff)
		binary.LittleEndian.PutUint32(out[pos+44:], dataSize)
	} else {
		binary.LittleEndian.PutUint32(out[pos+8:], dataOff)
		binary.LittleEndian.PutUint32(out[pos+12:], dataSize)
	}
	binary.LittleEndian.PutUint32(out[16:], oldN+1)
	binary.LittleEndian.PutUint32(out[20:], oldSize+sz)
	return out
}

func TestExistingDyldMetadataIsPreservedWhenEntryAddressesStayStable(t *testing.T) {
	cases := []struct {
		name string
		kind uint32
	}{
		{name: "function starts", kind: lcFunctionStarts},
		{name: "data in code", kind: lcDataInCode},
		{name: "exports trie", kind: lcDyldExportsTrie},
		{name: "dyld info", kind: lcDyldInfoOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := addMetadataCommand(syntheticDylib(cpuSubtypeARM64, false), tc.kind, 0x3000, 4)
			if _, err := Parse(in); err != nil {
				t.Fatalf("parse metadata fixture: %v", err)
			}
			result, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}})
			if err != nil || len(result.Artifact) == 0 {
				t.Fatalf("Process error=%v artifact=%d", err, len(result.Artifact))
			}
		})
	}
}

func TestChainedFixupsFailClosedUntilSegmentTableIsRewritten(t *testing.T) {
	in := addMetadataCommand(syntheticDylib(cpuSubtypeARM64, false), lcDyldChainedFixups, 0x3000, 4)
	_, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}})
	if err == nil || !strings.Contains(err.Error(), "dyld chained fixups") {
		t.Fatalf("Process error=%v", err)
	}
}

func TestMetadataRangeMustFitFile(t *testing.T) {
	in := addMetadataCommand(syntheticDylib(cpuSubtypeARM64, false), lcFunctionStarts, 0x4fff, 2)
	if _, err := Parse(in); err == nil || !strings.Contains(err.Error(), "LC_FUNCTION_STARTS data range") {
		t.Fatalf("Parse error=%v", err)
	}
}

func addMetadataSection(in []byte, name string) []byte {
	out := append([]byte(nil), in...)
	seg := uint64(header64Size)
	oldCmdSize := binary.LittleEndian.Uint32(out[seg+4:])
	if oldCmdSize != uint32(segment64Size+section64Size) {
		panic("test fixture segment command already has extra sections")
	}
	binary.LittleEndian.PutUint32(out[seg+4:], oldCmdSize+section64Size)
	binary.LittleEndian.PutUint32(out[seg+64:], 2)
	binary.LittleEndian.PutUint32(out[20:], binary.LittleEndian.Uint32(out[20:])+section64Size)
	sec := seg + segment64Size + section64Size
	copy(out[sec:sec+16], []byte(name))
	copy(out[sec+16:sec+32], []byte("__TEXT"))
	binary.LittleEndian.PutUint64(out[sec+32:], 0x1000)
	binary.LittleEndian.PutUint32(out[sec+48:], 0x1000)
	return out
}

func TestObjCAndCompactUnwindMetadataFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "__compact_unwind", want: "__compact_unwind"},
		{name: "__unwind_info", want: "unwind"},
		{name: "__eh_frame", want: "unwind"},
		{name: "__gcc_except_tab", want: "unwind"},
		{name: "__objc_methname", want: "Objective-C metadata"},
		{name: "__swift5_types", want: "Swift metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := addMetadataSection(syntheticDylib(cpuSubtypeARM64, false), tc.name)
			_, err := Process(in, []SelectionRequest{{Address: 0x1000, End: 0x1008}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Process error=%v, want %q", err, tc.want)
			}
		})
	}
}
