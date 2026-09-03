package runtime

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/unwind"
)

func TestGenerateExceptionInvokeThunksNormalizesPersonalityThroughPointerSlot(t *testing.T) {
	for _, sourceEncoding := range []byte{
		unwind.PEPcrel | unwind.PESdata4,
		unwind.PEIndirect | unwind.PEPcrel | unwind.PESdata4,
	} {
		cfg := testExceptionInvokeConfig()
		cfg.Plan.PersonalityEncoding = sourceEncoding
		assembly, got, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg})
		if err != nil {
			t.Fatalf("encoding 0x%x: %v", sourceEncoding, err)
		}
		if len(got) != 1 {
			t.Fatalf("encoding 0x%x: metadata=%+v", sourceEncoding, got)
		}
		item := got[0]
		if item.PersonalityEncoding != sourceEncoding {
			t.Fatalf("encoding 0x%x: preserved source encoding=0x%x", sourceEncoding, item.PersonalityEncoding)
		}
		if item.EmittedPersonalityEncoding != exceptionInvokeCFIPersonalityEncoding {
			t.Fatalf("encoding 0x%x: emitted encoding=0x%x", sourceEncoding, item.EmittedPersonalityEncoding)
		}
		text := string(assembly)
		anchor := "vm_personality_anchor_0000000000001000"
		if !strings.Contains(text, anchor+":\n.xword 0\n") {
			t.Fatalf("encoding 0x%x: personality anchor is not an explicit 8-byte pointer slot", sourceEncoding)
		}
		if !strings.Contains(text, ".cfi_personality 0x9b, "+anchor) {
			t.Fatalf("encoding 0x%x: CFI does not use normalized indirect PC-relative pointer slot", sourceEncoding)
		}
		if strings.Contains(text, ".cfi_personality 0x1b, "+anchor) {
			t.Fatalf("encoding 0x%x: direct CFI incorrectly targets the local pointer slot as the personality", sourceEncoding)
		}
	}
}

func TestGenerateExceptionInvokeThunksRejectsDuplicateFunctionPlans(t *testing.T) {
	first := testExceptionInvokeConfig()
	second := testExceptionInvokeConfig()
	second.Plan.Thunks[0].ID++
	second.Plan.Thunks[0].OriginalPC += 4
	second.Plan.Thunks[0].OriginalLandingPad += 4
	if _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{first, second}); err == nil {
		t.Fatal("duplicate exception plans for one function were accepted")
	}
}
