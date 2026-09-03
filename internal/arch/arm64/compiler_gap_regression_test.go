package arm64

import (
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestExactCompilerLDARFamilyDoesNotAliasPairLoad(t *testing.T) {
	decoder := NewDecoder()
	cases := []struct {
		raw   uint32
		width int
	}{
		{0x08dffd08, 1}, // ldarb w8, [x8]
		{0x48dffd08, 2}, // ldarh w8, [x8]
		{0x88dffd08, 4}, // ldar w8, [x8]
		{0xc8dffd08, 8}, // ldar x8, [x8]
	}
	for _, tc := range cases {
		inst := decoder.Decode(tc.raw, 0)
		if Op(inst.Op) != LDAR || inst.Rn != 8 || inst.Rd != 8 || inst.Shift != tc.width {
			t.Fatalf("raw=%08x decoded as %s Rn=%d Rd=%d width=%d", tc.raw, OpName(Op(inst.Op)), inst.Rn, inst.Rd, inst.Shift)
		}
		if err := validateInstructionPolicy(inst); err != nil {
			t.Fatalf("raw=%08x policy: %v", tc.raw, err)
		}
	}
}

func TestExactCompilerPairSignedOffsetIsNonWritebackAddressing(t *testing.T) {
	decoder := NewDecoder()
	for _, raw := range []uint32{
		0xa9037bfd, // stp x29, x30, [sp, #0x30]
		0xa9437bfd, // ldp x29, x30, [sp, #0x30]
		0xa94039ed, // ldp x13, x14, [x15]
		0xa9003140, // stp x0, x12, [x10]
	} {
		inst := decoder.Decode(raw, 0)
		if Op(inst.Op) != LDP && Op(inst.Op) != STP {
			t.Fatalf("raw=%08x decoded as %s", raw, OpName(Op(inst.Op)))
		}
		if inst.WB != 2 {
			t.Fatalf("raw=%08x pair mode=%d want signed-offset mode 2", raw, inst.WB)
		}
		if err := validateInstructionPolicy(inst); err != nil {
			t.Fatalf("raw=%08x policy: %v", raw, err)
		}
		translator, err := NewTranslator(0x1000, 4, vm.IdentityOpcodeMap())
		if err != nil {
			t.Fatal(err)
		}
		result, err := translator.Translate([]vm.Instruction{inst})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Unsupported) != 0 {
			t.Fatalf("raw=%08x translation unsupported=%v", raw, result.Unsupported)
		}
	}
}

func TestExactCompilerLogicalImmediateUsesUnsignedBitPattern(t *testing.T) {
	decoder := NewDecoder()
	for _, raw := range []uint32{
		0xb201f3e8, // orr x8, xzr, #0xaaaaaaaaaaaaaaaa
		0xd2089d08, // eor x8, x8, #0xff00ff00ff00ff00
		0xb2410108, // orr x8, x8, #0x8000000000000000
	} {
		inst := decoder.Decode(raw, 0)
		if Op(inst.Op) != ORR_IMM && Op(inst.Op) != EOR_IMM {
			t.Fatalf("raw=%08x decoded as %s", raw, OpName(Op(inst.Op)))
		}
		if !inst.SF || inst.Imm >= 0 {
			t.Fatalf("raw=%08x did not exercise high-bit 64-bit logical immediate: SF=%v imm=%#x", raw, inst.SF, uint64(inst.Imm))
		}
		if err := validateInstructionPolicy(inst); err != nil {
			t.Fatalf("raw=%08x logical immediate policy: %v", raw, err)
		}
		translator, err := NewTranslator(0x2000, 4, vm.IdentityOpcodeMap())
		if err != nil {
			t.Fatal(err)
		}
		result, err := translator.Translate([]vm.Instruction{inst})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Unsupported) != 0 {
			t.Fatalf("raw=%08x translation unsupported=%v", raw, result.Unsupported)
		}
	}
}
