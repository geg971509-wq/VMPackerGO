package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

func init() {
	// CASP is decoded into the existing CAS semantic family. Override only the
	// CAS validator so pair encodings can apply their stricter register-pair
	// rules without widening the scalar atomic policy.
	instructionRules[CAS] = instructionRule{disposition: dispositionVirtual, validate: validateCASOrPair}
}

func validateCASOrPair(inst vm.Instruction) error {
	if !isCASPPair(inst) {
		return validateAtomicNative(inst)
	}
	if inst.Shift != 4 && inst.Shift != 8 {
		return fmt.Errorf("CASP pair member width %d is unsupported", inst.Shift)
	}
	if inst.Rn < 0 || inst.Rn > 31 {
		return fmt.Errorf("CASP address register X%d is invalid", inst.Rn)
	}
	validPairLow := func(reg int) bool {
		// Architectural CASP requires even pair lows. Low register 30 is valid;
		// its implicit high member is encoding 31 (ZR), which the runtime pair
		// transport handles explicitly rather than aliasing VM R31/SP.
		return reg >= 0 && reg <= 30 && reg&1 == 0
	}
	if !validPairLow(inst.Rm) {
		return fmt.Errorf("CASP expected/result pair low register %d is invalid", inst.Rm)
	}
	if !validPairLow(inst.Rd) {
		return fmt.Errorf("CASP replacement pair low register %d is invalid", inst.Rd)
	}
	return nil
}
