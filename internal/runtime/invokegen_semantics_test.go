package runtime

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/unwind"
)

func TestGenerateExceptionInvokeThunksNormalizesPersonalityThroughLocalBridge(t *testing.T) {
	for _, sourceEncoding := range []byte{
		unwind.PEAbsptr,
		unwind.PEPcrel | unwind.PESdata4,
		unwind.PEIndirect | unwind.PEPcrel | unwind.PESdata4,
	} {
		cfg := testExceptionInvokeConfig()
		cfg.Plan.PersonalityEncoding = sourceEncoding
		header, assembly, got, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg})
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
		bridge := "vm_personality_bridge_0000000000001000"
		for _, token := range []string{
			bridge + ":", "bti c", "adr x17", "ldrsw x16, [x17]",
			".cfi_personality 0x1b, " + bridge,
			"vm_invoke_routes:", "vm_invoke_1234abcd - .",
		} {
			if !strings.Contains(text, token) {
				t.Fatalf("encoding 0x%x: generated invoke assembly lacks %q", sourceEncoding, token)
			}
		}
		for _, token := range []string{
			"VM_INVOKE_ROUTE_COUNT 1u", "vm_try_exception_invoke",
			"route->landing_vm_offset", "VM_INVOKE_LANDING",
		} {
			if !strings.Contains(string(header), token) {
				t.Fatalf("encoding 0x%x: generated invoke header lacks %q", sourceEncoding, token)
			}
		}
	}
}

func TestGenerateExceptionInvokeThunksRejectsDuplicateFunctionPlans(t *testing.T) {
	first := testExceptionInvokeConfig()
	second := testExceptionInvokeConfig()
	second.Plan.Thunks[0].ID++
	second.Plan.Thunks[0].OriginalPC += 4
	second.Plan.Thunks[0].OriginalLandingPad += 4
	second.Routes[0].ThunkID = second.Plan.Thunks[0].ID
	if _, _, _, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{first, second}); err == nil {
		t.Fatal("duplicate exception plans for one function were accepted")
	}
}
