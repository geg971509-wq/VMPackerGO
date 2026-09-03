package arm64

import (
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestSIMDStructureTransfersPreserveVectorRegisterAndSpan(t *testing.T) {
	for _, tc := range []struct {
		op   Op
		want vm.Opcode
	}{
		{LD1_16B, vm.OpVld16},
		{ST1_16B, vm.OpVst16},
	} {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(tc.op), Rd: 31, Rn: 5, Imm: 64}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(tc.op), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != tc.want {
			t.Fatalf("%s ops=%v", OpName(tc.op), ops)
		}
		if got := operands[0]; len(got) != 3 || got[0] != 31 || got[1] != 5 || got[2] != 64 {
			t.Fatalf("%s operands=%v", OpName(tc.op), got)
		}
	}
}

func TestNativeRegisterCallsUseValidatedAssemblyBridgeOpcodes(t *testing.T) {
	for _, tc := range []struct {
		op   Op
		want vm.Opcode
	}{
		{BLR, vm.OpCallReg},
		{BR, vm.OpBrReg},
	} {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(tc.op), Rn: 9}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(tc.op), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != tc.want || len(operands[0]) != 1 || operands[0][0] != 9 {
			t.Fatalf("%s ops=%v operands=%v", OpName(tc.op), ops, operands)
		}
	}
}

func TestTranslatorRecordsThrowCapableNativeCallLocations(t *testing.T) {
	result := translateForPhase5(t, []vm.Instruction{
		{Op: int(BL), Offset: 0, Imm: 0x100},
		{Op: int(NOP), Offset: 4},
		{Op: int(BLR), Offset: 8, Rn: 9},
		{Op: int(BR), Offset: 12, Rn: 10},
	})
	if len(result.Unsupported) != 0 || len(result.NativeCallSites) != 2 {
		t.Fatalf("unsupported=%v calls=%+v", result.Unsupported, result.NativeCallSites)
	}
	if result.NativeCallSites[0].ARM64Offset != 0 || result.NativeCallSites[1].ARM64Offset != 8 || result.NativeCallSites[0].VMOffset >= result.NativeCallSites[1].VMOffset {
		t.Fatalf("calls=%+v", result.NativeCallSites)
	}
}

func TestNativeBridgePolicyRejectsInvalidStackPointerRegister(t *testing.T) {
	if err := validateInstructionPolicy(vm.Instruction{Op: int(BLR), Rn: 31}); err == nil {
		t.Fatal("BLR X31 was accepted")
	}
}

func TestArchitecturalBarriersPreserveKindAndDomain(t *testing.T) {
	for _, tc := range []struct {
		op     Op
		raw    uint32
		kind   byte
		option byte
	}{
		{DMB, 0xD5033BBF, 0, 0xb},
		{DSB, 0xD5033F9F, 1, 0xf},
		{ISB, 0xD5033FDF, 2, 0xf},
	} {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(tc.op), Raw: tc.raw}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(tc.op), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != vm.OpBarrier || len(operands[0]) != 2 || operands[0][0] != tc.kind || operands[0][1] != tc.option {
			t.Fatalf("%s ops=%v operands=%v", OpName(tc.op), ops, operands)
		}
	}
}

func TestReservedBarrierOptionsFailClosed(t *testing.T) {
	for _, op := range []Op{DMB, DSB, ISB} {
		if err := validateInstructionPolicy(vm.Instruction{Op: int(op), Raw: 0xD50330BF}); err == nil {
			t.Fatalf("%s reserved option accepted", OpName(op))
		}
	}
}

func TestNativeAtomicsPreserveWidthOrderAndRegisters(t *testing.T) {
	for _, tc := range []struct {
		inst vm.Instruction
		want []byte
	}{
		{vm.Instruction{Op: int(LDAR), Rd: vm.REG_XZR, Rn: 2, Shift: 8}, []byte{0, 8, 1, 0xff, 2, 0xff}},
		{vm.Instruction{Op: int(STLR), Rd: 1, Rn: 2, Shift: 4}, []byte{1, 4, 2, 1, 2, 0xff}},
		{vm.Instruction{Op: int(LDADD), Rd: 1, Rn: 2, Rm: 3, Shift: 8, Raw: 3 << 22}, []byte{2, 8, 3, 1, 2, 3}},
		{vm.Instruction{Op: int(CAS), Rd: 1, Rn: 2, Rm: 3, Shift: 4, Raw: 1<<22 | 1<<15}, []byte{3, 4, 3, 1, 2, 3}},
	} {
		result := translateForPhase5(t, []vm.Instruction{tc.inst})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(Op(tc.inst.Op)), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != vm.OpAtomic || len(operands[0]) != len(tc.want) {
			t.Fatalf("%s ops=%v operands=%v", OpName(Op(tc.inst.Op)), ops, operands)
		}
		for i := range tc.want {
			if operands[0][i] != tc.want[i] {
				t.Fatalf("%s operands=%v want=%v", OpName(Op(tc.inst.Op)), operands[0], tc.want)
			}
		}
	}
}

func TestClosedExclusiveRegionBecomesOneContinuousThunkOperation(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc85ffc20, // ldaxr x0, [x1]
		0x91000400, // add x0, x0, #1
		0xc802fc20, // stlxr w2, x0, [x1]
	}
	instructions := make([]vm.Instruction, len(raws))
	for i, raw := range raws {
		instructions[i] = decoder.Decode(raw, i*4)
	}
	result := translateForPhase5(t, instructions)
	if len(result.Unsupported) != 0 || len(result.ExclusiveRegions) != 1 {
		t.Fatalf("unsupported=%v regions=%v", result.Unsupported, result.ExclusiveRegions)
	}
	region := result.ExclusiveRegions[0]
	if !region.Valid() || len(region.Instructions) != len(raws) {
		t.Fatalf("invalid region=%#v", region)
	}
	ops, operands := translatedOps(t, result)
	if len(ops) < 1 || ops[0] != vm.OpExclusive || len(operands[0]) != 4 {
		t.Fatalf("ops=%v operands=%v", ops, operands)
	}
}

func TestClosedExclusiveRegionSupportsHighGuestRegisters(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc85ffe34,
		0x91000694,
		0xc813fe34,
	}
	instructions := make([]vm.Instruction, len(raws))
	for i, raw := range raws {
		instructions[i] = decoder.Decode(raw, i*4)
	}
	result := translateForPhase5(t, instructions)
	if len(result.Unsupported) != 0 || len(result.ExclusiveRegions) != 1 {
		t.Fatalf("unsupported=%v regions=%v", result.Unsupported, result.ExclusiveRegions)
	}
	patched, registers, err := PlanExclusiveThunk(result.ExclusiveRegions[0])
	if err != nil {
		t.Fatal(err)
	}
	wantInstructions := []uint32{0xc85ffc02, 0x91000442, 0xc801fc02}
	if !sameInstructionWords(patched, wantInstructions) {
		t.Fatalf("instructions=%#x want=%#x", patched, wantInstructions)
	}
	wantRegisters := []int{17, 19, 20}
	if len(registers) != len(wantRegisters) {
		t.Fatalf("registers=%v", registers)
	}
	for i, want := range wantRegisters {
		if registers[i] != want {
			t.Fatalf("register[%d]=%v want=%v", i, registers[i], want)
		}
	}
}

func TestExclusiveThunkRejectsMoreThanSixteenGuestRegisters(t *testing.T) {
	raws := []uint32{0xc85ffe00}
	for reg := uint32(1); reg <= 15; reg++ {
		raws = append(raws, 0x91000000|(reg<<5)|reg)
	}
	raws = append(raws, 0xc801fe00)
	region := vm.NewExclusiveRegion(raws)
	if _, _, err := PlanExclusiveThunk(region); err == nil {
		t.Fatal("exclusive thunk accepted more than sixteen distinct guest registers")
	}
}

func TestExclusiveRegionsFailClosedWhenUnclosedOrUnsafe(t *testing.T) {
	decoder := NewDecoder()
	for name, raws := range map[string][]uint32{
		"unclosed":         {0xc85ffc20},
		"standalone-store": {0xc802fc20},
		"branch-inside":    {0xc85ffc20, 0x14000000, 0xc802fc20},
		"sp-address":       {0xc85fffe0, 0xc802ffe0},
	} {
		var instructions []vm.Instruction
		for i, raw := range raws {
			instructions = append(instructions, decoder.Decode(raw, i*4))
		}
		result := translateForPhase5(t, instructions)
		if len(result.Unsupported) == 0 || len(result.ExclusiveRegions) != 0 {
			t.Errorf("%s unsupported=%v regions=%v", name, result.Unsupported, result.ExclusiveRegions)
		}
	}
}
