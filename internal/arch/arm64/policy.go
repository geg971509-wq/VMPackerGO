package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

type semanticDisposition uint8

const (
	dispositionVirtual semanticDisposition = iota
	dispositionNativeThunk
	dispositionReject
)

type instructionRule struct {
	disposition semanticDisposition
	validate    func(vm.Instruction) error
}

var instructionRules = buildInstructionRules()

func buildInstructionRules() map[Op]instructionRule {
	rules := make(map[Op]instructionRule)
	allow := func(ops []Op, validate func(vm.Instruction) error) {
		for _, op := range ops {
			rules[op] = instructionRule{disposition: dispositionVirtual, validate: validate}
		}
	}
	classify := func(disposition semanticDisposition, ops ...Op) {
		for _, op := range ops {
			rules[op] = instructionRule{disposition: disposition}
		}
	}

	allow([]Op{NOP, MUL, LSL_REG, LSR_REG, ASR_REG, ROR_REG, MADD, MSUB,
		SMADDL, SMSUBL, UMADDL, UMSUBL, UMULH, UDIV, SDIV, SMULH, CLZ,
		CLS, RBIT, REV, REV16, REV32, ADC, ADCS, SBC, SBCS}, nil)
	allow([]Op{ADD_IMM, SUB_IMM, ADDS_IMM, SUBS_IMM, AND_IMM, ANDS_IMM,
		ORR_IMM, EOR_IMM}, validateImmediateDataProcessing)
	allow([]Op{MOVZ, MOVK, MOVN}, validateMoveWide)
	allow([]Op{ADD_REG, SUB_REG, ADDS_REG, SUBS_REG}, validateAddSubShifted)
	allow([]Op{AND_REG, ORR_REG, EOR_REG, EON, MVN, ANDS_REG, BIC, BICS,
		ORN}, validateLogicalShifted)
	allow([]Op{UBFM, SBFM, BFM, EXTR}, validateBitfield)
	allow([]Op{ADD_EXT, SUB_EXT, ADDS_EXT, SUBS_EXT}, validateExtendedRegister)
	allow([]Op{LDR_IMM, LDRB_IMM, LDRH_IMM, LDRSB_IMM, LDRSH_IMM,
		LDRSW_IMM, STR_IMM, STRB_IMM, STRH_IMM, STP, LDP, LDPSW},
		validateImmediateAddressing)
	allow([]Op{LDR_REG, LDRB_REG, LDRH_REG, LDRSB_REG, LDRSH_REG, LDRSW_REG,
		STR_REG, STRB_REG, STRH_REG}, validateRegisterOffset)
	allow([]Op{B, B_COND, CBZ, CBNZ, TBZ, TBNZ, BL, RET, CSEL, CSINC, CSINV,
		CSNEG, CCMP_REG, CCMP_IMM, CCMN_REG, CCMN_IMM}, validateConditional)
	allow([]Op{BLR, BR}, validateNativeBranch)
	allow([]Op{LD1_16B, ST1_16B}, validateSIMDStructureTransfer)
	allow([]Op{ADR, ADRP, LDR_LIT}, nil)
	allow([]Op{MRS}, validateSystemRead)
	allow([]Op{MSR_WRITE}, validateSystemWrite)
	allow([]Op{SVC}, nil)
	allow([]Op{PACIASP, AUTIASP, PACIAZ, AUTIAZ, PACIBSP, AUTIBSP, XPACLRI}, nil)
	allow([]Op{BTI_C, BTI_J, BTI_JC, BTI}, nil)
	allow([]Op{DMB, DSB, ISB}, validateBarrier)
	// PRFM/YIELD are architectural hints. CLREX is also state-free at the VM
	// boundary because every supported exclusive monitor is contained inside a
	// single generated LDXR/LDAXR...STXR/STLXR thunk.
	allow([]Op{PRFM, YIELD_ARM, CLREX}, nil)
	allow([]Op{LDAR, STLR, LDADD, CAS, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN}, validateAtomicNative)
	allow([]Op{FPSIMD_NATIVE}, func(inst vm.Instruction) error { return ValidateFPSIMDInstruction(inst.Raw) })

	classify(dispositionNativeThunk, WFE, WFI, LDXR, LDAXR, STXR, STLXR)
	classify(dispositionReject, HLT, BRK, UNKNOWN, UNSUPPORTED)
	return rules
}

func validateAtomicNative(inst vm.Instruction) error {
	if inst.Shift != 1 && inst.Shift != 2 && inst.Shift != 4 && inst.Shift != 8 {
		return fmt.Errorf("atomic width %d is unsupported", inst.Shift)
	}
	if inst.Rn < 0 || inst.Rn > 31 {
		return fmt.Errorf("atomic address register X%d is invalid", inst.Rn)
	}
	validDataReg := func(reg int) bool {
		return reg == vm.REG_XZR || (reg >= 0 && reg <= 30)
	}
	if !validDataReg(inst.Rd) {
		return fmt.Errorf("atomic data register %d is invalid", inst.Rd)
	}
	if atomicUsesRm(Op(inst.Op)) && !validDataReg(inst.Rm) {
		return fmt.Errorf("atomic operand register %d is invalid", inst.Rm)
	}
	return nil
}

func validateBarrier(inst vm.Instruction) error {
	option := (inst.Raw >> 8) & 0xf
	if Op(inst.Op) == ISB {
		if option != 0xf {
			return fmt.Errorf("ISB option 0x%x is reserved", option)
		}
		return nil
	}
	switch option {
	case 0x1, 0x2, 0x3, 0x5, 0x6, 0x7, 0x9, 0xa, 0xb, 0xd, 0xe, 0xf:
		return nil
	default:
		return fmt.Errorf("barrier option 0x%x is reserved", option)
	}
}

func validateNativeBranch(inst vm.Instruction) error {
	if inst.Rn < 0 || inst.Rn > 30 {
		return fmt.Errorf("native branch register X%d is invalid", inst.Rn)
	}
	return nil
}

func validateSIMDStructureTransfer(inst vm.Instruction) error {
	if inst.Rd < 0 || inst.Rd > 31 || inst.Rn < 0 || inst.Rn > 31 {
		return fmt.Errorf("SIMD structure transfer has an invalid register")
	}
	if inst.Imm != 16 && inst.Imm != 32 && inst.Imm != 48 && inst.Imm != 64 {
		return fmt.Errorf("SIMD structure transfer length %d is unsupported", inst.Imm)
	}
	return nil
}

func validateInstructionPolicy(inst vm.Instruction) error {
	op := Op(inst.Op)
	rule, ok := instructionRules[op]
	if !ok {
		return fmt.Errorf("instruction %s has no product whitelist rule", OpName(op))
	}
	switch rule.disposition {
	case dispositionNativeThunk:
		return fmt.Errorf("%s requires a validated native thunk or relocation", OpName(op))
	case dispositionReject:
		return fmt.Errorf("%s is rejected by the product whitelist", OpName(op))
	case dispositionVirtual:
		if rule.validate != nil {
			return rule.validate(inst)
		}
		return nil
	default:
		return fmt.Errorf("instruction %s has invalid whitelist disposition", OpName(op))
	}
}

func registerWidth(inst vm.Instruction) int {
	if inst.SF {
		return 64
	}
	return 32
}

func validateImmediateDataProcessing(inst vm.Instruction) error {
	if inst.Imm < 0 {
		return fmt.Errorf("negative data-processing immediate")
	}
	return nil
}

func validateMoveWide(inst vm.Instruction) error {
	if inst.Shift < 0 || inst.Shift%16 != 0 || inst.Shift >= registerWidth(inst) {
		return fmt.Errorf("move-wide shift %d is invalid for %d-bit form", inst.Shift, registerWidth(inst))
	}
	return nil
}

func validateAddSubShifted(inst vm.Instruction) error {
	if inst.ShiftType < 0 || inst.ShiftType > 2 {
		return fmt.Errorf("add/sub shifted-register type %d is reserved", inst.ShiftType)
	}
	return validateShiftAmount(inst)
}

func validateLogicalShifted(inst vm.Instruction) error {
	if inst.ShiftType < 0 || inst.ShiftType > 3 {
		return fmt.Errorf("logical shifted-register type %d is invalid", inst.ShiftType)
	}
	return validateShiftAmount(inst)
}

func validateShiftAmount(inst vm.Instruction) error {
	if inst.Shift < 0 || inst.Shift >= registerWidth(inst) {
		return fmt.Errorf("shift %d is invalid for %d-bit form", inst.Shift, registerWidth(inst))
	}
	return nil
}

func validateBitfield(inst vm.Instruction) error {
	width := registerWidth(inst)
	if inst.Imm < 0 || inst.Imm >= int64(width) || inst.Shift < 0 || inst.Shift >= width {
		return fmt.Errorf("bitfield selectors %d/%d are invalid for %d-bit form", inst.Imm, inst.Shift, width)
	}
	return nil
}

func validateExtendedRegister(inst vm.Instruction) error {
	if inst.ShiftType < 0 || inst.ShiftType > 7 || inst.Shift < 0 || inst.Shift > 4 {
		return fmt.Errorf("extended-register option/shift %d/%d is invalid", inst.ShiftType, inst.Shift)
	}
	return nil
}

func validateImmediateAddressing(inst vm.Instruction) error {
	if inst.WB != 0 && inst.WB != 1 && inst.WB != 3 {
		return fmt.Errorf("address writeback mode %d is invalid", inst.WB)
	}
	if inst.WB != 0 && inst.Rn == inst.Rd {
		return fmt.Errorf("writeback base overlaps transfer register")
	}
	if (Op(inst.Op) == STP || Op(inst.Op) == LDP || Op(inst.Op) == LDPSW) &&
		inst.WB != 0 && inst.Rn == inst.Rm {
		return fmt.Errorf("pair writeback base overlaps second transfer register")
	}
	return nil
}

func validateRegisterOffset(inst vm.Instruction) error {
	option := (inst.Raw >> 13) & 0x7
	if option != 2 && option != 3 && option != 6 && option != 7 {
		return fmt.Errorf("register-offset extend option %d is reserved", option)
	}
	return nil
}

func validateConditional(inst vm.Instruction) error {
	switch Op(inst.Op) {
	case B_COND, CSEL, CSINC, CSINV, CSNEG, CCMP_REG, CCMP_IMM, CCMN_REG, CCMN_IMM:
		if inst.Cond < 0 || inst.Cond > 0xF {
			return fmt.Errorf("condition code 0x%X is invalid", inst.Cond)
		}
	}
	return nil
}

func validateSystemRead(inst vm.Instruction) error {
	if inst.Rd != vm.REG_XZR && (inst.Rd < 0 || inst.Rd > 30) {
		return fmt.Errorf("system register destination %d is invalid", inst.Rd)
	}
	switch inst.Imm {
	case 0x5F02, 0x5F00, 0x5E82, 0x5E83, 0x5A10, 0x5A20, 0x5A21:
		return nil
	default:
		return fmt.Errorf("unsupported system register encoding 0x%X", inst.Imm)
	}
}

func validateSystemWrite(inst vm.Instruction) error {
	if inst.Rd != vm.REG_XZR && (inst.Rd < 0 || inst.Rd > 30) {
		return fmt.Errorf("system register source %d is invalid", inst.Rd)
	}
	switch inst.Imm {
	case 0x5A10, 0x5A20, 0x5A21:
		return nil
	default:
		return fmt.Errorf("unsupported system register encoding 0x%X", inst.Imm)
	}
}
