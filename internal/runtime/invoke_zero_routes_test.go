package runtime

import (
	"strings"
	"testing"
)

func TestExceptionInvokeGeneratorOmitsZeroRouteRuntimeSymbols(t *testing.T) {
	header, assembly, invokes, err := generateExceptionInvokeThunks(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(invokes) != 0 {
		t.Fatalf("invokes=%+v", invokes)
	}
	headerText := string(header)
	if !strings.Contains(headerText, "return VM_INVOKE_NONE;") {
		t.Fatal("zero-route header does not compile to a no-op invoke lookup")
	}
	for _, forbidden := range []string{"extern const vm_invoke_route_t", "extern const u64 vm_invoke_route_count"} {
		if strings.Contains(headerText, forbidden) {
			t.Fatalf("zero-route header retains runtime symbol %q", forbidden)
		}
	}
	if strings.Contains(string(assembly), "vm_invoke_routes") || strings.Contains(string(assembly), "vm_invoke_route_count") {
		t.Fatal("zero-route assembly emits empty route symbols")
	}
	if err := validateExceptionInvokeImage(&Image{}, nil); err != nil {
		t.Fatalf("zero-route image validation: %v", err)
	}
}
