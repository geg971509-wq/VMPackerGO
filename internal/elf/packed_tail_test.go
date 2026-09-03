package elf

import (
	"debug/elf"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/arch/arm64"
	"github.com/geg971509-wq/VMPackerGO/internal/vm"
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

func TestExecutableUnselectedExternalTailBecomesNativeCallReturn(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	translator, err := arm64.NewTranslator(0x1000, 4, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Name: "caller", Address: 0x1000, End: 0x1004}
	instructions := []vm.Instruction{{Op: int(arm64.B), Offset: 0, Imm: 0x1000}}
	meta := &elfMetadata{loads: []loadMapping{{
		vaddr: 0x2000, filesz: 0x100, memsz: 0x100,
		flags: elf.PF_R | elf.PF_X,
	}}}
	if err := configureExternalTailTransfers(nil, meta, nil, selection, instructions, translator, nil); err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("native tail was rejected: %v", result.Unsupported)
	}
	if len(result.Relocations) != 1 || result.Relocations[0].TargetVA != 0x2000 {
		t.Fatalf("native tail relocation=%+v", result.Relocations)
	}
	if len(result.NativeCallSites) != 1 || result.NativeCallSites[0].ARM64Offset != 0 || result.NativeCallSites[0].VMOffset != 0 {
		t.Fatalf("native tail call sites=%+v", result.NativeCallSites)
	}
	callImage, _ := opcodes.Wire(vm.OpCallImage)
	ret, _ := opcodes.Wire(vm.OpRet)
	if len(result.Bytecode) < 11 || result.Bytecode[0] != callImage || result.Bytecode[9] != ret || result.Bytecode[10] != 0 {
		t.Fatalf("native tail bytecode prefix=%x", result.Bytecode)
	}
}

func TestUnmappedExternalTailRemainsFailClosed(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	translator, err := arm64.NewTranslator(0x1000, 4, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Name: "caller", Address: 0x1000, End: 0x1004}
	instructions := []vm.Instruction{{Op: int(arm64.B), Offset: 0, Imm: 0x1000}}
	err = configureExternalTailTransfers(nil, &elfMetadata{}, nil, selection, instructions, translator, nil)
	if err == nil {
		t.Fatal("unmapped external tail unexpectedly passed preparation")
	}
}

func TestNonTerminalNativeExternalTailRemainsFailClosed(t *testing.T) {
	opcodes := vm.IdentityOpcodeMap()
	translator, err := arm64.NewTranslator(0x1000, 8, opcodes)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Name: "caller", Address: 0x1000, End: 0x1008}
	instructions := []vm.Instruction{
		{Op: int(arm64.B), Offset: 0, Imm: 0x1000},
		{Op: int(arm64.NOP), Offset: 4},
	}
	meta := &elfMetadata{loads: []loadMapping{{
		vaddr: 0x2000, filesz: 0x100, memsz: 0x100,
		flags: elf.PF_R | elf.PF_X,
	}}}
	err = configureExternalTailTransfers(nil, meta, nil, selection, instructions, translator, nil)
	if err == nil {
		t.Fatal("non-terminal native external branch unexpectedly passed preparation")
	}
}
