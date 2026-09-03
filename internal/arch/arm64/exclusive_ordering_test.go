package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestDecoderRecognizesNonAcquireReleaseExclusiveForms(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		raw   uint32
		op    Op
		width int
		rd    int
		rn    int
		rm    int
	}{
		{0x085f7c20, LDXR, 1, 0, 1, -1},
		{0x485f7c20, LDXR, 2, 0, 1, -1},
		{0x885f7c20, LDXR, 4, 0, 1, -1},
		{0xc85f7c20, LDXR, 8, 0, 1, -1},
		{0x08027c20, STXR, 1, 0, 1, 2},
		{0x48027c20, STXR, 2, 0, 1, 2},
		{0x88027c20, STXR, 4, 0, 1, 2},
		{0xc8027c20, STXR, 8, 0, 1, 2},
	} {
		inst := decoder.Decode(tc.raw, 0)
		if got := Op(inst.Op); got != tc.op || inst.Shift != tc.width || inst.Rd != tc.rd || inst.Rn != tc.rn || inst.Rm != tc.rm {
			t.Fatalf("raw=%#08x got=%s width=%d rd=%d rn=%d rm=%d", tc.raw, OpName(got), inst.Shift, inst.Rd, inst.Rn, inst.Rm)
		}
	}
}

func TestExclusiveRegionSupportsAllLoadStoreOrderingPairs(t *testing.T) {
	decoder := NewDecoder()
	for _, tc := range []struct {
		name  string
		load  uint32
		store uint32
	}{
		{"relaxed-relaxed", 0xc85f7c20, 0xc8027c20},
		{"acquire-relaxed", 0xc85ffc20, 0xc8027c20},
		{"relaxed-release", 0xc85f7c20, 0xc802fc20},
		{"acquire-release", 0xc85ffc20, 0xc802fc20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raws := []uint32{tc.load, 0x91000400, tc.store}
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
				t.Fatalf("region=%#x want=%#x", region.Instructions, raws)
			}
			patched, _, err := PlanExclusiveThunk(region)
			if err != nil {
				t.Fatal(err)
			}
			if patched[0]&0x00008000 != tc.load&0x00008000 || patched[len(patched)-1]&0x00008000 != tc.store&0x00008000 {
				t.Fatalf("ordering bits changed: patched=%#x original=%#x", patched, raws)
			}
		})
	}
}

func TestExclusiveOrderingExtensionsRemainFailClosed(t *testing.T) {
	decoder := NewDecoder()
	cases := map[string][]uint32{
		"standalone-stxr": {0xc8027c20},
		"nested-mixed":    {0xc85f7c20, 0xc85ffc20, 0xc8027c20},
		"branch-inside":   {0xc85f7c20, 0x14000000, 0xc8027c20},
		"sp-address":      {0xc85f7fe0, 0xc8027fe0},
		"width-mismatch":  {0xc85f7c20, 0x88027c20},
	}
	for name, raws := range cases {
		t.Run(name, func(t *testing.T) {
			var instructions []vm.Instruction
			for i, raw := range raws {
				instructions = append(instructions, decoder.Decode(raw, i*4))
			}
			result := translateForPhase5(t, instructions)
			if len(result.Unsupported) == 0 {
				t.Fatalf("unsafe exclusive sequence was accepted: regions=%v", result.ExclusiveRegions)
			}
			// A nested-load sequence may expose a later independently closed inner
			// region during continued diagnostics. The enclosing function is still
			// rejected because Unsupported is non-empty, so do not misclassify that
			// diagnostic artifact as successful translation. Other unsafe cases must
			// not materialize any region at all.
			if name != "nested-mixed" && len(result.ExclusiveRegions) != 0 {
				t.Fatalf("unsafe exclusive sequence materialized regions=%v", result.ExclusiveRegions)
			}
		})
	}
}
