package elf

import (
	"strings"
	"testing"
)

func TestFinalBytecodeCapacityCoversObservedOLLVMLayout(t *testing.T) {
	const observedFinalSize = 72836
	if err := validateFinalBytecodeSize(observedFinalSize); err != nil {
		t.Fatalf("observed %d-byte final layout should fit: %v", observedFinalSize, err)
	}
	if err := validateFinalBytecodeSize(maxFinalBytecodeSize); err != nil {
		t.Fatalf("exact maximum %d should fit: %v", maxFinalBytecodeSize, err)
	}
	err := validateFinalBytecodeSize(maxFinalBytecodeSize + 1)
	if err == nil || !strings.Contains(err.Error(), "maximum is 262144") {
		t.Fatalf("oversized layout err=%v", err)
	}
}

func TestFinalBytecodeCapacityRejectsInvalidNegativeSize(t *testing.T) {
	if err := validateFinalBytecodeSize(-1); err == nil {
		t.Fatal("negative final bytecode size was accepted")
	}
}
