package arm64

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestFlagSettingInstructionsUseWidthAwareOpcodes(t *testing.T) {
	tests := []struct {
		name string
		inst vm.Instruction
		want vm.Opcode
	}{
		{"adds32", vm.Instruction{Op: int(ADDS_REG), Rd: 0, Rn: 1, Rm: 2}, vm.OpSAddFlags},
		{"subs64", vm.Instruction{Op: int(SUBS_REG), Rd: 0, Rn: 1, Rm: 2, SF: true}, vm.OpSSubFlags},
		{"ands32", vm.Instruction{Op: int(ANDS_REG), Rd: vm.REG_XZR, Rn: 1, Rm: 2}, vm.OpSAndFlags},
		{"adcs64", vm.Instruction{Op: int(ADCS), Rd: 0, Rn: 1, Rm: 2, SF: true}, vm.OpSAdcFlags},
		{"sbcs32", vm.Instruction{Op: int(SBCS), Rd: 0, Rn: 1, Rm: 2}, vm.OpSSbcFlags},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.inst.Offset = 0
			result := translateForPhase5(t, []vm.Instruction{tc.inst})
			ops, operands := translatedOps(t, result)
			index := opcodeIndex(ops, tc.want)
			if index < 0 {
				t.Fatalf("ops=%v do not contain %s", ops, vm.OpcodeName(tc.want))
			}
			wantSF := byte(0)
			if tc.inst.SF {
				wantSF = 1
			}
			if len(operands[index]) != 1 || operands[index][0] != wantSF {
				t.Fatalf("%s operands=%v, want sf=%d", vm.OpcodeName(tc.want), operands[index], wantSF)
			}
		})
	}
}

func TestUMULHUsesBothSourceRegisters(t *testing.T) {
	result := translateForPhase5(t, []vm.Instruction{{Op: int(UMULH), Rd: 0, Rn: 1, Rm: 2, SF: true}})
	ops, operands := translatedOps(t, result)
	want := []vm.Opcode{vm.OpSVload, vm.OpSVload, vm.OpSUmulh, vm.OpSVstore, vm.OpHalt}
	if len(ops) != len(want) {
		t.Fatalf("ops=%v, want %v", ops, want)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("ops[%d]=%s, want %s", i, vm.OpcodeName(ops[i]), vm.OpcodeName(want[i]))
		}
	}
	if operands[0][0] != 1 || operands[1][0] != 2 {
		t.Fatalf("UMULH sources=%v/%v, want R1/R2", operands[0], operands[1])
	}
}

func TestEveryConditionUsesArchitecturalJCond(t *testing.T) {
	for cond := 0; cond < 16; cond++ {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(B_COND), Cond: cond, Imm: 4}})
		ops, operands := translatedOps(t, result)
		if len(ops) != 2 || ops[0] != vm.OpJCond || ops[1] != vm.OpHalt {
			t.Fatalf("cond=%d ops=%v", cond, ops)
		}
		if len(operands[0]) != 5 || operands[0][0] != byte(cond) {
			t.Fatalf("cond=%d operands=%v", cond, operands[0])
		}
		if target := binary.LittleEndian.Uint32(operands[0][1:]); target != 6 {
			t.Fatalf("cond=%d target=%d, want 6", cond, target)
		}
	}
}

func TestCBZDoesNotModifyNZCVAndCarriesWidth(t *testing.T) {
	for _, sf := range []bool{false, true} {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(CBZ), Rd: 3, Imm: 4, SF: sf}})
		ops, operands := translatedOps(t, result)
		if len(ops) != 2 || ops[0] != vm.OpCbz || ops[1] != vm.OpHalt {
			t.Fatalf("sf=%v ops=%v", sf, ops)
		}
		wantReg := byte(3)
		if sf {
			wantReg |= 0x80
		}
		if operands[0][0] != wantReg {
			t.Fatalf("sf=%v encoded register=0x%02x, want 0x%02x", sf, operands[0][0], wantReg)
		}
		if opcodeIndex(ops, vm.OpSCmp) >= 0 {
			t.Fatal("CBZ unexpectedly modifies NZCV")
		}
	}
}

func TestSystemSemanticsFailClosed(t *testing.T) {
	tests := []vm.Instruction{
		{Op: int(MRS), Rd: 0, Imm: 0x1234, SF: true},
		{Op: int(MSR_WRITE), Rd: 0, Imm: 0x5A10, SF: true},
		{Op: int(DMB)},
		{Op: int(PRFM)},
	}
	for _, inst := range tests {
		result := translateForPhase5(t, []vm.Instruction{inst})
		if len(result.Unsupported) != 1 {
			t.Fatalf("%s unsupported=%v", OpName(Op(inst.Op)), result.Unsupported)
		}
		if !strings.Contains(result.Unsupported[0], "offset") {
			t.Fatalf("path lacks diagnostic: %q", result.Unsupported[0])
		}
	}
}

func TestPointerAuthenticationUsesNativeHelperOpcode(t *testing.T) {
	ops := []Op{PACIASP, AUTIASP, PACIAZ, AUTIAZ, PACIBSP, AUTIBSP, XPACLRI}
	for kind, op := range ops {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(op)}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(op), result.Unsupported)
		}
		translated, operands := translatedOps(t, result)
		if len(translated) != 2 || translated[0] != vm.OpPAuth || operands[0][0] != byte(kind) {
			t.Fatalf("%s ops=%v operands=%v", OpName(op), translated, operands)
		}
	}
}

func TestBTIIsPreservedAsEntryMetadataOnly(t *testing.T) {
	result := translateForPhase5(t, []vm.Instruction{{Op: int(BTI_C)}})
	if len(result.Unsupported) != 0 || !result.HasEntryBTI || result.EntryBTI != BTI_C {
		t.Fatalf("entry BTI result=%+v", result)
	}
	result = translateForPhase5(t, []vm.Instruction{{Op: int(NOP)}, {Op: int(BTI_J)}})
	if len(result.Unsupported) != 1 {
		t.Fatalf("non-entry BTI unsupported=%v", result.Unsupported)
	}
}

func TestSVCImmediatesAreCollectedForPerPackNativeThunks(t *testing.T) {
	result := translateForPhase5(t, []vm.Instruction{
		{Op: int(SVC), Imm: 0x1234},
		{Op: int(SVC), Imm: 0},
		{Op: int(SVC), Imm: 0x1234},
	})
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	want := []uint16{0, 0x1234}
	if len(result.SVCImmediates) != len(want) {
		t.Fatalf("SVC immediates=%v, want %v", result.SVCImmediates, want)
	}
	for i := range want {
		if result.SVCImmediates[i] != want[i] {
			t.Fatalf("SVC immediates=%v, want %v", result.SVCImmediates, want)
		}
	}
}

func TestProductWhitelistCoversEveryDecodedOperation(t *testing.T) {
	for op := UNKNOWN; op <= UNSUPPORTED; op++ {
		if _, ok := instructionRules[op]; !ok {
			t.Errorf("%s has no product whitelist rule", OpName(op))
		}
	}
}

func TestRegisterOffsetWhitelist(t *testing.T) {
	for option := uint32(0); option < 8; option++ {
		inst := vm.Instruction{Op: int(LDR_REG), Rd: 0, Rn: 1, Rm: 2, SF: true, Raw: option << 13}
		err := validateInstructionPolicy(inst)
		allowed := option == 2 || option == 3 || option == 6 || option == 7
		if allowed && err != nil {
			t.Errorf("option=%d rejected: %v", option, err)
		}
		if !allowed && err == nil {
			t.Errorf("reserved option=%d accepted", option)
		}
	}
}

func TestCSELUsesAllConditionsWithoutXZRRegisterClobber(t *testing.T) {
	for cond := 0; cond < 16; cond++ {
		result := translateForPhase5(t, []vm.Instruction{{Op: int(CSEL), Rd: 0, Rn: vm.REG_XZR, Rm: vm.REG_XZR, Cond: cond, SF: true}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("cond=%d unsupported=%v", cond, result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) == 0 || ops[0] != vm.OpJCond || operands[0][0] != byte(cond) {
			t.Fatalf("cond=%d ops=%v operands=%v", cond, ops, operands)
		}
		for i, op := range ops {
			if op == vm.OpSVstore && len(operands[i]) == 1 && operands[i][0] == 16 {
				t.Fatalf("cond=%d writes the XZR placeholder through architectural X16", cond)
			}
		}
	}
}

func TestRegisterOffsetEmitsArchitecturalExtension(t *testing.T) {
	tests := []struct {
		option uint32
		want   vm.Opcode
	}{
		{2, vm.OpSTrunc32}, // UXTW
		{6, vm.OpSSext32},  // SXTW
	}
	for _, tc := range tests {
		raw := uint32(0xF8600800) | tc.option<<13
		result := translateForPhase5(t, []vm.Instruction{{Op: int(LDR_REG), Rd: 0, Rn: 1, Rm: 2, SF: true, Raw: raw}})
		if len(result.Unsupported) != 0 {
			t.Fatalf("option=%d unsupported=%v", tc.option, result.Unsupported)
		}
		ops, _ := translatedOps(t, result)
		if opcodeIndex(ops, tc.want) < 0 {
			t.Fatalf("option=%d ops=%v do not contain %s", tc.option, ops, vm.OpcodeName(tc.want))
		}
	}
}

func TestPCRelativeInstructionsProduceTypedImageRelocations(t *testing.T) {
	tests := []struct {
		name       string
		inst       vm.Instruction
		wantOp     vm.Opcode
		wantTarget uint64
	}{
		{"adr", vm.Instruction{Op: int(ADR), Rd: 0, Imm: 0x24, SF: true}, vm.OpMovImage, 0x1024},
		{"ldr-literal", vm.Instruction{Op: int(LDR_LIT), Rd: 0, Imm: -0x10, SF: true}, vm.OpSPushImage, 0x0FF0},
		{"bl", vm.Instruction{Op: int(BL), Imm: 0x40}, vm.OpCallImage, 0x1040},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := translateForPhase5(t, []vm.Instruction{tc.inst})
			if len(result.Unsupported) != 0 {
				t.Fatalf("unsupported=%v", result.Unsupported)
			}
			ops, _ := translatedOps(t, result)
			if len(ops) == 0 || ops[0] != tc.wantOp {
				t.Fatalf("ops=%v, want first %s", ops, vm.OpcodeName(tc.wantOp))
			}
			if len(result.Relocations) != 1 || result.Relocations[0].TargetVA != tc.wantTarget {
				t.Fatalf("relocations=%v, want target 0x%x", result.Relocations, tc.wantTarget)
			}
			off := result.Relocations[0].Offset
			if off < 0 || off+8 > result.CodeLen || binary.LittleEndian.Uint64(result.Bytecode[off:off+8]) != 0 {
				t.Fatalf("relocation placeholder offset=%d codeLen=%d", off, result.CodeLen)
			}
		})
	}
}

func TestADRPAddProducesOneFinalImageRelocation(t *testing.T) {
	instructions := []vm.Instruction{
		{Op: int(ADRP), Rd: 3, Imm: 0x2000, SF: true},
		{Op: int(ADD_IMM), Rd: 3, Rn: 3, Imm: 0x128, SF: true},
	}
	result := translateForPhase5(t, instructions)
	if len(result.Unsupported) != 0 || len(result.Relocations) != 1 {
		t.Fatalf("unsupported=%v relocations=%v", result.Unsupported, result.Relocations)
	}
	if got := result.Relocations[0].TargetVA; got != 0x3128 {
		t.Fatalf("target=0x%x, want 0x3128", got)
	}
}

func translateForPhase5(t *testing.T, instructions []vm.Instruction) *TranslateResult {
	t.Helper()
	for i := range instructions {
		instructions[i].Offset = i * 4
	}
	translator, err := NewTranslator(0x1000, len(instructions)*4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func translatedOps(t *testing.T, result *TranslateResult) ([]vm.Opcode, [][]byte) {
	t.Helper()
	var ops []vm.Opcode
	var operands [][]byte
	opcodes := vm.IdentityOpcodeMap()
	for pc := 0; pc < result.CodeLen; {
		op, err := opcodes.Decode(result.Bytecode[pc])
		if err != nil {
			t.Fatalf("pc=%d: %v", pc, err)
		}
		size := vm.InstructionSize(op)
		if size <= 0 || pc+size > result.CodeLen {
			t.Fatalf("pc=%d op=%d size=%d codeLen=%d", pc, op, size, result.CodeLen)
		}
		ops = append(ops, op)
		operands = append(operands, append([]byte(nil), result.Bytecode[pc+1:pc+size]...))
		pc += size
	}
	return ops, operands
}

func opcodeIndex(ops []vm.Opcode, want vm.Opcode) int {
	for i, op := range ops {
		if op == want {
			return i
		}
	}
	return -1
}
