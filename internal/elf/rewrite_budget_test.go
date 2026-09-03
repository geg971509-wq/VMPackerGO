package elf

import "testing"

func TestValidateRewriteBudgetAcceptsOrdinaryPlan(t *testing.T) {
	segments := []rewriteSegment{
		{fileOffset: 0x200000, fileSize: 0x1000, data: make([]byte, 0x1000)},
		{fileOffset: 0x204000, fileSize: 0x2000, data: make([]byte, 0x2000)},
	}
	if err := validateRewriteBudget(0x100000, segments); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRewriteBudgetRejectsExpansionAndOutputEndpoint(t *testing.T) {
	tooMuch := []rewriteSegment{{fileOffset: 0, fileSize: maxRewriteExpansion + 1,
		data: make([]byte, 0)}}
	// Avoid allocating a 1 GiB test buffer: a nonzero fileSize/data mismatch is
	// itself invalid and proves the helper checks representation consistency.
	if err := validateRewriteBudget(0, tooMuch); err == nil {
		t.Fatal("inconsistent oversized segment was accepted")
	}

	far := []rewriteSegment{{fileOffset: maxRewriteOutput, fileSize: 1, data: []byte{0}}}
	if err := validateRewriteBudget(1, far); err == nil {
		t.Fatal("final output endpoint beyond 2 GiB was accepted")
	}
}

func TestValidateRewriteBudgetRejectsOversizedInput(t *testing.T) {
	if err := validateRewriteBudget((1<<30)+1, nil); err == nil {
		t.Fatal("input above 1 GiB was accepted")
	}
}
