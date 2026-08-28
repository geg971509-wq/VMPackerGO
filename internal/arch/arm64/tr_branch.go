package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

// ============================================================
// 分支翻译 — B / B.cond / BL / BLR / BR / TBZ
// CSEL/CBZ 已迁移到 tr_stack.go (trStackCSEL/trStackCBZ)
// ============================================================

func (t *Translator) trBranch(inst vm.Instruction) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		return fmt.Errorf("branch target 0x%X is outside function range [0, 0x%X)", target, t.funcSize)
	}

	t.emitOp(vm.OpJmp)
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}

func (t *Translator) trBranchCond(inst vm.Instruction) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		return fmt.Errorf("conditional branch target 0x%X is outside function range [0, 0x%X]", target, t.funcSize)
	}

	if inst.Cond < 0 || inst.Cond > 0xF {
		return fmt.Errorf("invalid condition code 0x%X", inst.Cond)
	}
	t.emitOp(vm.OpJCond, byte(inst.Cond))
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}

func (t *Translator) trBL(inst vm.Instruction) error {
	pc, err := addAddressDelta(t.funcAddr, int64(inst.Offset))
	if err != nil {
		return err
	}
	target, err := addAddressDelta(pc, inst.Imm)
	if err != nil {
		return err
	}
	t.emitImageReference(vm.OpCallImage, nil, target)
	return nil
}

func (t *Translator) trBLR(inst vm.Instruction) error {
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	t.emitOp(vm.OpCallReg, rn)
	return nil
}

func (t *Translator) trBR(inst vm.Instruction) error {
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	t.emitOp(vm.OpBrReg, rn)
	return nil
}

// trTBZ 翻译 TBZ/TBNZ — test bit and branch
// 字节码: [OpTbz/OpTbnz][reg][bit][target32] = 7B
// inst.Shift = bit number (b5:b40), inst.Imm = offset (已乘4)
func (t *Translator) trTBZ(inst vm.Instruction, isZero bool) error {
	target := inst.Offset + int(inst.Imm)

	if target < 0 || target > t.funcSize {
		return fmt.Errorf("TBZ/TBNZ target 0x%X is outside function range [0, 0x%X)", target, t.funcSize)
	}

	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return err
	}

	var vmOp vm.Opcode
	if isZero {
		vmOp = vm.OpTbz
	} else {
		vmOp = vm.OpTbnz
	}

	t.emitOp(vmOp, rd, byte(inst.Shift))
	fixPos := t.pos()
	t.emitU32(0)
	t.fixups = append(t.fixups, branchFixup{vmOffset: fixPos, arm64Target: target})
	return nil
}
