package runtime

import (
	"bytes"
	"testing"
)

func TestBytecodeCapacityTemplateContract(t *testing.T) {
	types, err := templates.ReadFile(templateRoot + "/vm_types.h")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(types, []byte("#define VM_BYTECODE_MAX (256u * 1024u)")) {
		t.Fatal("runtime VM_BYTECODE_MAX is not the approved 256 KiB bound")
	}

	interp, err := templates.ReadFile(templateRoot + "/vm_interp.c")
	if err != nil {
		t.Fatal(err)
	}
	call, err := templates.ReadFile(templateRoot + "/vm_call.h")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string][]byte{"vm_interp.c": interp, "vm_call.h": call} {
		if bytes.Contains(source, []byte("bc_len = VM_BYTECODE_MAX")) {
			t.Fatalf("%s still silently truncates oversized bytecode", name)
		}
		if !bytes.Contains(source, []byte("bc_len > VM_BYTECODE_MAX")) {
			t.Fatalf("%s does not reject oversized bytecode", name)
		}
	}
}
