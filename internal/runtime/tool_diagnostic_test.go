package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestBuildCompilerErrorPreservesRedactedDiagnostic(t *testing.T) {
	root, capture, fixture := fakeNDK(t)
	t.Setenv("VMPACKER_TEST_CAPTURE", capture)
	t.Setenv("VMPACKER_TEST_FIXTURE", fixture)
	t.Setenv("VMPACKER_TEST_FAIL", "1")

	_, err := Build(context.Background(), BuildConfig{NDKDir: root, Opcodes: vm.IdentityOpcodeMap()})
	if err == nil {
		t.Fatal("compiler failure was accepted")
	}
	text := err.Error()
	if !strings.Contains(text, "compile runtime C failed:") {
		t.Fatalf("compiler stage missing from diagnostic: %q", text)
	}
	if !strings.Contains(text, "private path: <path>") {
		t.Fatalf("compiler reason was not preserved and redacted: %q", text)
	}
	for _, private := range []string{root, capture, fixture} {
		if strings.Contains(text, private) {
			t.Fatalf("diagnostic leaked private path %q: %q", private, text)
		}
	}
}
