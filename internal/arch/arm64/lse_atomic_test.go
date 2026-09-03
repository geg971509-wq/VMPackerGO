package arm64

import (
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestLSEAtomicDecoderFamiliesAndWidths(t *testing.T) {
	decoder := NewDecoder()
	families := []struct {
		name string
		base uint32
		op   Op
	}{
		{"SWP", 0x38208000, SWP},
		{"LDCLR", 0x38201000, LDCLR},
		{"LDEOR", 0x38202000, LDEOR},
		{"LDSET", 0x38203000, LDSET},
		{"LDSMAX", 0x38204000, LDSMAX},
		{"LDSMIN", 0x38205000, LDSMIN},
		{"LDUMAX", 0x38206000, LDUMAX},
		{"LDUMIN", 0x38207000, LDUMIN},
	}
	for _, family := range families {
		for size := uint32(0); size < 4; size++ {
			raw := family.base | size<<30 | 3<<16 | 2<<5 | 1
			inst := decoder.Decode(raw, 0)
			if got := Op(inst.Op); got != family.op {
				t.Fatalf("%s size=%d decoded as %s raw=%#08x", family.name, size, OpName(got), raw)
			}
			if inst.Shift != 1<<size || inst.Rm != 3 || inst.Rn != 2 || inst.Rd != 1 {
				t.Fatalf("%s size=%d decoded=%+v", family.name, size, inst)
			}
		}
	}
}

func TestLSEAtomicFamiliesPreserveKindWidthOrderAndRegisters(t *testing.T) {
	for _, tc := range []struct {
		inst vm.Instruction
		want []byte
	}{
		{vm.Instruction{Op: int(SWP), Rd: 1, Rn: 2, Rm: 3, Shift: 8, Raw: 3 << 22}, []byte{4, 8, 3, 1, 2, 3}},
		{vm.Instruction{Op: int(LDCLR), Rd: 4, Rn: 5, Rm: 6, Shift: 1, Raw: 1 << 23}, []byte{5, 1, 1, 4, 5, 6}},
		{vm.Instruction{Op: int(LDEOR), Rd: 7, Rn: 8, Rm: 9, Shift: 2, Raw: 1 << 22}, []byte{6, 2, 2, 7, 8, 9}},
		{vm.Instruction{Op: int(LDSET), Rd: 10, Rn: 11, Rm: 12, Shift: 4}, []byte{7, 4, 0, 10, 11, 12}},
		{vm.Instruction{Op: int(LDSMAX), Rd: 13, Rn: 14, Rm: 15, Shift: 8, Raw: 1 << 23}, []byte{8, 8, 1, 13, 14, 15}},
		{vm.Instruction{Op: int(LDSMIN), Rd: 16, Rn: 17, Rm: 18, Shift: 4, Raw: 1 << 22}, []byte{9, 4, 2, 16, 17, 18}},
		{vm.Instruction{Op: int(LDUMAX), Rd: 19, Rn: 20, Rm: 21, Shift: 2, Raw: 3 << 22}, []byte{10, 2, 3, 19, 20, 21}},
		{vm.Instruction{Op: int(LDUMIN), Rd: 22, Rn: 23, Rm: 24, Shift: 1}, []byte{11, 1, 0, 22, 23, 24}},
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

func TestLoadReturnLSESuppressesAcquireWhenRtIsXZR(t *testing.T) {
	for _, op := range []Op{LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN} {
		inst := vm.Instruction{
			Op: int(op), Rd: vm.REG_XZR, Rn: 2, Rm: 3,
			Shift: 8, Raw: 1 << 23,
		}
		result := translateForPhase5(t, []vm.Instruction{inst})
		if len(result.Unsupported) != 0 {
			t.Fatalf("%s unsupported=%v", OpName(op), result.Unsupported)
		}
		ops, operands := translatedOps(t, result)
		if len(ops) < 1 || ops[0] != vm.OpAtomic || operands[0][2] != 0 || operands[0][3] != 0xff {
			t.Fatalf("%s operands=%v", OpName(op), operands[0])
		}
	}
}

func TestLSEAtomicPolicyFailsClosedOnInvalidWidth(t *testing.T) {
	for _, op := range []Op{SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN} {
		if err := validateInstructionPolicy(vm.Instruction{Op: int(op), Rd: 1, Rn: 2, Rm: 3, Shift: 16}); err == nil {
			t.Fatalf("%s accepted invalid 16-byte scalar width", OpName(op))
		}
	}
}
