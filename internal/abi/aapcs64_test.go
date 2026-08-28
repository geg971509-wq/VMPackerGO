package abi

import "testing"

func TestParse(t *testing.T) {
	sig, err := Parse("i32(ptr,u64)")
	if err != nil {
		t.Fatal(err)
	}
	if sig.String() != "i32(ptr,u64)" {
		t.Fatalf("got %s", sig.String())
	}
	void, err := Parse("void()")
	if err != nil || !void.Void || len(void.Params) != 0 {
		t.Fatalf("void parse: %#v %v", void, err)
	}
}

func TestParseRejectsUnsupportedAndTooManyParams(t *testing.T) {
	for _, text := range []string{"f32(i32)", "i32(f32)", "i32(i8,i8,i8,i8,i8,i8,i8,i8,i8)", "void(void)"} {
		if _, err := Parse(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestFromPartsRequiresOneExactTokenPerElement(t *testing.T) {
	for _, parts := range []struct {
		params []string
		result string
	}{
		{[]string{"i32,u32"}, "void"},
		{[]string{" i32"}, "void"},
		{[]string{"i32 u32"}, "void"},
		{[]string{""}, "void"},
		{[]string{"i32"}, " void"},
		{[]string{"i32"}, "unknown"},
	} {
		if _, err := FromParts(parts.params, parts.result); err == nil {
			t.Fatalf("accepted params=%q result=%q", parts.params, parts.result)
		}
	}
	if sig, err := FromParts([]string{"i32", "ptr"}, "u64"); err != nil || sig.String() != "u64(i32,ptr)" {
		t.Fatalf("sig=%v err=%v", sig, err)
	}
}
