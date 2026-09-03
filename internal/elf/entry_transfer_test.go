package elf

import "testing"

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
