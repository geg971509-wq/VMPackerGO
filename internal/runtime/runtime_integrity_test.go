package runtime

import (
	"strings"
	"testing"
)

func readRuntimeTemplate(t *testing.T, name string) string {
	t.Helper()
	data, err := templates.ReadFile(templateRoot + "/" + name)
	if err != nil {
		t.Fatalf("read runtime template %s: %v", name, err)
	}
	return string(data)
}

func requireTemplateTokens(t *testing.T, name string, tokens ...string) {
	t.Helper()
	content := readRuntimeTemplate(t, name)
	for _, token := range tokens {
		if !strings.Contains(content, token) {
			t.Errorf("runtime template %s lacks fail-closed token %q", name, token)
		}
	}
}

func forbidTemplateTokens(t *testing.T, name string, tokens ...string) {
	t.Helper()
	content := readRuntimeTemplate(t, name)
	for _, token := range tokens {
		if strings.Contains(content, token) {
			t.Errorf("runtime template %s retains prohibited silent-degradation token %q", name, token)
		}
	}
}

func TestRuntimeTemplatesKeepFaultsSeparateAndFatal(t *testing.T) {
	requireTemplateTokens(t, "vm_types.h",
		"VM_FAULT_BYTECODE", "VM_FAULT_CONTROL", "VM_FAULT_RESOURCE",
		"VM_FAULT_DESCRIPTOR", "VM_FAULT_EVAL_STACK", "vm_runtime_abort",
		"VM_MEMORY_STACK_SIZE", "vm_frame_t *frames")
	requireTemplateTokens(t, "vm_interp.c",
		"vm_parse_code", "sys_mprotect", "vm_release_frames",
		"vm_runtime_abort(fault)")
	requireTemplateTokens(t, "vm_call.h",
		"vm_reserve_frame", "vm_round_mapping_size", "VM_CALL_DEPTH_MAX",
		"reverse ? (vm_offset != 0 && vm_offset <= code_len)")
}

func TestRuntimeHandlersRejectSilentSemanticDegradation(t *testing.T) {
	requireTemplateTokens(t, "vm_handlers/h_mem.h", "VM_FAULT_STACK")
	requireTemplateTokens(t, "vm_handlers/h_stack_ops.h",
		"VM_FAULT_EVAL_STACK", "vm_sdiv64")
	requireTemplateTokens(t, "vm_handlers/h_branch.h",
		"VM_FAULT_CONTROL", "target != 0 && target <= vm->bc_len")
	requireTemplateTokens(t, "vm_handlers/h_system.h",
		"VM_FAULT_CONTROL", "dedicated LR/PAC-preserving tail bridge")
	requireTemplateTokens(t, "vm_handlers/h_alu.h",
		"vm_sdiv64", "(u64)((i64)v >> 1)")

	forbidTemplateTokens(t, "vm_handlers/h_mem.h", "静默跳过")
	forbidTemplateTokens(t, "vm_handlers/h_branch.h", "安全 fall-through")
	forbidTemplateTokens(t, "vm_handlers/h_stack_ops.h",
		"? (vm)->eval_stk[(vm)->eval_sp--] : 0")
}
