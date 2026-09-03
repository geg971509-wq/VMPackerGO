package arm64

import (
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func decodeExclusiveTestWords(raws []uint32) []vm.Instruction {
	decoder := NewDecoder()
	instructions := make([]vm.Instruction, len(raws))
	for i, raw := range raws {
		instructions[i] = decoder.Decode(raw, i*4)
	}
	return instructions
}

func TestExclusiveCFGKeepsShortestLinearRegion(t *testing.T) {
	raws := []uint32{
		0xc85ffc0a, // ldaxr x10, [x0]
		0x91000d4b, // add x11, x10, #3
		0xc80cfc0b, // stlxr w12, x11, [x0]
		0x35ffffac, // cbnz w12, retry (outside the thunk)
	}
	instructions := decodeExclusiveTestWords(raws)
	length, err := findExclusiveRegionLength(NewDecoder(), instructions, 0)
	if err != nil {
		t.Fatal(err)
	}
	if length != 3 {
		t.Fatalf("linear region length=%d want 3", length)
	}
	region := vm.NewExclusiveRegion(raws[:length])
	if err := ValidateExclusiveRegion(region); err != nil {
		t.Fatal(err)
	}
}

func TestExactR29ScalarBranchfulExclusiveCFG(t *testing.T) {
	raws := []uint32{
		0xc85ffc0c, // ldaxr x12, [x0]
		0xeb0b019f, // cmp x12, x11
		0x54000081, // b.ne +16 -> CLREX
		0xc80efc0d, // stlxr w14, x13, [x0]
		0x35ffff8e, // cbnz w14, retry load
		0x14000002, // b +8 -> one-past-region cleanup
		0xd5033f5f, // clrex
	}
	instructions := decodeExclusiveTestWords(append(append([]uint32(nil), raws...), 0xd503201f))
	length, err := findExclusiveRegionLength(NewDecoder(), instructions, 0)
	if err != nil {
		t.Fatal(err)
	}
	if length != len(raws) {
		t.Fatalf("branchful scalar region length=%d want %d", length, len(raws))
	}
	region := vm.NewExclusiveRegion(raws)
	patched, registers, err := PlanExclusiveThunk(region)
	if err != nil {
		t.Fatal(err)
	}
	if len(registers) != 5 {
		t.Fatalf("registers=%v", registers)
	}
	if patched[2] != raws[2] || patched[5] != raws[5] {
		t.Fatalf("PC-relative immediates changed: b.cond=%08x b=%08x", patched[2], patched[5])
	}
	if patched[4]&31 != 4 { // guest X14 is the fifth sorted guest register.
		t.Fatalf("CBNZ register was not remapped: raw=%08x registers=%v", patched[4], registers)
	}
}

func TestExactR29O0CompareFailureExitsToCleanup(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0x085ffd69, // ldaxrb w9, [x11]
		0x6b2a013f, // cmp w9, w10, uxtb
		0x54000061, // b.ne +12 -> one-past-region cleanup
		0x0808fd6c, // stlxrb w8, w12, [x11]
		0x35ffff88, // cbnz w8, retry load
	})
	if err := ValidateExclusiveRegion(region); err != nil {
		t.Fatal(err)
	}
}

func TestExactR29PairExclusiveChoosesEitherStorePath(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0xc87fc00f, // ldaxp x15, x16, [x0]
		0xeb0d01ff, // cmp x15, x13
		0x1a9f07f1, // cset w17, ne
		0xeb0c021f, // cmp x16, x12
		0x1a910631, // cinc w17, w17, ne
		0x34000091, // cbz w17, alternate store
		0xc831c00f, // stlxp w17, x15, x16, [x0]
		0x35ffff31, // cbnz w17, retry load
		0x14000003, // b one-past-region cleanup
		0xc8318c0e, // stlxp w17, x14, x3, [x0]
		0x35fffed1, // cbnz w17, retry load
	})
	patched, registers, err := PlanExclusiveThunk(region)
	if err != nil {
		t.Fatal(err)
	}
	if len(patched) != 11 || len(registers) != 8 {
		t.Fatalf("patched=%d registers=%v", len(patched), registers)
	}
	for _, index := range []int{5, 7, 8, 10} {
		decoded := NewDecoder().Decode(patched[index], index*4)
		if Op(decoded.Op) != Op(NewDecoder().Decode(region.Instructions[index], index*4).Op) {
			t.Fatalf("patched branch/store index %d changed opcode", index)
		}
	}
}

func TestExclusiveCFGRejectsUnclosedTargetsAndBackwardInteriorLoops(t *testing.T) {
	cases := []struct {
		name string
		raws []uint32
		want string
	}{
		{
			name: "target-past-region",
			raws: []uint32{0xc85ffc0c, 0xeb0b019f, 0x54000101, 0xc80efc0d},
			want: "outside closed region",
		},
		{
			name: "backward-not-entry",
			raws: []uint32{0xc85ffc0c, 0xeb0b019f, 0x35ffffec, 0xc80efc0d},
			want: "does not retry the exclusive load",
		},
		{
			name: "no-store",
			raws: []uint32{0xc85ffc0c, 0xeb0b019f, 0xd5033f5f},
			want: "no matching store-exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExclusiveRegion(vm.NewExclusiveRegion(tc.raws))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestTranslatorClosesExactR29BranchfulRegionAsOneThunk(t *testing.T) {
	raws := []uint32{
		0xc85ffc0c, 0xeb0b019f, 0x54000081, 0xc80efc0d,
		0x35ffff8e, 0x14000002, 0xd5033f5f, 0xd503201f,
	}
	instructions := decodeExclusiveTestWords(raws)
	translator, err := NewTranslator(0x1000, len(raws)*4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	if len(result.ExclusiveRegions) != 1 || len(result.ExclusiveRegions[0].Instructions) != 7 {
		t.Fatalf("exclusive regions=%+v", result.ExclusiveRegions)
	}
}
