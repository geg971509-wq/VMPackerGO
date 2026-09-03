package runtime

import (
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestGenerateBranchfulExclusiveThunkTargetsCleanupAndClearsMonitor(t *testing.T) {
	region := vm.NewExclusiveRegion([]uint32{
		0xc85ffc0c, // ldaxr x12, [x0]
		0xeb0b019f, // cmp x12, x11
		0x54000081, // b.ne -> raw CLREX path
		0xc80efc0d, // stlxr w14, x13, [x0]
		0x35ffff8e, // cbnz w14, retry load
		0x14000002, // b -> one-past-region cleanup
		0xd5033f5f, // clrex
	})
	_, assembly, normalized, err := generateExclusiveThunks([]vm.ExclusiveRegion{region})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 1 || normalized[0].ID != region.ID {
		t.Fatalf("normalized=%+v", normalized)
	}
	text := string(assembly)
	for _, token := range []string{
		".inst 0x54000081",
		".inst 0x35ffff84", // guest status X14 remapped to host X4
		".inst 0x14000002",
		".inst 0xd5033f5f\n  clrex\n  mrs x17, nzcv",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("generated assembly lacks %q\n%s", token, text)
		}
	}
	if strings.Count(text, "\n  clrex\n") != 1 {
		t.Fatalf("cleanup CLREX count=%d", strings.Count(text, "\n  clrex\n"))
	}
}
