package arm64

import (
	"fmt"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

// ============================================================
// 位域翻译 — SBFM (安全，无 temp 寄存器冲突)
// UBFM 已迁移到 tr_stack.go (trStackUBFM)
// ============================================================

func (t *Translator) trSBFM(inst vm.Instruction) error {
	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	immr := uint32(inst.Imm)
	imms := uint32(inst.Shift)

	regSize := uint32(32)
	if inst.SF {
		regSize = 64
	}

	if imms == regSize-1 {
		// ASR. Keep the source on the stack so XZR is treated as zero rather
		// than being mapped onto a real VM register.
		t.pushRegOrZero(inst.Rn, rn)
		if !inst.SF {
			t.emitOp(vm.OpSSext32)
		}
		t.sPushImm32(immr)
		t.emitOp(vm.OpSAsr)
		if !inst.SF {
			t.emitOp(vm.OpSTrunc32)
		}
		t.storeRegOrDrop(inst.Rd, rd)
		return nil
	}
	if immr == 0 {
		// SXTB/SXTH/SXTW: 符号扩展
		// VM寄存器是64-bit，所以需要用64-bit的shift宽度来做sign extension
		var shiftAmt uint32
		if inst.SF {
			shiftAmt = 64 - (imms + 1)
		} else {
			// 32-bit: 先SHL到bit63位置，再ASR回来，最后trunc32
			shiftAmt = 64 - (imms + 1)
		}
		t.pushRegOrZero(inst.Rn, rn)
		t.sPushImm32(shiftAmt)
		t.emitOp(vm.OpSShl)
		t.sPushImm32(shiftAmt)
		t.emitOp(vm.OpSAsr)
		if !inst.SF {
			t.emitOp(vm.OpSTrunc32)
		}
		t.storeRegOrDrop(inst.Rd, rd)
		return nil
	}
	return fmt.Errorf("complex SBFM is unsupported (immr=%d, imms=%d)", immr, imms)
}
