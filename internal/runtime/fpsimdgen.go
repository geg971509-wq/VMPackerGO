package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vmpacker/internal/arch/arm64"
)

func generateFPSIMDThunks(instructions []uint32) (header, assembly []byte, normalized []uint32, err error) {
	seen := make(map[uint32]bool, len(instructions))
	for _, raw := range instructions {
		if err := arm64.ValidateFPSIMDInstruction(raw); err != nil {
			return nil, nil, nil, err
		}
		seen[raw] = true
	}
	for raw := range seen {
		normalized = append(normalized, raw)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })

	var h strings.Builder
	h.WriteString("#ifndef VMPACKER_VM_FPSIMD_H\n#define VMPACKER_VM_FPSIMD_H\n\n")
	for _, raw := range normalized {
		fmt.Fprintf(&h, "void vm_fpsimd_%08x(vm_ctx_t *vm);\n", raw)
	}
	h.WriteString("\nstatic inline u32 h_fpsimd(vm_ctx_t *vm) {\n")
	h.WriteString("  u32 raw = rd32(&vm->bc[vm->pc + 1]);\n  switch (raw) {\n")
	for _, raw := range normalized {
		fmt.Fprintf(&h, "  case 0x%08xu: vm_fpsimd_%08x(vm); break;\n", raw, raw)
	}
	h.WriteString("  default: vm->fault |= VM_FAULT_SYSTEM; break;\n  }\n  return 5;\n}\n\n#endif\n")

	var s strings.Builder
	s.WriteString("#include \"vm_abi.h\"\n.text\n.p2align 2\n")
	for _, raw := range normalized {
		fmt.Fprintf(&s, ".global vm_fpsimd_%08x\n.hidden vm_fpsimd_%08x\n.type vm_fpsimd_%08x, %%function\n", raw, raw, raw)
		fmt.Fprintf(&s, "vm_fpsimd_%08x:\n", raw)
		s.WriteString("  .cfi_startproc\n  bti c\n  sub sp, sp, #144\n  .cfi_def_cfa_offset 144\n")
		s.WriteString("  str x18, [sp]\n  .cfi_offset x18, -144\n")
		for reg := 8; reg < 16; reg += 2 {
			offset := 16 + (reg-8)*16
			fmt.Fprintf(&s, "  stp q%d, q%d, [sp, #%d]\n", reg, reg+1, offset)
			fmt.Fprintf(&s, "  .cfi_offset %d, %d\n  .cfi_offset %d, %d\n", 64+reg, -128+(reg-8)*16, 65+reg, -112+(reg-8)*16)
		}
		s.WriteString("  mov x16, x0\n  ldr w17, [x16, #VM_CTX_FPCR]\n  msr fpcr, x17\n")
		s.WriteString("  ldr w17, [x16, #VM_CTX_FPSR]\n  msr fpsr, x17\n")
		for reg := 0; reg < 32; reg += 2 {
			fmt.Fprintf(&s, "  ldp q%d, q%d, [x16, #(VM_CTX_V + %d * 16)]\n", reg, reg+1, reg)
		}
		s.WriteString("  mov x17, sp\n  ldr x18, [x16, #(VM_CTX_R + 31 * 8)]\n  mov sp, x18\n")
		for reg := 0; reg < 16; reg += 2 {
			fmt.Fprintf(&s, "  ldp x%d, x%d, [x16, #(VM_CTX_R + %d * 8)]\n", reg, reg+1, reg)
		}
		fmt.Fprintf(&s, "  .inst 0x%08x\n", raw)
		s.WriteString("  mov sp, x17\n")
		for reg := 0; reg < 16; reg += 2 {
			fmt.Fprintf(&s, "  stp x%d, x%d, [x16, #(VM_CTX_R + %d * 8)]\n", reg, reg+1, reg)
		}
		for reg := 0; reg < 32; reg += 2 {
			fmt.Fprintf(&s, "  stp q%d, q%d, [x16, #(VM_CTX_V + %d * 16)]\n", reg, reg+1, reg)
		}
		s.WriteString("  mrs x17, fpcr\n  str w17, [x16, #VM_CTX_FPCR]\n")
		s.WriteString("  mrs x17, fpsr\n  str w17, [x16, #VM_CTX_FPSR]\n")
		if arm64.FPSIMDWritesNZCV(raw) {
			s.WriteString("  mrs x17, nzcv\n  lsr x17, x17, #28\n  str w17, [x16, #VM_CTX_FL]\n")
		}
		for reg := 8; reg < 16; reg += 2 {
			offset := 16 + (reg-8)*16
			fmt.Fprintf(&s, "  ldp q%d, q%d, [sp, #%d]\n  .cfi_restore %d\n  .cfi_restore %d\n", reg, reg+1, offset, 64+reg, 65+reg)
		}
		s.WriteString("  ldr x18, [sp]\n  .cfi_restore x18\n  add sp, sp, #144\n  .cfi_def_cfa_offset 0\n  ret\n  .cfi_endproc\n")
		fmt.Fprintf(&s, ".size vm_fpsimd_%08x, .-vm_fpsimd_%08x\n\n", raw, raw)
	}
	s.WriteString(".section .note.gnu.property,\"a\",%note\n")
	s.WriteString(".p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n")
	s.WriteString(".p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n")
	s.WriteString(".section .note.GNU-stack,\"\",%progbits\n")
	return []byte(h.String()), []byte(s.String()), normalized, nil
}
