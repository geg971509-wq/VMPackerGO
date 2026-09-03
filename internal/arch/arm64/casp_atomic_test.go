package arm64

import (
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
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
		{"X30-expected", 0x483e7c1c, 30, 0, 28, 8, 0},
		{"X30-replacement", 0x483c7c1e, 28, 0, 30, 8, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := decoder.Decode(tc.raw, 0)
			if got := Op(inst.Op); got != CAS || !isCASPPair(inst) {
				t.Fatalf("raw=%08x decoded as %s pair=%v", tc.raw, OpName(got), isCASPPair(inst))
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

func TestCASPPolicyRequiresEvenArchitecturalPairs(t *testing.T) {
	for _, good := range []vm.Instruction{
		{Op: int(CAS), Rd: 2, Rn: 31, Rm: 4, Shift: 8, Raw: 0x48247fe2},
		{Op: int(CAS), Rd: 28, Rn: 0, Rm: 30, Shift: 8, Raw: 0x483e7c1c},
		{Op: int(CAS), Rd: 30, Rn: 0, Rm: 28, Shift: 8, Raw: 0x483c7c1e},
	} {
		if err := validateInstructionPolicy(good); err != nil {
			t.Fatalf("valid CASP rejected: %+v: %v", good, err)
		}
	}
	for _, bad := range []vm.Instruction{
		{Op: int(CAS), Rd: 3, Rn: 0, Rm: 4, Shift: 8, Raw: 0x48207c00},
		{Op: int(CAS), Rd: 2, Rn: 0, Rm: 5, Shift: 8, Raw: 0x48207c00},
		{Op: int(CAS), Rd: 2, Rn: 0, Rm: 4, Shift: 16, Raw: 0x48207c00},
	} {
		if err := validateInstructionPolicy(bad); err == nil {
			t.Fatalf("invalid CASP accepted: %+v", bad)
		}
	}
}

func TestCASPReusesSevenByteAtomicWireFormat(t *testing.T) {
	inst := vm.Instruction{Op: int(CAS), Rd: 4, Rn: 6, Rm: 2, Shift: 8, Raw: 0x4862fcc4}
	if !isCASPPair(inst) {
		t.Fatalf("test encoding is not CASP: raw=%08x", inst.Raw)
	}
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
		if inst := decoder.Decode(raw, 0); isCASPPair(inst) {
			t.Fatalf("reserved CASP size decoded as pair atomic: raw=%08x inst=%+v", raw, inst)
		}
	}
}
