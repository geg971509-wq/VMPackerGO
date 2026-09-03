package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestDecoderRecognizesPairExclusiveForms(t *testing.T) {
	decoder := NewDecoder()
	cases := []struct {
		raw        uint32
		op         Op
		width      int
		sf         bool
		rd, rt2    int
		rn, status int
	}{
		{0xc87f0440, LDXP, 8, true, 0, 1, 2, -1},
		{0xc87f90a3, LDAXP, 8, true, 3, 4, 5, -1},
		{0xc8262127, STXP, 8, true, 7, 8, 9, 6},
		{0xc82ab1ab, STLXP, 8, true, 11, 12, 13, 10},
		{0x887f3e0e, LDXP, 4, false, 14, 15, 16, -1},
		{0x887fca71, LDAXP, 4, false, 17, 18, 19, -1},
		{0x88345af5, STXP, 4, false, 21, 22, 23, 20},
		{0x8838eb79, STLXP, 4, false, 25, 26, 27, 24},
	}
	for _, tc := range cases {
		inst := decoder.Decode(tc.raw, 0)
		if got := Op(inst.Op); got != tc.op || inst.Shift != tc.width || inst.SF != tc.sf ||
			inst.Rd != tc.rd || inst.Rt2 != tc.rt2 || inst.Rn != tc.rn || inst.Rm != tc.status {
			t.Fatalf("raw=%#08x got=%s width=%d sf=%v rd=%d rt2=%d rn=%d rm=%d", tc.raw, OpName(got), inst.Shift, inst.SF, inst.Rd, inst.Rt2, inst.Rn, inst.Rm)
		}
	}
}

func TestPairExclusiveSupportsAllOrderingBoundaryPairs(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		name        string
		load, store uint32
	}{
		{"relaxed-relaxed", 0xc87f0440, 0xc8231444},
		{"acquire-relaxed", 0xc87f8440, 0xc8231444},
		{"relaxed-release", 0xc87f0440, 0xc8239444},
		{"acquire-release", 0xc87f8440, 0xc8239444},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raws := []uint32{tc.load, tc.store}
			instructions := []vm.Instruction{decoder.Decode(tc.load, 0), decoder.Decode(tc.store, 4)}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) != 0 || len(result.ExclusiveRegions) != 1 {
				t.Fatalf("unsupported=%v regions=%v", result.Unsupported, result.ExclusiveRegions)
			}
			region := result.ExclusiveRegions[0]
			if !sameInstructionWords(region.Instructions, raws) {
				t.Fatalf("region=%#x want=%#x", region.Instructions, raws)
			}
			patched, _, err := PlanExclusiveThunk(region)
			if err != nil {
				t.Fatal(err)
			}
			if patched[0]&0x8000 != tc.load&0x8000 || patched[len(patched)-1]&0x8000 != tc.store&0x8000 {
				t.Fatalf("ordering bits changed: patched=%#x original=%#x", patched, raws)
			}
		})
	}
}

func TestPairExclusiveAcceptsAuditedCompiler128BitBody(t *testing.T) {
	decoder := NewDecoder()
	raws := []uint32{
		0xc87fa009, // ldaxp x9, x8, [x0]
		0xeb02013f, // cmp x9, x2
		0x1a9f97ea, // cset w10, hi
		0xeb03011f, // cmp x8, x3
		0x1a9fd7eb, // cset w11, gt
		0x1a8b014a, // csel w10, w10, w11, eq
		0x7100015f, // cmp w10, #0
		0x9a83110a, // csel x10, x8, x3, ne
		0x9a82112b, // csel x11, x9, x2, ne
		0xc82ca80b, // stlxp w12, x11, x10, [x0]
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
	if !sameInstructionWords(region.Instructions, raws) {
		t.Fatal("compiler-style exclusive region words changed before thunk planning")
	}
	patched, registers, err := PlanExclusiveThunk(region)
	if err != nil {
		t.Fatal(err)
	}
	if len(registers) == 0 || len(registers) > maxExclusiveThunkRegisters {
		t.Fatalf("register remap=%v", registers)
	}
	for i, raw := range patched {
		inst := decoder.Decode(raw, i*4)
		for _, field := range exclusiveRegisterFields(inst) {
			if field.register != vm.REG_XZR && (field.register < 0 || field.register >= maxExclusiveThunkRegisters) {
				t.Fatalf("patched instruction %d kept non-thunk register %d: %#08x", i, field.register, raw)
			}
		}
	}
	if Op(decoder.Decode(patched[0], 0).Op) != LDAXP || Op(decoder.Decode(patched[len(patched)-1], 0).Op) != STLXP {
		t.Fatalf("pair-exclusive boundary opcodes changed: %#x", patched)
	}
}

func TestPairExclusiveAndStatusOverlapRemainFailClosed(t *testing.T) {
	decoder := NewDecoder()
	cases := map[string][]uint32{
		"single-load-pair-store": {0xc85f7c40, 0xc8231444},
		"pair-load-single-store": {0xc87f0440, 0xc8037c44},
		"pair-width-mismatch":    {0xc87f0440, 0x88231444},
		"pair-address-mismatch":  {0xc87f0440, 0xc8231464},
		"pair-sp-address":        {0xc87f07e0, 0xc82317e4},
		"pair-load-overlap":      {0xc87f0040, 0xc8231444},
		"pair-status-data":       {0xc87f0440, 0xc8241444},
		"pair-status-base":       {0xc87f0440, 0xc8221444},
		"single-status-data":     {0xc85f7c40, 0xc8047c44},
		"single-status-base":     {0xc85f7c40, 0xc8027c44},
		"nested-pair-load":       {0xc87f0440, 0xc87f8440, 0xc8231444},
		"branch-inside-pair":     {0xc87f0440, 0x14000000, 0xc8231444},
	}
	for name, raws := range cases {
		t.Run(name, func(t *testing.T) {
			instructions := make([]vm.Instruction, len(raws))
			for i, raw := range raws {
				instructions[i] = decoder.Decode(raw, i*4)
			}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) == 0 {
				t.Fatalf("unsafe pair-exclusive sequence was accepted: regions=%v", result.ExclusiveRegions)
			}
			if name != "nested-pair-load" && len(result.ExclusiveRegions) != 0 {
				t.Fatalf("unsafe pair-exclusive sequence materialized regions=%v", result.ExclusiveRegions)
			}
		})
	}
}
