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

func TestMRSReadsVMBackedFPStatus(t *testing.T) {
	systemBytes, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	system := string(systemBytes)
	for _, token := range []string{
		"case 0x5A20", "value = vm->FPCR",
		"case 0x5A21", "value = vm->FPSR",
	} {
		if !strings.Contains(system, token) {
			t.Fatalf("MRS runtime lacks %q", token)
		}
	}
}

func TestMSRWritesVMBackedSystemState(t *testing.T) {
	systemBytes, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	system := string(systemBytes)
	for _, token := range []string{
		"static inline u32 h_msr",
		"source == 0xff ? 0 : vm->R[source & 31]",
		"case 0x5A10",
		"vm->FL = (u32)((value >> 28) & 0xfu)",
		"case 0x5A20",
		"vm->FPCR = (u32)value",
		"case 0x5A21",
		"vm->FPSR = (u32)value",
	} {
		if !strings.Contains(system, token) {
			t.Fatalf("MSR runtime lacks %q", token)
		}
	}

	decodeBytes, err := templates.ReadFile(templateRoot + "/vm_decode.h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decodeBytes), "case OP_MSR:") {
		t.Fatal("MSR opcode has no runtime instruction size")
	}

	dispatchBytes, err := templates.ReadFile(templateRoot + "/vm_dispatch.h")
	if err != nil {
		t.Fatal(err)
	}
	dispatch := string(dispatchBytes)
	if !strings.Contains(dispatch, "return h_msr(vm);") ||
		!strings.Contains(dispatch, "tbl[OP_MSR] = hw_msr;") {
		t.Fatal("MSR opcode is not wired into runtime dispatch")
	}
}

func TestPackedTailReplacesContextWithoutGrowingCallDepth(t *testing.T) {
	callBytes, err := templates.ReadFile(templateRoot + "/vm_call.h")
	if err != nil {
		t.Fatal(err)
	}
	callSource := string(callBytes)
	start := strings.Index(callSource, "static inline int vm_try_packed_tail")
	if start < 0 {
		t.Fatal("packed tail helper is missing")
	}
	end := strings.Index(callSource[start:], "\n}\n")
	if end < 0 {
		t.Fatal("packed tail helper body is incomplete")
	}
	tailBody := callSource[start : start+end+3]
	for _, token := range []string{
		"vm_lookup_packed", "vm_load_func",
		"old_bc_buf != vm->root_bc_buf",
		"sys_munmap(old_bc_buf, old_bc_alloc)",
	} {
		if !strings.Contains(tailBody, token) {
			t.Errorf("packed tail helper lacks %q", token)
		}
	}
	for _, token := range []string{
		"vm->depth++", "vm->depth--", "vm->R[30] =", "vm_try_packed_call",
	} {
		if strings.Contains(tailBody, token) {
			t.Errorf("packed tail helper changes call semantics via %q", token)
		}
	}

	typesBytes, err := templates.ReadFile(templateRoot + "/vm_types.h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(typesBytes), "u8 *root_bc_buf;") {
		t.Fatal("VM context does not retain root bytecode ownership")
	}

	systemBytes, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	systemSource := string(systemBytes)
	start = strings.Index(systemSource, "static inline u32 h_br_reg")
	if start < 0 {
		t.Fatal("BR_REG handler is missing")
	}
	endMarker := strings.Index(systemSource[start:], "static inline u32 h_vld16")
	if endMarker < 0 {
		t.Fatal("BR_REG handler is incomplete")
	}
	brBody := systemSource[start : start+endMarker]
	if !strings.Contains(brBody, "vm_try_packed_tail(vm, address)") ||
		!strings.Contains(brBody, "vm_fault_set(vm, VM_FAULT_CONTROL)") {
		t.Fatal("BR_REG does not use transactional packed-tail replacement with native fail-closed fallback")
	}
	if strings.Contains(brBody, "vm_native_call") ||
		strings.Contains(brBody, "vm_prepare_native_call") {
		t.Fatal("BR_REG still implements native tail as a returning native call")
	}

	interpBytes, err := templates.ReadFile(templateRoot + "/vm_interp.c")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(interpBytes), "current_bytecode != root_bytecode") {
		t.Fatal("top-level packed tail buffer is not released separately from the root buffer")
	}
}

func TestTrailerFunctionAddressUsesImageBias(t *testing.T) {
	callBytes, err := templates.ReadFile(templateRoot + "/vm_call.h")
	if err != nil {
		t.Fatal(err)
	}
	source := string(callBytes)
	start := strings.Index(source, "static inline int vm_parse_code")
	if start < 0 {
		t.Fatal("transactional bytecode parser is missing")
	}
	end := strings.Index(source[start:], "static inline void vm_install_code")
	if end < 0 {
		t.Fatal("transactional bytecode parser body is incomplete")
	}
	body := source[start : start+end]
	if !strings.Contains(body, "vm_file_bias(vm, &bias)") ||
		!strings.Contains(body, "state->func_addr = func_file_va + bias") {
		t.Fatal("trailer function address is not validated and rebased from file VA to runtime VA")
	}
}

func TestReturningNativeBridgeIsNotAcceptedAsTailHandoff(t *testing.T) {
	assemblyBytes, err := templates.ReadFile(templateRoot + "/vm_native.S")
	if err != nil {
		t.Fatal(err)
	}
	assembly := string(assemblyBytes)
	start := strings.Index(assembly, "vm_native_call:")
	if start < 0 {
		t.Fatal("native call bridge is missing")
	}
	end := strings.Index(assembly[start:], ".size vm_native_call")
	if end < 0 {
		t.Fatal("native call bridge is incomplete")
	}
	bridge := assembly[start : start+end]
	if !strings.Contains(bridge, "blr x20") || !strings.Contains(bridge, "ret") {
		t.Fatal("native call bridge no longer has the expected returning call shape")
	}
	if strings.Contains(bridge, "br x20") {
		t.Fatal("native call bridge unexpectedly contains a non-returning target handoff")
	}

	systemBytes, err := templates.ReadFile(templateRoot + "/vm_handlers/h_system.h")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(systemBytes), "vm_prepare_native_call(vm, address, 1)") {
		t.Fatal("runtime still routes a tail through the returning native call bridge")
	}
}

func TestTokenEntryPreservesCallerContinuationAcrossPackedTail(t *testing.T) {
	entryBytes, err := templates.ReadFile(templateRoot + "/vm_entry.S")
	if err != nil {
		t.Fatal(err)
	}
	entry := string(entryBytes)
	for _, token := range []string{
		"mov x10, x30",
		"paciasp",
		"stp x9, x10, [sp, #80]",
		"bl vm_entry_token_inner",
		"ldp x29, x30, [sp], #96",
		"autiasp",
		"ret",
	} {
		if !strings.Contains(entry, token) {
			t.Fatalf("token entry does not preserve caller continuation: missing %q", token)
		}
	}
	if strings.Index(entry, "mov x10, x30") > strings.Index(entry, "paciasp") {
		t.Fatal("token entry captures caller LR only after signing its own return address")
	}
	if strings.Index(entry, "ldp x29, x30, [sp], #96") > strings.Index(entry, "autiasp") {
		t.Fatal("token entry authenticates before restoring its signed return address")
	}
}

func TestAtomicHelperUsesNativeAcquireReleaseAndLSEInstructions(t *testing.T) {
	assembly, err := templates.ReadFile(templateRoot + "/vm_native.S")
	if err != nil {
		t.Fatal(err)
	}
	source := string(assembly)
	for _, token := range []string{
		"vm_atomic_native:", ".arch_extension lse", "ldar x0", "stlr x4",
		"ldaddal", "casal", ".cfi_startproc",
	} {
		if !strings.Contains(source, token) {
			t.Errorf("atomic helper lacks %q", token)
		}
	}
}
