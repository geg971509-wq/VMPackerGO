package macho

import (
	"encoding/binary"
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
	if got := binary.LittleEndian.Uint32(r.Artifact[0x1000:]); got&0xfc000000 != 0x14000000 {
		t.Fatalf("entry is not B: 0x%x", got)
	}
}

func TestParseRejectsArm64e(t *testing.T) {
	if _, err := Parse(syntheticDylib(2, false)); err == nil {
		t.Fatal("arm64e unexpectedly accepted")
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
