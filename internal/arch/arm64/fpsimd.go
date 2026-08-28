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

// ValidateFPSIMDInstruction proves that raw belongs to the exact-r29-derived
// whitelist and does not use the thunk's reserved GPRs X16-X18.
func ValidateFPSIMDInstruction(raw uint32) error {
	rule, ok := matchFPSIMD(raw)
	if !ok {
		return fmt.Errorf("FP/SIMD encoding 0x%08x is outside the exact-r29 whitelist", raw)
	}
	if rule.gprSource {
		if rn := int((raw >> 5) & 31); rn > 15 {
			return fmt.Errorf("%s source X%d is outside thunk bank X0-X15", rule.name, rn)
		}
	}
	if rule.gprDest {
		if rd := int(raw & 31); rd > 15 {
			return fmt.Errorf("%s destination X%d is outside thunk bank X0-X15", rule.name, rd)
		}
	}
	if rule.memoryBase {
		rn := int((raw >> 5) & 31)
		if rn > 15 && rn != 31 {
			return fmt.Errorf("%s base X%d is outside X0-X15/SP", rule.name, rn)
		}
	}
	return nil
}

func FPSIMDWritesNZCV(raw uint32) bool {
	rule, ok := matchFPSIMD(raw)
	return ok && rule.writesNZCV
}
