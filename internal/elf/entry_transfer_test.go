package elf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
)

func TestBuildEntryTransferPrefersDirectBranch(t *testing.T) {
	words, err := buildEntryTransfer(0x1000, 0x2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 1 || words[0]&0xfc000000 != 0x14000000 {
		t.Fatalf("direct entry transfer=%08x", words)
	}
}

func TestBuildEntryTransferUsesADRPAddBRWhenImm26CannotReach(t *testing.T) {
	words, err := buildEntryTransfer(0x1000, 0x10001234)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 3 {
		t.Fatalf("long entry transfer has %d instructions", len(words))
	}
	if words[0]&0x9f00001f != 0x90000011 {
		t.Fatalf("ADRP X17 encoding=0x%08x", words[0])
	}
	if words[1]&0xffc003ff != 0x91000231 || (words[1]>>10)&0xfff != 0x234 {
		t.Fatalf("ADD X17,X17,#lo12 encoding=0x%08x", words[1])
	}
	if words[2] != 0xd61f0220 {
		t.Fatalf("BR X17 encoding=0x%08x", words[2])
	}
}

func TestBuildEntryTransferRejectsOutsideADRPRangeAndMisalignment(t *testing.T) {
	if _, err := buildEntryTransfer(0x1000, 0x100002000); err == nil {
		t.Fatal("entry transfer beyond ADRP range was accepted")
	}
	if _, err := buildEntryTransfer(0x1002, 0x2000); err == nil {
		t.Fatal("misaligned entry source was accepted")
	}
	if _, err := buildEntryTransfer(0x1000, 0x2002); err == nil {
		t.Fatal("misaligned entry target was accepted")
	}
}

func TestPlannedTokenTrampolineUsesInlineLongTransfer(t *testing.T) {
	input := make([]byte, 24)
	for offset := 0; offset < len(input); offset += 4 {
		binary.LittleEndian.PutUint32(input[offset:offset+4], 0xd503201f)
	}
	selection := Selection{Name: "far", Address: 0x1000, End: 0x1018, Offset: 0}
	translation := &arm64.TranslateResult{}
	patch, err := buildPlannedTokenTrampoline(input, selection, translation, 0x10001234, 0x12345678)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch) != 20 {
		t.Fatalf("long trampoline length=%d, want 20", len(patch))
	}
	if got := binary.LittleEndian.Uint32(patch[8:12]); got&0x9f00001f != 0x90000011 {
		t.Fatalf("long trampoline ADRP=0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(patch[16:20]); got != 0xd61f0220 {
		t.Fatalf("long trampoline BR=0x%08x", got)
	}
}

func TestPlannedTokenTrampolineRejectsShortFarEntry(t *testing.T) {
	input := make([]byte, 16)
	selection := Selection{Name: "far-short", Address: 0x1000, End: 0x1010, Offset: 0}
	translation := &arm64.TranslateResult{}
	if _, err := buildPlannedTokenTrampoline(input, selection, translation, 0x10001234, 1); err == nil || !strings.Contains(err.Error(), "long") {
		t.Fatalf("short far trampoline err=%v", err)
	}
}
