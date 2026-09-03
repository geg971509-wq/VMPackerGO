package arm64

import "testing"

func TestExactR29CompilerGPRSIMDMovesUseExistingThunkABI(t *testing.T) {
	tests := []struct {
		name        string
		raw         uint32
		instruction uint32
		loadReg     int
		loadReg2    int
		storeReg    int
	}{
		{"FMOV D,D", 0x1e604001, 0x1e604001, -1, -1, -1},
		{"MOV D,V.D[1]", 0x5e180463, 0x5e180463, -1, -1, -1},
		{"FMOV X,D", 0x9e66002c, 0x9e660029, -1, -1, 12},
		{"FMOV D,X", 0x9e670140, 0x9e670120, 10, -1, -1},
		{"MOV V.D[0],X", 0x4e081d40, 0x4e081d20, 10, -1, -1},
		{"MOV V.D[1],X", 0x4e181d00, 0x4e181d20, 8, -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := PlanFPSIMDThunk(tt.raw, 9)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Instruction != tt.instruction || plan.LoadReg != tt.loadReg || plan.LoadReg2 != tt.loadReg2 || plan.StoreReg != tt.storeReg {
				t.Fatalf("plan=%+v", plan)
			}
			if err := ValidateFPSIMDInstruction(tt.raw); err != nil {
				t.Fatal(err)
			}
			if Op(NewDecoder().Decode(tt.raw, 0).Op) != FPSIMD_NATIVE {
				t.Fatalf("raw=%08x was not routed through FP/SIMD native thunk", tt.raw)
			}
		})
	}
}

func TestExactR29CompilerIndexedQLoadUsesTwoReadOnlyGPRRoles(t *testing.T) {
	const raw uint32 = 0x3ce97900 // ldr q0, [x8, x9, lsl #4]
	plan, err := PlanFPSIMDThunk(raw, 9)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Instruction != 0x3cea7920 || plan.LoadReg != 8 || plan.LoadReg2 != 9 || plan.StoreReg != -1 || plan.StackSize != 0 {
		t.Fatalf("plan=%+v", plan)
	}
	if err := ValidateFPSIMDInstruction(raw); err != nil {
		t.Fatal(err)
	}
	if Op(NewDecoder().Decode(raw, 0).Op) != FPSIMD_NATIVE {
		t.Fatal("indexed Q load was not routed through FP/SIMD native thunk")
	}
	if _, err := PlanFPSIMDThunk(raw, 15); err == nil {
		t.Fatal("two-role indexed Q load accepted without a second caller-saved scratch")
	}
	if _, err := PlanFPSIMDThunk(0x3ce97be0, 9); err == nil { // ldr q0, [sp, x9, lsl #4]
		t.Fatal("dynamic SP-indexed Q load was accepted without a bounded stack proof")
	}
}
