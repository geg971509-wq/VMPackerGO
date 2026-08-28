package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

// ============================================================
// ALU — 仅保留无法用栈模式实现的特殊格式指令
// ============================================================

// trCCMP 翻译 CCMP/CCMN (reg/imm)
// 字节码: [op][cond][nzcv][rn][rm_or_imm5][sf] = 6B
// inst.Cond = condition, inst.WB = nzcv (default flags)
// isNeg: true=CCMN, false=CCMP
// isImm: true=imm5 variant (inst.Rm reused as imm5), false=reg variant
func (t *Translator) trCCMP(inst vm.Instruction, isNeg bool, isImm bool) error {
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}

	var vmOp vm.Opcode
	if isNeg {
		if isImm {
			vmOp = vm.OpCcmnImm
		} else {
			vmOp = vm.OpCcmnReg
		}
	} else {
		if isImm {
			vmOp = vm.OpCcmpImm
		} else {
			vmOp = vm.OpCcmpReg
		}
	}

	var rmOrImm byte
	if isImm {
		rmOrImm = byte(inst.Rm) // Rm field reused as imm5
	} else {
		rm, err := t.mapReg(inst.Rm)
		if err != nil {
			return err
		}
		rmOrImm = rm
	}

	var sf byte
	if inst.SF {
		sf = 1
	}

	t.emitOp(vmOp, byte(inst.Cond), byte(inst.WB), rn, rmOrImm, sf)
	return nil
}

// trMRS 翻译 MRS Xd, <sysreg> — 读取系统寄存器
// 格式: [OpMrs][d][sysreg_lo][sysreg_hi] = 4B
// sysreg 是 15-bit 编码，存为 uint16 LE
func (t *Translator) trMRS(inst vm.Instruction) error {
	switch inst.Imm {
	case 0x5F02, // CNTVCT_EL0
		0x5F00, // CNTFRQ_EL0
		0x5E82, // TPIDR_EL0
		0x5E83, // TPIDRRO_EL0
		0x5A10: // NZCV
	default:
		return fmt.Errorf("unsupported system register encoding 0x%X", inst.Imm)
	}
	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}
	sysreg := uint16(inst.Imm & 0x7FFF)
	t.emitOp(vm.OpMrs, rd, byte(sysreg&0xFF), byte(sysreg>>8))
	return nil
}
