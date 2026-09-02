package elf

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	vmruntime "github.com/vmpacker/internal/runtime"
	"github.com/vmpacker/internal/vm"
)

func TestPrepareTranslationsAggregatesRuntimeRequirementsAndFunctionFacts(t *testing.T) {
	// Given: two selected functions whose translated instructions require the
	// same SVC and FP/SIMD thunk plus one exclusive-region thunk.
	const entry = 0x1200
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{
		0xD4024681,
		0x1E212000,
		0xD4000001,
		0xD65F03C0,
		0xC85FFC20,
		0x91000400,
		0xC802FC20,
		0x1E212000,
		0xD4024681,
		0xD65F03C0,
	}})
	request := Request{
		Input: fixture.data,
		Selections: []SelectionRequest{
			addressSelection(entry, entry+16),
			addressSelection(entry+16, entry+40),
		},
		Opcodes: vm.IdentityOpcodeMap(),
	}
	analysis, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}

	// When: the analyzed selections are prepared before runtime construction.
	preparation, err := PrepareTranslations(request, analysis)
	if err != nil {
		t.Fatal(err)
	}

	// Then: per-function translation state is retained and runtime requirements
	// are deduplicated into deterministic sets.
	if len(preparation.Functions) != 2 {
		t.Fatalf("functions=%d, want 2", len(preparation.Functions))
	}
	if got := preparation.SVCImmediates; len(got) != 2 || got[0] != 0 || got[1] != 0x1234 {
		t.Fatalf("SVC immediates=%v", got)
	}
	if got := preparation.FPSIMDInstructions; len(got) != 1 || got[0] != 0x1E212000 {
		t.Fatalf("FP/SIMD instructions=%#x", got)
	}
	if got := preparation.ExclusiveRegions; len(got) != 1 || !got[0].Valid() {
		t.Fatalf("exclusive regions=%#v", got)
	}
	facts := preparation.FunctionFacts()
	if len(facts) != 2 {
		t.Fatalf("facts=%d, want 2", len(facts))
	}
	for i, fact := range facts {
		translation := preparation.Functions[i].Translation
		if fact.Instructions != translation.TotalInsts || fact.Translated != translation.TransInsts || fact.Bytecode != len(translation.Bytecode) {
			t.Fatalf("fact[%d]=%+v translation=%+v", i, fact, translation)
		}
	}
}

func TestPrepareTranslationsRejectsUnsupportedInstruction(t *testing.T) {
	// Given: an explicitly selected range containing a decoded-but-rejected HLT.
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0xD4400000, 0xD503201F, 0xD65F03C0}})
	request := Request{
		Input: fixture.data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)},
		Opcodes: vm.IdentityOpcodeMap(),
	}
	analysis, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}

	// When: preparation translates the selected function.
	preparation, err := PrepareTranslations(request, analysis)

	// Then: the unsupported instruction fails closed instead of producing a
	// partial preparation that could be passed to the writer.
	if err == nil || !strings.Contains(err.Error(), "unsupported") || preparation != nil {
		t.Fatalf("preparation=%+v err=%v", preparation, err)
	}
}

func TestProcessRejectsRuntimeRequirementsMismatchBeforePlannerBoundary(t *testing.T) {
	// Given: a translatable function that requires one SVC thunk and a runtime
	// image built for the same opcode map but without that thunk.
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{0xD4000021, 0xD503201F, 0xD65F03C0}})
	input := append([]byte(nil), fixture.data...)
	before := append([]byte(nil), input...)
	opcodes := vm.IdentityOpcodeMap()
	digest, err := opcodes.Digest()
	if err != nil {
		t.Fatal(err)
	}

	// When: processing reaches the runtime validation boundary.
	result, err := Process(Request{
		Input: input, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}, Opcodes: opcodes,
		RuntimeImage: &vmruntime.Image{OpcodeMapDigest: hex.EncodeToString(digest[:])},
	})

	// Then: the exact runtime/preparation mismatch is rejected before the
	// rewrite planner and the original ELF remains untouched.
	if err == nil || !strings.Contains(err.Error(), "runtime image") || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("runtime requirement mismatch mutated input")
	}
}

func TestProcessRejectsPreparationForDifferentAnalysis(t *testing.T) {
	// Given: a valid preparation for one selection and a different analysis
	// over the same immutable input.
	fixture := buildELFFixture(fixtureOptions{dynamic: true, code: []uint32{
		0xD503201F, 0xD503201F, 0xD65F03C0,
		0xD503201F, 0xD503201F, 0xD65F03C0,
	}})
	opcodes := vm.IdentityOpcodeMap()
	firstRequest := Request{
		Input: fixture.data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}, Opcodes: opcodes,
	}
	firstAnalysis, err := Analyze(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := PrepareTranslations(firstRequest, firstAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := Request{
		Input: fixture.data, Selections: []SelectionRequest{addressSelection(0x120c, 0x1218)}, Opcodes: opcodes,
	}
	secondAnalysis, err := Analyze(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := opcodes.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondRequest.Preparation = preparation
	secondRequest.RuntimeImage = &vmruntime.Image{OpcodeMapDigest: hex.EncodeToString(digest[:])}

	// When: the prepared translation is supplied to the other analyzed range.
	result, err := ProcessAnalyzed(secondRequest, secondAnalysis)

	// Then: provenance mismatch fails before the rewrite planner can consume it.
	if err == nil || !strings.Contains(err.Error(), "preparation") || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProcessRejectsPreparationForDifferentOpcodeMap(t *testing.T) {
	fixture := buildELFFixture(fixtureOptions{dynamic: true})
	identity := vm.IdentityOpcodeMap()
	request := Request{
		Input: fixture.data, Selections: []SelectionRequest{addressSelection(0x1200, 0x120c)}, Opcodes: identity,
	}
	analysis, err := Analyze(request)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := PrepareTranslations(request, analysis)
	if err != nil {
		t.Fatal(err)
	}
	other, err := vm.NewOpcodeMap(bytes.NewReader(make([]byte, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	request.Opcodes = other
	request.Preparation = preparation
	request.RuntimeImage = rewritePlanRuntimeImage(t, other)

	result, err := ProcessAnalyzed(request, analysis)
	if err == nil || !strings.Contains(err.Error(), "opcode-map provenance") || len(result.Artifact) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
