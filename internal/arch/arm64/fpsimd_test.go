package arm64

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

func TestFPSIMDWhitelistCoversDerivedInstructionFamilies(t *testing.T) {
	raws := []uint32{
		0x1e204020, 0x1e20c000, 0x1e210800, 0x1e211800,
		0x1e212000, 0x1e212800, 0x1e213800, 0x1e214000,
		0x1e22c000, 0x1e380000, 0x1e60c000, 0x1e610800,
		0x1e611800, 0x1e612000, 0x1e612800, 0x1e613800,
		0x1e614000, 0x1e624000, 0x1e220000, 0x1e630000,
		0x3d800000, 0x3dc00000, 0x4e201c20, 0x4e21d400,
		0x4e61d400, 0x4ea01c20, 0x4ea1d400, 0x6e201c20,
		0x6e21dc00, 0x6e61dc00, 0x4f00e420, 0x4f02e6a0,
		0x6f00e400, 0x6f05e540,
		0x5e21d800, 0x7e61d800,
		0xbd000be1, 0xbd400fe0, 0xfd0007e0, 0xfd4003e1,
	}
	decoder := NewDecoder()
	var instructions []vm.Instruction
	for i, raw := range raws {
		if err := ValidateFPSIMDInstruction(raw); err != nil {
			t.Errorf("raw 0x%08x: %v", raw, err)
		}
		inst := decoder.Decode(raw, i*4)
		if Op(inst.Op) != FPSIMD_NATIVE {
			t.Errorf("raw 0x%08x decoded as %s", raw, OpName(Op(inst.Op)))
		}
		instructions = append(instructions, inst)
	}
	result := translateForPhase5(t, instructions)
	if len(result.Unsupported) != 0 || len(result.FPSIMDInstructions) != len(raws) {
		t.Fatalf("unsupported=%v instructions=%d", result.Unsupported, len(result.FPSIMDInstructions))
	}
}

func TestFPSIMDThunkPlanRemapsArchitecturalGPRs(t *testing.T) {
	tests := []struct {
		raw         uint32
		instruction uint32
		loadReg     int
		storeReg    int
		stackOffset uint64
		stackSize   uint64
	}{
		{0x1e220200, 0x1e220120, 16, -1, 0, 0},
		{0x1e220220, 0x1e220120, 17, -1, 0, 0},
		{0x1e220240, 0x1e220120, 18, -1, 0, 0},
		{0x1e220260, 0x1e220120, 19, -1, 0, 0},
		{0x1e2203a0, 0x1e220120, 29, -1, 0, 0},
		{0x1e38001e, 0x1e380009, -1, 30, 0, 0},
		{0x3dc003a0, 0x3dc00120, 29, -1, 0, 0},
		{0xfd0007e0, 0xfd000520, 31, -1, 8, 8},
	}
	for _, tt := range tests {
		plan, err := PlanFPSIMDThunk(tt.raw, 9)
		if err != nil {
			t.Fatalf("raw 0x%08x: %v", tt.raw, err)
		}
		if plan.Instruction != tt.instruction || plan.LoadReg != tt.loadReg || plan.StoreReg != tt.storeReg || plan.StackOffset != tt.stackOffset || plan.StackSize != tt.stackSize {
			t.Errorf("raw 0x%08x plan=%+v", tt.raw, plan)
		}
		if err := ValidateFPSIMDInstruction(tt.raw); err != nil {
			t.Errorf("raw 0x%08x validation: %v", tt.raw, err)
		}
	}
}

func TestFPSIMDExactRulesRejectReservedNeighbors(t *testing.T) {
	for _, raw := range []uint32{0x4f00e000, 0x6f00e000} {
		if err := ValidateFPSIMDInstruction(raw); err == nil {
			t.Errorf("reserved neighbor 0x%08x was accepted", raw)
		}
		if op := Op(NewDecoder().Decode(raw, 0).Op); op == FPSIMD_NATIVE {
			t.Errorf("reserved neighbor 0x%08x decoded as FP/SIMD native", raw)
		}
	}
}

func TestFPSIMDThunkPlanPreservesZeroRegisterAndRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []uint32{0x1e2203e0, 0x1e38001f} {
		plan, err := PlanFPSIMDThunk(raw, 9)
		if err != nil {
			t.Fatalf("raw 0x%08x: %v", raw, err)
		}
		if plan.Instruction != raw || plan.LoadReg != -1 || plan.StoreReg != -1 {
			t.Errorf("zero-register raw 0x%08x plan=%+v", raw, plan)
		}
	}
	for _, tc := range []struct {
		raw     uint32
		scratch uint8
	}{
		{0x00000000, 9},
		{0x1e2203a0, 16},
	} {
		if _, err := PlanFPSIMDThunk(tc.raw, tc.scratch); err == nil {
			t.Errorf("raw 0x%08x scratch X%d was accepted", tc.raw, tc.scratch)
		}
	}
}

func TestExactR29FPSIMDCorpusClosesWhitelist(t *testing.T) {
	path := os.Getenv("VMPACKER_FPSIMD_CORPUS")
	if path == "" {
		t.Skip("exact-r29 corpus listing is not configured")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	counts := map[string]int{}
	mnemonics := map[string]map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 4)
		if len(fields) != 4 || fields[0] == "optimization" {
			continue
		}
		raw, err := strconv.ParseUint(fields[1], 16, 32)
		if err != nil {
			t.Fatalf("parse corpus raw %q: %v", fields[1], err)
		}
		if err := ValidateFPSIMDInstruction(uint32(raw)); err != nil {
			t.Errorf("-%s %s %s: %v", fields[0], fields[1], fields[2], err)
		}
		counts[fields[0]]++
		if mnemonics[fields[0]] == nil {
			mnemonics[fields[0]] = map[string]bool{}
		}
		mnemonics[fields[0]][fields[2]] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	required := []string{"fadd", "fsub", "fmul", "fdiv", "fabs", "fneg", "fcmp", "fcvt", "fcvtzs", "scvtf", "ucvtf", "and", "orr", "eor", "mvn", "movi", "ldr", "str"}
	for _, optimization := range []string{"O0", "O2", "Oz"} {
		if counts[optimization] < 25 {
			t.Errorf("-%s corpus has only %d FP/SIMD records", optimization, counts[optimization])
		}
		for _, mnemonic := range required {
			if !mnemonics[optimization][mnemonic] {
				t.Errorf("-%s corpus lacks %s", optimization, mnemonic)
			}
		}
	}
}
