package elf

import (
	"testing"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/vm"
)

func TestSelectedExternalTailBecomesPackedTailTransfer(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	translator, err := arm64.NewTranslator(0x1000, 4, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Name: "caller", Address: 0x1000, End: 0x1004}
	instructions := []vm.Instruction{{Op: int(arm64.B), Offset: 0, Imm: 0x1000}}
	if err := configureExternalTailTransfers(nil, nil, nil, selection, instructions, translator, map[uint64]struct{}{0x2000: {}}); err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("packed tail was rejected: %v", result.Unsupported)
	}
	if len(result.Relocations) != 1 || result.Relocations[0].TargetVA != 0x2000 {
		t.Fatalf("packed tail relocation=%+v", result.Relocations)
	}
	movImage, _ := opcodes.Wire(vm.OpMovImage)
	brReg, _ := opcodes.Wire(vm.OpBrReg)
	if len(result.Bytecode) < 12 || result.Bytecode[0] != movImage || result.Bytecode[1] != 16 || result.Bytecode[10] != brReg || result.Bytecode[11] != 16 {
		t.Fatalf("packed tail bytecode prefix=%x", result.Bytecode)
	}
}

func TestUnselectedExternalTailRemainsFailClosed(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	translator, err := arm64.NewTranslator(0x1000, 4, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Name: "caller", Address: 0x1000, End: 0x1004}
	instructions := []vm.Instruction{{Op: int(arm64.B), Offset: 0, Imm: 0x1000}}
	err = configureExternalTailTransfers(nil, nil, &symbolIndex{}, selection, instructions, translator, nil)
	if err == nil {
		t.Fatal("unselected external tail unexpectedly passed preparation")
	}
}
