package runtime

import (
	"strings"
	"testing"
)

func TestExceptionInvokeRuntimeUsesFinalReverseInstructionBoundary(t *testing.T) {
	cfg := testExceptionInvokeConfig()
	header, _, routes, err := generateExceptionInvokeThunks([]ExceptionInvokeConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes=%+v", routes)
	}
	if routes[0].FinalVMCallOffset != cfg.Routes[0].FinalVMCallOffset ||
		routes[0].FinalVMLandingOffset != cfg.Routes[0].FinalVMLandingOffset {
		t.Fatalf("generated final route=%+v want=%+v", routes[0], cfg.Routes[0])
	}

	headerText := string(header)
	for _, token := range []string{
		"u32 call_vm_offset) {",
		"call_vm_offset > vm->bc_len",
		"call_vm_offset);",
	} {
		if !strings.Contains(headerText, token) {
			t.Errorf("generated invoke header lacks %q", token)
		}
	}
	if strings.Contains(headerText, "function_file_va, vm->pc") {
		t.Fatal("invoke route lookup regressed to the reverse instruction start")
	}

	handler, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	handlerText := string(handler)
	for _, token := range []string{
		"instruction_size + 1u > vm->bc_len - vm->pc",
		"u32 call_vm_offset = vm->pc + instruction_size + 1u;",
		"vm_try_exception_invoke(vm, address, call_vm_offset)",
	} {
		if !strings.Contains(handlerText, token) {
			t.Errorf("runtime native-call handler lacks %q", token)
		}
	}
}
