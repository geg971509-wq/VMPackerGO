package report

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/abi"
	elfpacker "github.com/geg971509-wq/VMPackerGO/internal/elf"
)

func TestSuccessGolden(t *testing.T) {
	sig, _ := abi.Parse("i32(ptr)")
	r := New("v1", "abc", `raw/../input.so`, `out.so`, "so", []Selection{{Source: "direct", Selector: "foo", Name: "foo", ABI: sig}})
	artifact := []byte("artifact")
	r.Success(elfpacker.Result{
		Artifact: artifact, TargetKind: elfpacker.TargetKindAndroidSO,
		DevelopmentStrategy: "rewrite-artifact-ready", RuntimeStrategy: "ndk-r29-et-rel-validated",
		OpcodeMapDigest: strings.Repeat("a", 64),
		Functions:       []elfpacker.FunctionFact{{Name: "foo", Address: 16, Size: 8, Section: ".text", Instructions: 2, Translated: 2, Bytecode: 7}},
	})
	data, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	wantHash := hex.EncodeToString(sum[:])
	text := string(data)
	for _, want := range []string{`"schema_version": 1`, `"input": "raw/../input.so"`, `"functions": [`, `"status": "ok"`,
		`"development_strategy": "rewrite-artifact-ready"`, `"opcode_map_digest": "` + strings.Repeat("a", 64) + `"`,
		`"runtime_strategy": "ndk-r29-et-rel-validated"`, wantHash, `"release_ready": false`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, secret := range []string{"seed", "/Users/", "/private/ndk-root", "target_os", "injector_requested"} {
		if strings.Contains(strings.ToLower(text), secret) {
			t.Fatalf("report contains prohibited %q: %s", secret, text)
		}
	}
}

func TestDirectAddressPreservesRawSelectorAndNormalizesFields(t *testing.T) {
	sig, _ := abi.Parse("void()")
	r := New("dev", "unknown", "in", "out", "auto", []Selection{{
		Source: "direct", Selector: " 0X10:entry ", Name: "entry", Address: "0x10", ABI: sig,
	}})
	if len(r.Functions) != 1 || r.Functions[0].Selector != " 0X10:entry " || r.Functions[0].Address != "0x10" || r.Functions[0].Range != "" || r.Functions[0].Name != "entry" {
		t.Fatalf("unexpected address fields: %#v", r.Functions)
	}
	r.Success(elfpacker.Result{Artifact: []byte("x"), Functions: []elfpacker.FunctionFact{{Source: "direct", Name: "resolved", Address: 0x10, End: 0x1c, Size: 12, SymbolSource: "dynsym"}}})
	if r.Functions[0].Name != "resolved" || r.Functions[0].Source != "direct" || r.Functions[0].Range != "0x10-0x1c" || r.Functions[0].SymbolSource != "dynsym" {
		t.Fatalf("address fact did not match normalized analysis facts: %#v", r.Functions[0])
	}
}

func TestSuccessMatchesAddressZeroFact(t *testing.T) {
	sig, _ := abi.Parse("void()")
	r := New("dev", "unknown", "in", "out", "auto", []Selection{{
		Source: "direct", Selector: "0x0", Name: "zero", Address: "0x0", ABI: sig,
	}})
	r.Success(elfpacker.Result{Artifact: []byte("x"), Functions: []elfpacker.FunctionFact{{
		Source: "direct", Name: "resolved-zero", Address: 0, End: 12, Size: 12, Section: ".text",
	}}})
	if r.Functions[0].Name != "resolved-zero" || r.Functions[0].Range != "0x0-0xc" || r.Functions[0].Section != ".text" {
		t.Fatalf("address-zero fact did not match: %#v", r.Functions[0])
	}
}

func TestFailureGoldenAndNonNullArrays(t *testing.T) {
	r := New("dev", "unknown", "in", "out", "auto", nil)
	r.Fail(errors.New("bad ELF"), elfpacker.Result{})
	data, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"functions": []`, `"limitations": [`, `"status": "failed"`, `"error": "bad ELF"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "output_sha256") {
		t.Fatalf("failure has output hash: %s", text)
	}
}

func TestFailurePreservesCompletedFunctionFacts(t *testing.T) {
	sig, _ := abi.Parse("void()")
	r := New("dev", "unknown", "in", "out", "auto", []Selection{{
		Source: "direct", Selector: "0x10", Name: "entry", Address: "0x10", ABI: sig,
	}})
	r.Fail(errors.New("writer failed"), elfpacker.Result{
		TargetKind:          elfpacker.TargetKindAndroidSO,
		DevelopmentStrategy: "rewrite-plan-ready",
		RuntimeStrategy:     "ndk-r29-et-rel-validated",
		Functions: []elfpacker.FunctionFact{{
			Source: "direct", Name: "resolved", Address: 0x10, End: 0x1c, Size: 12, Section: ".text",
			SymbolSource: "dynsym", Instructions: 3, Translated: 3, Bytecode: 19,
		}},
	})
	if r.Status != "failed" || r.DevelopmentStrategy != "rewrite-plan-ready" || r.RuntimeStrategy != "ndk-r29-et-rel-validated" {
		t.Fatalf("unexpected failure metadata: %#v", r)
	}
	if len(r.Functions) != 1 || r.Functions[0].Name != "resolved" || r.Functions[0].Range != "0x10-0x1c" ||
		r.Functions[0].Instructions != 3 || r.Functions[0].Translated != 3 || r.Functions[0].Bytecode != 19 {
		t.Fatalf("failure dropped completed function facts: %#v", r.Functions)
	}
}
