package arm64

import (
	"fmt"

	"github.com/vmpacker/internal/vm"
)

const maxExclusiveRegionInstructions = 32

// trExclusiveRegion lowers one complete LDAXR...STLXR sequence to a single
// bytecode operation. The generated runtime executes the exact instruction
// words in one leaf thunk, so no interpreter memory access can break the host
// exclusive monitor between the load and store.
func (t *Translator) trExclusiveRegion(instructions []vm.Instruction, start int) (int, error) {
	if start < 0 || start >= len(instructions) || Op(instructions[start].Op) != LDAXR {
		return 0, fmt.Errorf("exclusive region must start with LDAXR")
	}

	first := instructions[start]
	if err := validateDecodedExclusiveInstruction(t.decoder, first, LDAXR); err != nil {
		return 0, err
	}
	if err := validateExclusiveRegister(first.Rn); err != nil {
		return 0, fmt.Errorf("exclusive address: %w", err)
	}
	if err := validateExclusiveRegister(first.Rd); err != nil {
		return 0, fmt.Errorf("exclusive load result: %w", err)
	}

	end := -1
	for i := start + 1; i < len(instructions) && i-start < maxExclusiveRegionInstructions; i++ {
		inst := instructions[i]
		if inst.Offset != first.Offset+(i-start)*4 {
			return 0, fmt.Errorf("exclusive region is not contiguous at offset 0x%x", inst.Offset)
		}
		if Op(inst.Op) == LDAXR {
			return 0, fmt.Errorf("nested LDAXR is not a closed exclusive region")
		}
		if Op(inst.Op) == STLXR {
			end = i
			break
		}
		if err := validateExclusiveBodyInstruction(t.decoder, inst, first.Rn); err != nil {
			return 0, fmt.Errorf("exclusive region offset 0x%x: %w", inst.Offset, err)
		}
	}
	if end < 0 {
		return 0, fmt.Errorf("LDAXR has no contiguous STLXR within %d instructions", maxExclusiveRegionInstructions)
	}

	last := instructions[end]
	if err := validateDecodedExclusiveInstruction(t.decoder, last, STLXR); err != nil {
		return 0, err
	}
	if last.Rn != first.Rn || last.Shift != first.Shift {
		return 0, fmt.Errorf("STLXR address/width does not match LDAXR")
	}
	for name, reg := range map[string]int{"store value": last.Rd, "status": last.Rm} {
		if err := validateExclusiveRegister(reg); err != nil {
			return 0, fmt.Errorf("exclusive %s: %w", name, err)
		}
	}

	words := make([]uint32, end-start+1)
	for i := range words {
		words[i] = instructions[start+i].Raw
	}
	region := vm.NewExclusiveRegion(words)
	if previous, ok := t.exclusiveRegions[region.ID]; ok {
		if !sameInstructionWords(previous.Instructions, region.Instructions) {
			return 0, fmt.Errorf("exclusive region identifier collision 0x%08x", region.ID)
		}
	} else {
		t.exclusiveRegions[region.ID] = region
	}
	t.emitOp(vm.OpExclusive)
	t.emitU32(region.ID)
	return end - start, nil
}

func validateDecodedExclusiveInstruction(decoder *Decoder, inst vm.Instruction, want Op) error {
	decoded := decoder.Decode(inst.Raw, inst.Offset)
	if Op(decoded.Op) != want || decoded.Rd != inst.Rd || decoded.Rn != inst.Rn ||
		decoded.Rm != inst.Rm || decoded.Shift != inst.Shift {
		return fmt.Errorf("%s fields do not match raw encoding 0x%08x", OpName(want), inst.Raw)
	}
	return nil
}

func validateExclusiveBodyInstruction(decoder *Decoder, inst vm.Instruction, addressReg int) error {
	decoded := decoder.Decode(inst.Raw, inst.Offset)
	if decoded.Op != inst.Op || decoded.Rd != inst.Rd || decoded.Rn != inst.Rn || decoded.Rm != inst.Rm {
		return fmt.Errorf("decoded fields do not match raw encoding 0x%08x", inst.Raw)
	}
	var registers []int
	switch Op(inst.Op) {
	case ADD_IMM, SUB_IMM:
		registers = []int{inst.Rd, inst.Rn}
	case ADD_REG, SUB_REG, AND_REG, ORR_REG, EOR_REG, MUL:
		registers = []int{inst.Rd, inst.Rn, inst.Rm}
	default:
		return fmt.Errorf("%s is not in the branch-free exclusive-body whitelist", OpName(Op(inst.Op)))
	}
	for _, reg := range registers {
		if err := validateExclusiveRegister(reg); err != nil {
			return err
		}
	}
	if inst.Rd == addressReg {
		return fmt.Errorf("exclusive body overwrites address register X%d", addressReg)
	}
	return nil
}

// X16/X17 are reserved by generated thunks, X18 is platform state, and
// callee-saved registers would require a larger host ABI frame. Restricting the
// closed region to caller-saved X0-X15 makes that invariant machine-checkable.
func validateExclusiveRegister(reg int) error {
	if reg < 0 || reg > 15 {
		return fmt.Errorf("register X%d is outside the exclusive-thunk bank X0-X15", reg)
	}
	return nil
}

func sameInstructionWords(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidateExclusiveRegion independently validates a content-addressed region
// before runtime code generation. This keeps Build fail-closed even if its
// caller did not obtain the region from Translator.
func ValidateExclusiveRegion(region vm.ExclusiveRegion) error {
	if !region.Valid() {
		return fmt.Errorf("exclusive region has an invalid content identifier")
	}
	if len(region.Instructions) < 2 || len(region.Instructions) > maxExclusiveRegionInstructions {
		return fmt.Errorf("exclusive region length %d is outside [2,%d]", len(region.Instructions), maxExclusiveRegionInstructions)
	}
	decoder := NewDecoder()
	decoded := make([]vm.Instruction, len(region.Instructions))
	for i, raw := range region.Instructions {
		decoded[i] = decoder.Decode(raw, i*4)
	}
	first := decoded[0]
	last := decoded[len(decoded)-1]
	if Op(first.Op) != LDAXR || Op(last.Op) != STLXR {
		return fmt.Errorf("exclusive region must be bounded by LDAXR and STLXR")
	}
	if first.Rn != last.Rn || first.Shift != last.Shift {
		return fmt.Errorf("exclusive region address/width mismatch")
	}
	for name, reg := range map[string]int{
		"address": first.Rn, "load result": first.Rd,
		"store value": last.Rd, "status": last.Rm,
	} {
		if err := validateExclusiveRegister(reg); err != nil {
			return fmt.Errorf("exclusive %s: %w", name, err)
		}
	}
	for i := 1; i < len(decoded)-1; i++ {
		if err := validateExclusiveBodyInstruction(decoder, decoded[i], first.Rn); err != nil {
			return fmt.Errorf("exclusive region instruction %d: %w", i, err)
		}
	}
	return nil
}
