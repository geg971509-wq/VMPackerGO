package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

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
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if _, exists := t.nativeTailTransfers[branchOffset]; exists {
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if err := ValidateOutlinedTailHelper(raws); err != nil {
		return err
	}
	t.outlinedTailInlines[branchOffset] = append([]uint32(nil), raws...)
	return nil
}

// SetPackedTailTransfer marks a terminal direct B whose target is another
// selection in the same pack operation. A nil value is an internal sentinel;
// real outlined helpers are always non-empty after validation.
func (t *Translator) SetPackedTailTransfer(branchOffset int) error {
	if branchOffset < 0 || branchOffset%4 != 0 || branchOffset+4 != t.funcSize {
		return fmt.Errorf("packed tail branch offset 0x%x is not the final instruction of a 0x%x-byte function", branchOffset, t.funcSize)
	}
	if _, exists := t.outlinedTailInlines[branchOffset]; exists {
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if _, exists := t.nativeTailTransfers[branchOffset]; exists {
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	t.outlinedTailInlines[branchOffset] = nil
	return nil
}

// SetNativeTailTransfer marks a terminal direct B to an executable target
// outside the selected set. It is deliberately lowered to CALL_IMAGE + RET
// instead of branching out of the interpreter: this preserves cleanup,
// AAPCS64 bridge validation, and exception/unwind routing.
func (t *Translator) SetNativeTailTransfer(branchOffset int) error {
	if branchOffset < 0 || branchOffset%4 != 0 || branchOffset+4 != t.funcSize {
		return fmt.Errorf("native tail branch offset 0x%x is not the final instruction of a 0x%x-byte function", branchOffset, t.funcSize)
	}
	if _, exists := t.outlinedTailInlines[branchOffset]; exists {
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	if _, exists := t.nativeTailTransfers[branchOffset]; exists {
		return fmt.Errorf("tail branch offset 0x%x is configured more than once", branchOffset)
	}
	t.nativeTailTransfers[branchOffset] = struct{}{}
	return nil
}

func (t *Translator) trBranchOrOutlined(inst vm.Instruction) error {
	if _, configured := t.nativeTailTransfers[inst.Offset]; configured {
		target := int64(inst.Offset) + inst.Imm
		if target >= 0 && target < int64(t.funcSize) {
			return fmt.Errorf("native tail handling at 0x%x is configured for an in-function branch target 0x%x", inst.Offset, target)
		}
		return t.trNativeTail(inst)
	}
	raws, configured := t.outlinedTailInlines[inst.Offset]
	if !configured {
		return t.trBranch(inst)
	}
	target := int64(inst.Offset) + inst.Imm
	if target >= 0 && target < int64(t.funcSize) {
		return fmt.Errorf("external tail handling at 0x%x is configured for an in-function branch target 0x%x", inst.Offset, target)
	}
	if raws == nil {
		return t.trPackedTail(inst)
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

// trPackedTail preserves the direct-B ABI contract. X16/IP0 is the only
// temporary and is permitted to be clobbered by an external AAPCS64 transfer
// or linker veneer. The runtime BR_REG path switches to the packed callee
// without growing call depth; if provenance no longer matches the immutable
// token table, execution faults rather than falling through to native code.
func (t *Translator) trPackedTail(inst vm.Instruction) error {
	pc, err := addAddressDelta(t.funcAddr, int64(inst.Offset))
	if err != nil {
		return err
	}
	target, err := addAddressDelta(pc, inst.Imm)
	if err != nil {
		return err
	}
	ip0 := byte(16)
	t.emitImageReference(vm.OpMovImage, &ip0, target)
	t.emitOp(vm.OpBrReg, ip0)
	return nil
}

// trNativeTail de-optimizes an external A64 tail branch to a validated
// native call followed by the protected function return. The call site is
// recorded explicitly because the source instruction is B rather than BL.
func (t *Translator) trNativeTail(inst vm.Instruction) error {
	pc, err := addAddressDelta(t.funcAddr, int64(inst.Offset))
	if err != nil {
		return err
	}
	target, err := addAddressDelta(pc, inst.Imm)
	if err != nil {
		return err
	}
	vmOffset := t.pos()
	t.emitImageReference(vm.OpCallImage, nil, target)
	t.nativeCallSites = append(t.nativeCallSites, NativeCallSite{
		ARM64Offset: inst.Offset,
		VMOffset:    vmOffset,
	})
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
