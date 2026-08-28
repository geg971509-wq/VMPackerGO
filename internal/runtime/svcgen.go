package runtime

import (
	"fmt"
	"sort"
	"strings"
)

func generateSVCThunks(immediates []uint16) (header, assembly []byte, normalized []uint16) {
	seen := make(map[uint16]bool, len(immediates))
	for _, immediate := range immediates {
		seen[immediate] = true
	}
	normalized = make([]uint16, 0, len(seen))
	for immediate := range seen {
		normalized = append(normalized, immediate)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })

	var h strings.Builder
	h.WriteString("#ifndef VMPACKER_VM_SVC_H\n#define VMPACKER_VM_SVC_H\n\n")
	for _, immediate := range normalized {
		fmt.Fprintf(&h, "void vm_svc_%04x(vm_ctx_t *vm);\n", immediate)
	}
	h.WriteString("\nstatic inline u32 h_svc(vm_ctx_t *vm) {\n")
	h.WriteString("  u16 immediate = rd16(&vm->bc[vm->pc + 1]);\n  switch (immediate) {\n")
	for _, immediate := range normalized {
		fmt.Fprintf(&h, "  case 0x%04x: vm_svc_%04x(vm); break;\n", immediate, immediate)
	}
	h.WriteString("  default: vm->fault |= VM_FAULT_SYSTEM; break;\n  }\n  return 3;\n}\n\n#endif\n")

	var s strings.Builder
	s.WriteString(".text\n.p2align 2\n")
	for _, immediate := range normalized {
		fmt.Fprintf(&s, ".global vm_svc_%04x\n.hidden vm_svc_%04x\n.type vm_svc_%04x, %%function\n", immediate, immediate, immediate)
		fmt.Fprintf(&s, "vm_svc_%04x:\n", immediate)
		s.WriteString("  .cfi_startproc\n  bti c\n  mov x9, x0\n")
		s.WriteString("  ldp x0, x1, [x9, #0]\n  ldp x2, x3, [x9, #16]\n")
		s.WriteString("  ldp x4, x5, [x9, #32]\n  ldr x8, [x9, #64]\n")
		fmt.Fprintf(&s, "  svc #0x%04x\n", immediate)
		s.WriteString("  str x0, [x9, #0]\n  ret\n  .cfi_endproc\n")
		fmt.Fprintf(&s, ".size vm_svc_%04x, .-vm_svc_%04x\n\n", immediate, immediate)
	}
	// GNU property merging is an intersection: every object containing executable
	// AArch64 code must opt in or ld.lld drops BTI/PAC from the linked ET_REL.
	s.WriteString(".section .note.gnu.property,\"a\",%note\n")
	s.WriteString(".p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n")
	s.WriteString(".p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n")
	s.WriteString(".section .note.GNU-stack,\"\",%progbits\n")
	return []byte(h.String()), []byte(s.String()), normalized
}
