package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

const atomicZeroRegister = byte(0xff)

func encodeAtomicRegister(reg int) (byte, error) {
	if reg == vm.REG_XZR {
		return atomicZeroRegister, nil
	}
	if reg < 0 || reg > 31 {
		return 0, fmt.Errorf("atomic register %d is invalid", reg)
	}
	return byte(reg), nil
}

func atomicMemoryOrder(op Op, raw uint32) byte {
	switch op {
	case LDAR:
		return 1
	case STLR:
		return 2
	case LDADD:
		acquire := byte((raw >> 23) & 1)
		release := byte((raw >> 22) & 1)
		return acquire | release<<1
	case CAS:
		acquire := byte((raw >> 22) & 1)
		release := byte((raw >> 15) & 1)
		return acquire | release<<1
	default:
		panic("atomicMemoryOrder called for a non-atomic operation")
	}
}

func (t *Translator) trAtomic(inst vm.Instruction) error {
	op := Op(inst.Op)
	kind := map[Op]byte{LDAR: 0, STLR: 1, LDADD: 2, CAS: 3}[op]
	rd, err := encodeAtomicRegister(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	rm := atomicZeroRegister
	if op == LDADD || op == CAS {
		rm, err = encodeAtomicRegister(inst.Rm)
		if err != nil {
			return err
		}
	}
	t.emitOp(vm.OpAtomic, kind, byte(inst.Shift), atomicMemoryOrder(op, inst.Raw), rd, rn, rm)
	return nil
}
