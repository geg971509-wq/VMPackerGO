package arm64

import "fmt"

type fpSIMDRule struct {
	name       string
	mask       uint32
	value      uint32
	gprSource  bool
	gprDest    bool
	memoryBase bool
	writesNZCV bool
}

type FPSIMDThunkPlan struct {
	Instruction uint32
	LoadReg     int
	StoreReg    int
	StackOffset uint64
	StackSize   uint64
}

// fpSIMDRules is the executable whitelist derived from the exact NDK r29
// compiler corpus at -O0/-O2/-Oz. Register fields are variable; operation,
// shape, element width, and addressing form remain fixed.
var fpSIMDRules = []fpSIMDRule{
	{"FMOV S", 0xfffffc00, 0x1e204000, false, false, false, false},
	{"FABS S", 0xfffffc00, 0x1e20c000, false, false, false, false},
	{"FNEG S", 0xfffffc00, 0x1e214000, false, false, false, false},
	{"FABS D", 0xfffffc00, 0x1e60c000, false, false, false, false},
	{"FNEG D", 0xfffffc00, 0x1e614000, false, false, false, false},
	{"FCVT D,S", 0xfffffc00, 0x1e22c000, false, false, false, false},
	{"FCVT S,D", 0xfffffc00, 0x1e624000, false, false, false, false},
	{"FCVTZS W,S", 0xfffffc00, 0x1e380000, false, true, false, false},
	{"SCVTF S,W", 0xfffffc00, 0x1e220000, true, false, false, false},
	{"UCVTF D,W", 0xfffffc00, 0x1e630000, true, false, false, false},
	{"FMUL S", 0xffe0fc00, 0x1e200800, false, false, false, false},
	{"FDIV S", 0xffe0fc00, 0x1e201800, false, false, false, false},
	{"FADD S", 0xffe0fc00, 0x1e202800, false, false, false, false},
	{"FSUB S", 0xffe0fc00, 0x1e203800, false, false, false, false},
	{"FMUL D", 0xffe0fc00, 0x1e600800, false, false, false, false},
	{"FDIV D", 0xffe0fc00, 0x1e601800, false, false, false, false},
	{"FADD D", 0xffe0fc00, 0x1e602800, false, false, false, false},
	{"FSUB D", 0xffe0fc00, 0x1e603800, false, false, false, false},
	{"FCMP S", 0xffe0fc1f, 0x1e202000, false, false, false, true},
	{"FCMP D", 0xffe0fc1f, 0x1e602000, false, false, false, true},
	{"AND V.16B", 0xffe0fc00, 0x4e201c00, false, false, false, false},
	{"ORR V.16B", 0xffe0fc00, 0x4ea01c00, false, false, false, false},
	{"EOR V.16B", 0xffe0fc00, 0x6e201c00, false, false, false, false},
	{"MVN V.16B", 0xfffffc00, 0x6e205800, false, false, false, false},
	{"MOVI V.16B", 0xfff8fc00, 0x4f00e400, false, false, false, false},
	{"MOVI V.2D", 0xfff8fc00, 0x6f00e400, false, false, false, false},
	{"FADD V.4S", 0xffe0fc00, 0x4e20d400, false, false, false, false},
	{"FADD V.2D", 0xffe0fc00, 0x4e60d400, false, false, false, false},
	{"FSUB V.4S", 0xffe0fc00, 0x4ea0d400, false, false, false, false},
	{"FMUL V.4S", 0xffe0fc00, 0x6e20dc00, false, false, false, false},
	{"FMUL V.2D", 0xffe0fc00, 0x6e60dc00, false, false, false, false},
	{"SCVTF S,S", 0xfffffc00, 0x5e21d800, false, false, false, false},
	{"UCVTF D,D", 0xfffffc00, 0x7e61d800, false, false, false, false},
	{"LDR S", 0xffc00000, 0xbd400000, false, false, true, false},
	{"STR S", 0xffc00000, 0xbd000000, false, false, true, false},
	{"LDR D", 0xffc00000, 0xfd400000, false, false, true, false},
	{"STR D", 0xffc00000, 0xfd000000, false, false, true, false},
	{"LDR Q", 0xffc00000, 0x3dc00000, false, false, true, false},
	{"STR Q", 0xffc00000, 0x3d800000, false, false, true, false},
}

func matchFPSIMD(raw uint32) (fpSIMDRule, bool) {
	for _, rule := range fpSIMDRules {
		if raw&rule.mask == rule.value {
			return rule, true
		}
	}
	return fpSIMDRule{}, false
}

func ValidateFPSIMDInstruction(raw uint32) error {
	_, err := PlanFPSIMDThunk(raw, 9)
	return err
}

func PlanFPSIMDThunk(raw uint32, scratch uint8) (FPSIMDThunkPlan, error) {
	plan := FPSIMDThunkPlan{Instruction: raw, LoadReg: -1, StoreReg: -1}
	if scratch > 15 {
		return plan, fmt.Errorf("FP/SIMD thunk scratch X%d is not caller-saved scratch X0-X15", scratch)
	}
	rule, ok := matchFPSIMD(raw)
	if !ok {
		return plan, fmt.Errorf("FP/SIMD encoding 0x%08x is outside the exact-r29 whitelist", raw)
	}
	roles := 0
	if rule.gprSource {
		roles++
		rn := int((raw >> 5) & 31)
		if rn != 31 {
			plan.LoadReg = rn
			plan.Instruction = (plan.Instruction &^ (31 << 5)) | uint32(scratch)<<5
		}
	}
	if rule.gprDest {
		roles++
		rd := int(raw & 31)
		if rd != 31 {
			plan.StoreReg = rd
			plan.Instruction = (plan.Instruction &^ 31) | uint32(scratch)
		}
	}
	if rule.memoryBase {
		roles++
		rn := int((raw >> 5) & 31)
		plan.LoadReg = rn
		plan.Instruction = (plan.Instruction &^ (31 << 5)) | uint32(scratch)<<5
		if rn == 31 {
			offset, size, ok := fpSIMDMemoryAccess(raw)
			if !ok {
				return plan, fmt.Errorf("%s SP addressing form is not proven for thunk remapping", rule.name)
			}
			plan.StackOffset = offset
			plan.StackSize = size
		}
	}
	if roles > 1 {
		return plan, fmt.Errorf("%s uses multiple GPR operand roles that cannot share one thunk scratch", rule.name)
	}
	return plan, nil
}

func fpSIMDMemoryAccess(raw uint32) (offset, size uint64, ok bool) {
	switch raw & 0xffc00000 {
	case 0xbd000000, 0xbd400000:
		size = 4
	case 0xfd000000, 0xfd400000:
		size = 8
	case 0x3d800000, 0x3dc00000:
		size = 16
	default:
		return 0, 0, false
	}
	return uint64((raw>>10)&0xfff) * size, size, true
}

func FPSIMDWritesNZCV(raw uint32) bool {
	rule, ok := matchFPSIMD(raw)
	return ok && rule.writesNZCV
}
