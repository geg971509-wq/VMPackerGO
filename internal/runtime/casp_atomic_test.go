package runtime

import (
	"strings"
	"testing"
)

func TestCASPRuntimeTemplatesPreserveSevenBytePairTransport(t *testing.T) {
	nativeS, err := templates.ReadFile(templateRoot + "/vm_native.S")
	if err != nil {
		t.Fatal(err)
	}
	nativeH, err := templates.ReadFile(templateRoot + "/vm_native.h")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}

	assembly := string(nativeS)
	for _, token := range []string{
		"vm_atomic_pair_native:",
		"casp w8, w9, w10, w11, [x2]",
		"caspa w8, w9, w10, w11, [x2]",
		"caspl w8, w9, w10, w11, [x2]",
		"caspal w8, w9, w10, w11, [x2]",
		"casp x8, x9, x10, x11, [x2]",
		"caspa x8, x9, x10, x11, [x2]",
		"caspl x8, x9, x10, x11, [x2]",
		"caspal x8, x9, x10, x11, [x2]",
		"mov x0, x8", "mov x1, x9", "mov w0, w8", "mov w1, w9",
	} {
		if !strings.Contains(assembly, token) {
			t.Errorf("vm_native.S lacks %q", token)
		}
	}

	header := string(nativeH)
	for _, token := range []string{
		"typedef struct", "vm_atomic_pair_t", "u64 lo;", "u64 hi;",
		"vm_atomic_pair_native",
	} {
		if !strings.Contains(header, token) {
			t.Errorf("vm_native.h lacks %q", token)
		}
	}

	h := string(handler)
	for _, token := range []string{
		"kind == 12", "rd > 28", "rm > 28", "rd & 1u", "rm & 1u",
		"vm_atomic_pair_native", "rm + 1", "rd + 1",
		"vm_atomic_reg_write(vm, rm, old.lo, width)",
		"vm_atomic_reg_write(vm, rm + 1, old.hi, width)",
		"return 7;",
	} {
		if !strings.Contains(h, token) {
			t.Errorf("h_system.h lacks %q", token)
		}
	}
}
