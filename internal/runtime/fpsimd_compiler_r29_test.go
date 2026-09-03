package runtime

import (
	"strings"
	"testing"
)

func TestGenerateExactR29CompilerFPSIMDThunksUsesOneAndTwoGPRRoles(t *testing.T) {
	_, assembly, got, err := generateFPSIMDThunks([]uint32{
		0x1e604001, // fmov d1, d0
		0x5e180400, // mov d0, v0.d[1]
		0x9e66002c, // fmov x12, d1
		0x9e670140, // fmov d0, x10
		0x4e081d40, // mov v0.d[0], x10
		0x4e181d00, // mov v0.d[1], x8
		0x3ce97900, // ldr q0, [x8, x9, lsl #4]
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("normalized instructions=%#v", got)
	}
	text := string(assembly)
	for _, token := range []string{
		".inst 0x1e604001",
		".inst 0x5e180400",
		".inst 0x9e660029",
		"str x9, [x16, #(VM_CTX_R + 12 * 8)]",
		".inst 0x9e670120",
		"ldr x9, [x16, #(VM_CTX_R + 10 * 8)]",
		".inst 0x4e081d20",
		".inst 0x4e181d20",
		"ldr x9, [x16, #(VM_CTX_R + 8 * 8)]",
		"ldr x10, [x16, #(VM_CTX_R + 9 * 8)]",
		".inst 0x3cea7920",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("generated assembly lacks %q", token)
		}
	}
}
