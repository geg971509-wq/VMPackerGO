package runtime

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestVMDecodeSizesMatchOpcodeDefinitions(t *testing.T) {
	data, err := templates.ReadFile(templateRoot + "/vm_decode.h")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	groupRE := regexp.MustCompile(`(?ms)((?:\s*case OP_[A-Z0-9_]+:\n)+\s*return\s+([0-9]+);)`)
	seen := make(map[string]int)
	for _, match := range groupRE.FindAllStringSubmatch(source, -1) {
		size, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(match[1], "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "case OP_") {
				seen[strings.TrimSuffix(strings.TrimPrefix(line, "case "), ":")] = size
			}
		}
	}
	for op := vm.Opcode(0); op < vm.OpcodeCount; op++ {
		def, ok := vm.OpcodeDefinitionFor(op)
		if !ok {
			t.Fatalf("missing definition for opcode %d", op)
		}
		got, ok := seen[def.CMacro]
		if !ok {
			t.Errorf("vm_decode.h has no size entry for %s", def.CMacro)
			continue
		}
		if got != def.Size {
			t.Errorf("vm_decode.h size for %s=%d, opcode definition=%d", def.CMacro, got, def.Size)
		}
	}
}
