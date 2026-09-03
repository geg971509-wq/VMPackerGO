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

func atomicMemoryOrder(inst vm.Instruction) byte {
	op := Op(inst.Op)
	switch op {
	case LDAR:
		return 1
	case STLR:
		return 2
	case LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:
		acquire := byte((inst.Raw >> 23) & 1)
		// ARM suppresses acquire for this LSE RMW class when Rt is XZR.
		if inst.Rd == vm.REG_XZR {
			acquire = 0
		}
		release := byte((inst.Raw >> 22) & 1)
		return acquire | release<<1
	case CAS:
		acquire := byte((inst.Raw >> 22) & 1)
		release := byte((inst.Raw >> 15) & 1)
		return acquire | release<<1
	default:
		panic("atomicMemoryOrder called for a non-atomic operation")
	}
}

func (t *Translator) trAtomic(inst vm.Instruction) error {
	op := Op(inst.Op)
	kind := map[Op]byte{
		LDAR: 0, STLR: 1, LDADD: 2, CAS: 3,
		SWP: 4, LDCLR: 5, LDEOR: 6, LDSET: 7,
		LDSMAX: 8, LDSMIN: 9, LDUMAX: 10, LDUMIN: 11,
	}[op]
	rd, err := encodeAtomicRegister(inst.Rd)
	if err != nil {
		return err
	}
	rn, err := t.mapReg(inst.Rn)
	if err != nil {
		return err
	}
	rm := atomicZeroRegister
	if atomicUsesRm(op) {
		rm, err = encodeAtomicRegister(inst.Rm)
		if err != nil {
			return err
		}
	}
	t.emitOp(vm.OpAtomic, kind, byte(inst.Shift), atomicMemoryOrder(inst), rd, rn, rm)
	return nil
}
