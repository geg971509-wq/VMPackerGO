package arm64

import (
	"fmt"
	"sort"

	"github.com/vmpacker/internal/vm"
)

const (
	maxExclusiveRegionInstructions = 32
	maxExclusiveThunkRegisters     = 16
)

// trExclusiveRegion lowers one complete LDXR/LDAXR...STXR/STLXR sequence
// to a single bytecode operation. The generated runtime executes the exact
// instruction words in one leaf thunk, so no interpreter memory access can
// break the host exclusive monitor between the load and store.
func (t *Translator) trExclusiveRegion(instructions []vm.Instruction, start int) (int, error) {
	if start < 0 || start >= len(instructions) || !isExclusiveLoadOp(Op(instructions[start].Op)) {
		return 0, fmt.Errorf("exclusive region must start with LDXR or LDAXR")
	}

	first := instructions[start]
	firstOp := Op(first.Op)
	if err := validateDecodedExclusiveInstruction(t.decoder, first, firstOp); err != nil {
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
		if isExclusiveLoadOp(Op(inst.Op)) {
			return 0, fmt.Errorf("nested exclusive load is not a closed exclusive region")
		}
		if isExclusiveStoreOp(Op(inst.Op)) {
			end = i
			break
		}
		if err := validateExclusiveBodyInstruction(t.decoder, inst, first.Rn); err != nil {
			return 0, fmt.Errorf("exclusive region offset 0x%x: %w", inst.Offset, err)
		}
	}
	if end < 0 {
		return 0, fmt.Errorf("exclusive load has no contiguous STXR or STLXR within %d instructions", maxExclusiveRegionInstructions)
	}

	last := instructions[end]
	lastOp := Op(last.Op)
	if err := validateDecodedExclusiveInstruction(t.decoder, last, lastOp); err != nil {
		return 0, err
	}
	if last.Rn != first.Rn || last.Shift != first.Shift {
		return 0, fmt.Errorf("exclusive store address/width does not match exclusive load")
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
	if err := ValidateExclusiveRegion(region); err != nil {
		return 0, err
	}
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

func isExclusiveLoadOp(op Op) bool {
	return op == LDXR || op == LDAXR
}

func isExclusiveStoreOp(op Op) bool {
	return op == STXR || op == STLXR
}

func validateExclusiveRegister(reg int) error {
	if reg == vm.REG_XZR {
		return nil
	}
	if reg < 0 || reg > 30 {
		return fmt.Errorf("register %d is not a remappable X0-X30/XZR operand", reg)
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

func validateExclusiveRegion(region vm.ExclusiveRegion) ([]vm.Instruction, error) {
	if !region.Valid() {
		return nil, fmt.Errorf("exclusive region has an invalid content identifier")
	}
	if len(region.Instructions) < 2 || len(region.Instructions) > maxExclusiveRegionInstructions {
		return nil, fmt.Errorf("exclusive region length %d is outside [2,%d]", len(region.Instructions), maxExclusiveRegionInstructions)
	}
	decoder := NewDecoder()
	decoded := make([]vm.Instruction, len(region.Instructions))
	for i, raw := range region.Instructions {
		decoded[i] = decoder.Decode(raw, i*4)
	}
	first := decoded[0]
	last := decoded[len(decoded)-1]
	if !isExclusiveLoadOp(Op(first.Op)) || !isExclusiveStoreOp(Op(last.Op)) {
		return nil, fmt.Errorf("exclusive region must be bounded by LDXR/LDAXR and STXR/STLXR")
	}
	if first.Rn != last.Rn || first.Shift != last.Shift {
		return nil, fmt.Errorf("exclusive region address/width mismatch")
	}
	for name, reg := range map[string]int{
		"address": first.Rn, "load result": first.Rd,
		"store value": last.Rd, "status": last.Rm,
	} {
		if err := validateExclusiveRegister(reg); err != nil {
			return nil, fmt.Errorf("exclusive %s: %w", name, err)
		}
	}
	for i := 1; i < len(decoded)-1; i++ {
		if err := validateExclusiveBodyInstruction(decoder, decoded[i], first.Rn); err != nil {
			return nil, fmt.Errorf("exclusive region instruction %d: %w", i, err)
		}
	}
	return decoded, nil
}

type exclusiveRegisterField struct {
	register int
	shift    uint
}

func exclusiveRegisterFields(inst vm.Instruction) []exclusiveRegisterField {
	switch Op(inst.Op) {
	case LDXR, LDAXR:
		return []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
	case STXR, STLXR:
		return []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
	case ADD_IMM, SUB_IMM:
		return []exclusiveRegisterField{{register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
	case ADD_REG, SUB_REG, AND_REG, ORR_REG, EOR_REG, MUL:
		return []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
	default:
		return nil
	}
}

// PlanExclusiveThunk rewrites guest register fields into the generated thunk's X0-X15 bank.
func PlanExclusiveThunk(region vm.ExclusiveRegion) ([]uint32, []int, error) {
	decoded, err := validateExclusiveRegion(region)
	if err != nil {
		return nil, nil, err
	}

	seen := make(map[int]struct{})
	for _, inst := range decoded {
		for _, field := range exclusiveRegisterFields(inst) {
			if field.register != vm.REG_XZR {
				seen[field.register] = struct{}{}
			}
		}
	}
	registers := make([]int, 0, len(seen))
	for reg := range seen {
		registers = append(registers, reg)
	}
	sort.Ints(registers)
	if len(registers) > maxExclusiveThunkRegisters {
		return nil, nil, fmt.Errorf("exclusive region uses %d guest registers; thunk remap bank holds %d", len(registers), maxExclusiveThunkRegisters)
	}

	hostByGuest := make(map[int]uint32, len(registers))
	for host, guest := range registers {
		hostByGuest[guest] = uint32(host)
	}
	patched := append([]uint32(nil), region.Instructions...)
	for i, inst := range decoded {
		raw := patched[i]
		for _, field := range exclusiveRegisterFields(inst) {
			if field.register == vm.REG_XZR {
				continue
			}
			host := hostByGuest[field.register]
			raw = (raw &^ (uint32(0x1f) << field.shift)) | (host << field.shift)
		}
		patched[i] = raw
	}
	return patched, registers, nil
}

// ValidateExclusiveRegion validates both semantics and thunk-remap capacity.
func ValidateExclusiveRegion(region vm.ExclusiveRegion) error {
	_, _, err := PlanExclusiveThunk(region)
	return err
}
