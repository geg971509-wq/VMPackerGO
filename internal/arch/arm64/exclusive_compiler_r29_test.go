package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestExactR29SUBSExtIsSafeInsideBranchFreeExclusiveThunk(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0x085ffd69, // ldaxrb w9, [x11]
		0x6b2c012a, // subs w10, w9, w12, uxtb
		0x080afd6c, // stlxrb w10, w12, [x11]
	})
	if err := ValidateExclusiveRegion(region); err != nil {
		t.Fatal(err)
	}
	patched, registers, err := PlanExclusiveThunk(region)
	if err != nil {
		t.Fatal(err)
	}
	if len(patched) != 3 || len(registers) != 4 {
		t.Fatalf("patched=%#v registers=%v", patched, registers)
	}
	if Op(NewDecoder().Decode(patched[1], 4).Op) != SUBS_EXT {
		t.Fatalf("patched body raw=%08x is not SUBS_EXT", patched[1])
	}

	decoder := NewDecoder()
	instructions := make([]vm.Instruction, len(region.Instructions))
	for index, raw := range region.Instructions {
		instructions[index] = decoder.Decode(raw, index*4)
	}
	translator, err := NewTranslator(0x1000, len(instructions)*4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate(instructions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 || len(result.ExclusiveRegions) != 1 {
		t.Fatalf("unsupported=%v exclusive=%v", result.Unsupported, result.ExclusiveRegions)
	}
}
