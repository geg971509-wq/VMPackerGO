package elf

import (
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

func outlinedFixture(helperName string, callerCode []uint32, helperCode []uint32, callerSize uint64, helperSize uint64) elfFixture {
	code := append(append([]uint32(nil), callerCode...), helperCode...)
	const callerAddr = uint64(0x1200)
	helperAddr := callerAddr + uint64(len(callerCode))*4
	return buildELFFixture(fixtureOptions{
		dynamic: true,
		code:    code,
		symtab: []fixtureSymbol{
			{name: "caller", addr: callerAddr, size: callerSize},
			{name: helperName, addr: helperAddr, size: helperSize},
		},
	})
}

func TestAnalyzeAndPrepareInlineValidatedOutlinedTail(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 8)
	req := Request{
		Input:      fixture.data,
		Mode:       string(AndroidModeAuto),
		Opcodes:    vm.IdentityOpcodeMap(),
		Selections: []SelectionRequest{{Name: "caller"}},
	}
	analysis, err := Analyze(req)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	preparation, err := PrepareTranslations(req, analysis)
	if err != nil {
		t.Fatalf("PrepareTranslations: %v", err)
	}
	if len(preparation.Functions) != 1 || len(preparation.Functions[0].Translation.Unsupported) != 0 {
		t.Fatalf("preparation=%+v", preparation.Functions)
	}
	mapping := preparation.Functions[0].Translation.SourceMap
	if len(mapping) == 0 || mapping[len(mapping)-1].ARM64Offset != 12 {
		t.Fatalf("source map=%+v", mapping)
	}
	for _, entry := range mapping {
		if entry.ARM64Offset > 12 {
			t.Fatalf("synthetic helper source offset leaked into map: %+v", mapping)
		}
	}
}

func TestAnalyzeRejectsGenericExternalTailBranch(t *testing.T) {
	fixture := outlinedFixture("helper",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported external unconditional branch") {
		t.Fatalf("generic external branch err=%v", err)
	}
}

func TestAnalyzeRejectsNonTailOutlinedBranch(t *testing.T) {
	// B at 0x1204 targets helper at 0x1210; caller continues for two instructions.
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0x14000003, 0xd503201f, 0xd503201f},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 16, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "not the final instruction") {
		t.Fatalf("non-tail outlined branch err=%v", err)
	}
}

func TestAnalyzeRejectsUnprovenOutlinedHelperBody(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0xd503201f, 0xd65f03c0}, 12, 8)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "not unshifted EOR") {
		t.Fatalf("unproven helper err=%v", err)
	}
}

func TestAnalyzeRejectsZeroSizedOutlinedHelper(t *testing.T) {
	fixture := outlinedFixture("OUTLINED_FUNCTION_0",
		[]uint32{0xd503201f, 0xd503201f, 0x14000001},
		[]uint32{0x4a0d0100, 0xd65f03c0}, 12, 0)
	_, err := Analyze(Request{Input: fixture.data, Mode: string(AndroidModeAuto), Selections: []SelectionRequest{{Name: "caller"}}})
	if err == nil || !strings.Contains(err.Error(), "invalid symbol size") {
		t.Fatalf("zero-sized helper err=%v", err)
	}
}
