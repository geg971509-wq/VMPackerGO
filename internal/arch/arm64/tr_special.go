package arm64

import (
	"fmt"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

// ============================================================
// 特殊指令翻译 — ADRP / ADR
// ============================================================

func (t *Translator) trADRP(instructions []vm.Instruction, idx int) (int, error) {
	inst := instructions[idx]
	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return 0, err
	}

	pc := t.funcAddr + uint64(inst.Offset)
	pageBase := pc &^ 0xFFF
	adrpResult, err := addAddressDelta(pageBase, inst.Imm)
	if err != nil {
		return 0, err
	}

	if idx+1 < len(instructions) {
		next := instructions[idx+1]
		if Op(next.Op) == ADD_IMM && next.Rd == inst.Rd && next.Rn == inst.Rd {
			finalAddr, err := addAddressDelta(adrpResult, next.Imm)
			if err != nil {
				return 0, err
			}
			t.emitImageReference(vm.OpMovImage, &rd, finalAddr)
			return 1, nil
		}
	}

	t.emitImageReference(vm.OpMovImage, &rd, adrpResult)
	return 0, nil
}

func (t *Translator) trADR(inst vm.Instruction) (int, error) {
	rd, err := t.mapReg(inst.Rd)
	if err != nil {
		return 0, err
	}
	pc := t.funcAddr + uint64(inst.Offset)
	addr, err := addAddressDelta(pc, inst.Imm)
	if err != nil {
		return 0, err
	}
	t.emitImageReference(vm.OpMovImage, &rd, addr)
	return 0, nil
}

func addAddressDelta(base uint64, delta int64) (uint64, error) {
	if delta >= 0 {
		value := base + uint64(delta)
		if value < base {
			return 0, fmt.Errorf("PC-relative address overflows")
		}
		return value, nil
	}
	amount := uint64(-(delta + 1)) + 1
	if amount > base {
		return 0, fmt.Errorf("PC-relative address underflows")
	}
	return base - amount, nil
}

// trSVC 翻译 SVC #imm16
// 字节码: [OpSvc][imm16_lo][imm16_hi] = 3B
// handler 使用 inline asm 执行 svc #0，从 VM 寄存器传递 syscall 参数
func (t *Translator) trSVC(inst vm.Instruction) error {
	imm16 := uint16(inst.Imm)
	t.svcImmediates[imm16] = true
	t.emitOp(vm.OpSvc, byte(imm16), byte(imm16>>8))
	return nil
}
