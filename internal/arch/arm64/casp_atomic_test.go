package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestCASPDecoderExactR29AndArchitecturalWidths(t *testing.T) {
	decoder := NewDecoder()
	cases := []struct {
		name  string
		raw   uint32
		rm    int
		rn    int
		rd    int
		width int
		order byte
	}{
		{"O0-CASP", 0x48207d02, 0, 8, 2, 8, 0},
		{"O0-CASPA", 0x48607d02, 0, 8, 2, 8, 1},
		{"O0-CASPL", 0x4820fd02, 0, 8, 2, 8, 2},
		{"O0-CASPAL", 0x4860fd02, 0, 8, 2, 8, 3},
		{"O2-CASP", 0x48267c04, 6, 0, 4, 8, 0},
		{"O2-CASPA", 0x48647c04, 4, 0, 4, 8, 1},
		{"O2-CASPL", 0x482afc02, 10, 0, 2, 8, 2},
		{"O2-CASPAL", 0x4868fc0a, 8, 0, 10, 8, 3},
		{"W-pair", 0x08207c82, 0, 4, 2, 4, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := decoder.Decode(tc.raw, 0)
			if got := Op(inst.Op); got != CASP {
				t.Fatalf("raw=%08x decoded as %s", tc.raw, OpName(got))
			}
			if inst.Rm != tc.rm || inst.Rn != tc.rn || inst.Rd != tc.rd || inst.Shift != tc.width {
				t.Fatalf("decoded=%+v", inst)
			}
			if got := atomicMemoryOrder(inst); got != tc.order {
				t.Fatalf("order=%d want=%d", got, tc.order)
			}
		})
	}
}

func TestCASPPolicyRequiresEvenBoundedPairs(t *testing.T) {
	good := vm.Instruction{Op: int(CASP), Rd: 2, Rn: 31, Rm: 4, Shift: 8, Raw: 0x48247fe2}
	if err := validateInstructionPolicy(good); err != nil {
		t.Fatalf("valid CASP rejected: %v", err)
	}
	for _, tc := range []vm.Instruction{
		{Op: int(CASP), Rd: 3, Rn: 0, Rm: 4, Shift: 8},
		{Op: int(CASP), Rd: 2, Rn: 0, Rm: 5, Shift: 8},
		{Op: int(CASP), Rd: 30, Rn: 0, Rm: 4, Shift: 8},
		{Op: int(CASP), Rd: 2, Rn: 0, Rm: 30, Shift: 8},
		{Op: int(CASP), Rd: 2, Rn: 0, Rm: 4, Shift: 16},
	} {
		if err := validateInstructionPolicy(tc); err == nil {
			t.Fatalf("invalid CASP accepted: %+v", tc)
		}
	}
}

func TestCASPReusesSevenByteAtomicWireFormat(t *testing.T) {
	inst := vm.Instruction{Op: int(CASP), Rd: 4, Rn: 6, Rm: 2, Shift: 8, Raw: 0x4862fcc4}
	result := translateForPhase5(t, []vm.Instruction{inst})
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	ops, operands := translatedOps(t, result)
	if len(ops) < 1 || ops[0] != vm.OpAtomic {
		t.Fatalf("ops=%v operands=%v", ops, operands)
	}
	want := []byte{12, 8, 3, 4, 6, 2}
	if len(operands[0]) != len(want) {
		t.Fatalf("operands=%v want=%v", operands[0], want)
	}
	for i := range want {
		if operands[0][i] != want[i] {
			t.Fatalf("operands=%v want=%v", operands[0], want)
		}
	}
	if def, ok := vm.OpcodeDefinitionFor(vm.OpAtomic); !ok || def.Size != 7 {
		t.Fatalf("OpAtomic definition=%+v ok=%v", def, ok)
	}
}

func TestCASPReservedSizeDoesNotDecodeAsPairAtomic(t *testing.T) {
	decoder := NewDecoder()
	for _, raw := range []uint32{0x88207c82, 0xc8207c82} {
		if got := Op(decoder.Decode(raw, 0).Op); got == CASP {
			t.Fatalf("reserved CASP size decoded as CASP: raw=%08x", raw)
		}
	}
}
