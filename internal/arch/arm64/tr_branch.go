package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

// ============================================================
// 分支翻译 — B / B.cond / BL / BLR / BR / TBZ
// CSEL/CBZ 已迁移到 tr_stack.go (trStackCSEL/trStackCBZ)
// ============================================================

// MaxOutlinedTailHelperInstructions bounds the compiler-generated helper body
// accepted for pack-time tail inlining. Exact NDK r29 currently emits at most
// five instructions including the terminal RET; the wider bound leaves modest
// headroom without turning this into a generic external-code importer.
const MaxOutlinedTailHelperInstructions = 16

// ValidateOutlinedTailHelper deliberately accepts only the exact semantic
// class proven by the exact-r29 machine-outliner audit: one or more unshifted
// 32-bit EOR(register) instructions followed by RET X30. Any future helper
// shape must first re-enter the compiler-derived audit rather than silently
// widening product behavior.
func ValidateOutlinedTailHelper(raws []uint32) error {
	if len(raws) < 2 {
		return fmt.Errorf("outlined helper must contain a body and terminal RET")
	}
	if len(raws) > MaxOutlinedTailHelperInstructions {
		return fmt.Errorf("outlined helper has %d instructions; maximum is %d", len(raws), MaxOutlinedTailHelperInstructions)
	}
	if raws[len(raws)-1] != 0xd65f03c0 {
		return fmt.Errorf("outlined helper must terminate with RET X30")
	}
	decoder := NewDecoder()
	for i, raw := range raws[:len(raws)-1] {
		inst := decoder.Decode(raw, i*4)
		if Op(inst.Op) != EOR_REG || inst.SF || inst.Shift != 0 || inst.ShiftType != 0 {
			return fmt.Errorf("outlined helper instruction %d raw=0x%08x is not unshifted EOR Wd, Wn, Wm", i, raw)
		}
		if err := validateInstructionPolicy(inst); err != nil {
			return fmt.Errorf("outlined helper instruction %d: %w", i, err)
		}
	}
	return nil
}

// SetOutlinedTailInline binds a proven helper body to the caller's original
// final B instruction. No synthetic ARM64 offsets are created; SourceMap and
// exception identities remain anchored to the selected function.
func (t *Translator) SetOutlinedTailInline(branchOffset int, raws []uint32) error {
	if branchOffset < 0 || branchOffset%4 != 0 || branchOffset+4 != t.funcSize {
		return fmt.Errorf("outlined tail branch offset 0x%x is not the final instruction of a 0x%x-byte function", branchOffset, t.funcSize)
	}
	if _, exists := t.outlinedTailInlines[branchOffset]; exists {
		return fmt.Errorf("outlined tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if err := ValidateOutlinedTailHelper(raws); err != nil {
		return err
	}
	t.outlinedTailInlines[branchOffset] = append([]uint32(nil), raws...)
	return nil
}

func (t *Translator) trBranchOrOutlined(inst vm.Instruction) error {
	raws, ok := t.outlinedTailInlines[inst.Offset]
	if !ok {
		return t.trBranch(inst)
	}
	target := int64(inst.Offset) + inst.Imm
	if target >= 0 && target < int64(t.funcSize) {
		return fmt.Errorf("outlined tail inline at 0x%x is configured for an in-function branch target 0x%x", inst.Offset, target)
	}
	return t.trOutlinedTailInline(raws)
}

func (t *Translator) trOutlinedTailInline(raws []uint32) error {
	if err := ValidateOutlinedTailHelper(raws); err != nil {
		return err
	}
	for i, raw := range raws[:len(raws)-1] {
		inst := t.decoder.Decode(raw, i*4)
		if err := t.trStackAluReg(inst, vm.OpSXor); err != nil {
			return fmt.Errorf("inline outlined helper instruction %d: %w", i, err)
		}
	}
	// Original A64 semantics are tail B to helper followed by helper RET using
	// the caller's existing LR. Inlined helper body + VM RET is equivalent.
	t.emitOp(vm.OpRet, 0)
	return nil
}

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
