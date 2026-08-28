package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/vm"
)

func generateExclusiveThunks(regions []vm.ExclusiveRegion) (header, assembly []byte, normalized []vm.ExclusiveRegion, err error) {
	byID := make(map[uint32]vm.ExclusiveRegion, len(regions))
	for _, region := range regions {
		if err := arm64.ValidateExclusiveRegion(region); err != nil {
			return nil, nil, nil, fmt.Errorf("validate exclusive region 0x%08x: %w", region.ID, err)
		}
		if previous, ok := byID[region.ID]; ok {
			if !equalRegionWords(previous.Instructions, region.Instructions) {
				return nil, nil, nil, fmt.Errorf("exclusive region identifier collision 0x%08x", region.ID)
			}
			continue
		}
		byID[region.ID] = region
	}
	for _, region := range byID {
		normalized = append(normalized, region)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].ID < normalized[j].ID })

	var h strings.Builder
	h.WriteString("#ifndef VMPACKER_VM_EXCLUSIVE_H\n#define VMPACKER_VM_EXCLUSIVE_H\n\n")
	for _, region := range normalized {
		fmt.Fprintf(&h, "void vm_exclusive_%08x(vm_ctx_t *vm);\n", region.ID)
	}
	h.WriteString("\nstatic inline u32 h_exclusive(vm_ctx_t *vm) {\n")
	h.WriteString("  u32 id = rd32(&vm->bc[vm->pc + 1]);\n  switch (id) {\n")
	for _, region := range normalized {
		fmt.Fprintf(&h, "  case 0x%08xu: vm_exclusive_%08x(vm); break;\n", region.ID, region.ID)
	}
	h.WriteString("  default: vm->fault |= VM_FAULT_SYSTEM; break;\n  }\n  return 5;\n}\n\n#endif\n")

	var s strings.Builder
	s.WriteString(".text\n.p2align 2\n")
	for _, region := range normalized {
		fmt.Fprintf(&s, ".global vm_exclusive_%08x\n.hidden vm_exclusive_%08x\n.type vm_exclusive_%08x, %%function\n", region.ID, region.ID, region.ID)
		fmt.Fprintf(&s, "vm_exclusive_%08x:\n", region.ID)
		s.WriteString("  .cfi_startproc\n  bti c\n  mov x16, x0\n")
		for reg := 0; reg < 16; reg += 2 {
			fmt.Fprintf(&s, "  ldp x%d, x%d, [x16, #%d]\n", reg, reg+1, reg*8)
		}
		for _, raw := range region.Instructions {
			fmt.Fprintf(&s, "  .inst 0x%08x\n", raw)
		}
		for reg := 0; reg < 16; reg += 2 {
			fmt.Fprintf(&s, "  stp x%d, x%d, [x16, #%d]\n", reg, reg+1, reg*8)
		}
		s.WriteString("  ret\n  .cfi_endproc\n")
		fmt.Fprintf(&s, ".size vm_exclusive_%08x, .-vm_exclusive_%08x\n\n", region.ID, region.ID)
	}
	s.WriteString(".section .note.gnu.property,\"a\",%note\n")
	s.WriteString(".p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n")
	s.WriteString(".p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n")
	s.WriteString(".section .note.GNU-stack,\"\",%progbits\n")
	return []byte(h.String()), []byte(s.String()), normalized, nil
}

func equalRegionWords(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
