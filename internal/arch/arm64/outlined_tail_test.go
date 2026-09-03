package arm64

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

var exactR29BaseOutlinedHelper = []uint32{
	0x4a080128,
	0x4a0a0108,
	0x4a0c0108,
	0x4a0b0100,
	0xd65f03c0,
}

var exactR29LSEOutlinedHelper = []uint32{
	0x4a0d0100,
	0xd65f03c0,
}

func TestValidateOutlinedTailHelperExactR29Shapes(t *testing.T) {
	for _, raws := range [][]uint32{exactR29BaseOutlinedHelper, exactR29LSEOutlinedHelper} {
		if err := ValidateOutlinedTailHelper(raws); err != nil {
			t.Fatalf("valid helper rejected: %v", err)
		}
	}
	for _, raws := range [][]uint32{
		{0xd65f03c0},
		{0xd503201f, 0xd65f03c0},
		{0xca0d0100, 0xd65f03c0},
		{0x4a0d0100, 0xd65f0000},
	} {
		if err := ValidateOutlinedTailHelper(raws); err == nil {
			t.Fatalf("invalid outlined helper accepted: %08x", raws)
		}
	}
}

func TestOutlinedTailInlineKeepsOriginalSourceMap(t *testing.T) {
	decoder := NewDecoder()
	branch := decoder.Decode(0x14000002, 0) // B +8, outside a 4-byte selected function.
	translator, err := NewTranslator(0x1000, 4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.SetOutlinedTailInline(0, exactR29LSEOutlinedHelper); err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate([]vm.Instruction{branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
	if result.TotalInsts != 1 || result.TransInsts != 1 {
		t.Fatalf("instruction accounting total=%d translated=%d", result.TotalInsts, result.TransInsts)
	}
	if len(result.SourceMap) != 2 || result.SourceMap[0].ARM64Offset != 0 || result.SourceMap[1].ARM64Offset != 4 {
		t.Fatalf("source map=%+v", result.SourceMap)
	}
}

func TestExternalBranchWithoutOutlinedEvidenceRemainsRejected(t *testing.T) {
	branch := NewDecoder().Decode(0x14000002, 0)
	translator, err := NewTranslator(0x1000, 4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate([]vm.Instruction{branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 1 || !strings.Contains(result.Unsupported[0], "outside function range") {
		t.Fatalf("unsupported=%v", result.Unsupported)
	}
}

func TestOutlinedTailInlineRequiresFinalBranch(t *testing.T) {
	translator, err := NewTranslator(0x1000, 8, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	if err := translator.SetOutlinedTailInline(0, exactR29LSEOutlinedHelper); err == nil {
		t.Fatal("non-tail outlined branch was accepted")
	}
}
