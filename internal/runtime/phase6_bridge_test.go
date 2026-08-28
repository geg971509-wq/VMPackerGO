package runtime

import (
	"strings"
	"testing"
)

func TestNativeBridgeCarriesCompleteAAPCS64ArgumentBanksAndCFI(t *testing.T) {
	assembly, err := templates.ReadFile(templateRoot + "/vm_native.S")
	if err != nil {
		t.Fatal(err)
	}
	source := string(assembly)
	for _, token := range []string{
		"bti c", "paciasp", "autiasp", ".cfi_startproc",
		"ldr x8", "ldp q0, q1", "ldp q6, q7", "mov sp, x22",
		"mrs x9, nzcv", "mrs x10, fpcr", "mrs x11, fpsr",
	} {
		if !strings.Contains(source, token) {
			t.Errorf("native bridge lacks %q", token)
		}
	}
}

func TestRuntimeHasNoCFunctionPointerNativeCall(t *testing.T) {
	source, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "native_fn_t") {
		t.Fatal("native calls still use a C function-pointer cast")
	}
}

func TestAtomicHelperUsesNativeAcquireReleaseAndLSEInstructions(t *testing.T) {
	assembly, err := templates.ReadFile(templateRoot + "/vm_native.S")
	if err != nil {
		t.Fatal(err)
	}
	source := string(assembly)
	for _, token := range []string{"vm_atomic_native:", ".arch_extension lse", "ldar x0", "stlr x4", "ldaddal", "casal", ".cfi_startproc"} {
		if !strings.Contains(source, token) {
			t.Errorf("atomic helper lacks %q", token)
		}
	}
}
