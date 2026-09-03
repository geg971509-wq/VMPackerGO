from pathlib import Path


def replace(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"expected block not found in {path}")
    p.write_text(text.replace(old, new, 1))


path = "internal/arch/arm64/tr_exclusive.go"
old = '''\tend := -1
\tfor i := start + 1; i < len(instructions) && i-start < maxExclusiveRegionInstructions; i++ {
\t\tinst := instructions[i]
\t\tif inst.Offset != first.Offset+(i-start)*4 {
\t\t\treturn 0, fmt.Errorf("exclusive region is not contiguous at offset 0x%x", inst.Offset)
\t\t}
\t\tif isExclusiveLoadOp(Op(inst.Op)) {
\t\t\treturn 0, fmt.Errorf("nested exclusive load is not a closed exclusive region")
\t\t}
\t\tif isExclusiveStoreOp(Op(inst.Op)) {
\t\t\tend = i
\t\t\tbreak
\t\t}
\t\tif err := validateExclusiveBodyInstruction(t.decoder, inst, first.Rn); err != nil {
\t\t\treturn 0, fmt.Errorf("exclusive region offset 0x%x: %w", inst.Offset, err)
\t\t}
\t}
\tif end < 0 {
\t\treturn 0, fmt.Errorf("exclusive load has no contiguous supported store-exclusive within %d instructions", maxExclusiveRegionInstructions)
\t}

\tlast := instructions[end]
\tlastOp := Op(last.Op)
\tif err := validateDecodedExclusiveInstruction(t.decoder, last, lastOp); err != nil {
\t\treturn 0, err
\t}
\tif err := validateExclusiveBoundary(first, last); err != nil {
\t\treturn 0, err
\t}

\twords := make([]uint32, end-start+1)
\tfor i := range words {
\t\twords[i] = instructions[start+i].Raw
\t}
'''
new = '''\tlength, err := findExclusiveRegionLength(t.decoder, instructions, start)
\tif err != nil {
\t\treturn 0, err
\t}

\twords := make([]uint32, length)
\tfor i := range words {
\t\twords[i] = instructions[start+i].Raw
\t}
'''
replace(path, old, new)
replace(path, '''\treturn end - start, nil
}\n\nfunc validateDecodedExclusiveInstruction''', '''\treturn length - 1, nil
}\n\nfunc validateDecodedExclusiveInstruction''')

insert_at = '''func validateExclusiveBodyInstruction(decoder *Decoder, inst vm.Instruction, addressReg int) error {'''
helpers = r'''func findExclusiveRegionLength(decoder *Decoder, instructions []vm.Instruction, start int) (int, error) {
	maxLength := len(instructions) - start
	if maxLength > maxExclusiveRegionInstructions {
		maxLength = maxExclusiveRegionInstructions
	}
	if maxLength < 2 {
		return 0, fmt.Errorf("exclusive load has no following instruction")
	}

	seenStore := false
	var lastErr error
	for length := 2; length <= maxLength; length++ {
		if isExclusiveStoreOp(Op(instructions[start+length-1].Op)) {
			seenStore = true
		}
		if !seenStore {
			continue
		}
		candidate := instructions[start : start+length]
		if err := validateExclusiveCFG(decoder, candidate); err == nil {
			return length, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("exclusive load has no closed supported region within %d instructions", maxExclusiveRegionInstructions)
}

func validateExclusiveCFG(decoder *Decoder, instructions []vm.Instruction) error {
	if len(instructions) < 2 || len(instructions) > maxExclusiveRegionInstructions {
		return fmt.Errorf("exclusive region length %d is outside [2,%d]", len(instructions), maxExclusiveRegionInstructions)
	}
	first := instructions[0]
	firstOp := Op(first.Op)
	if !isExclusiveLoadOp(firstOp) {
		return fmt.Errorf("exclusive region must start with a supported load-exclusive instruction")
	}
	if err := validateDecodedExclusiveInstruction(decoder, first, firstOp); err != nil {
		return err
	}
	if err := validateExclusiveRegister(first.Rn); err != nil {
		return fmt.Errorf("exclusive address: %w", err)
	}
	if err := validateExclusiveRegister(first.Rd); err != nil {
		return fmt.Errorf("exclusive load result: %w", err)
	}
	if exclusiveRegisterArity(firstOp) == 2 {
		if err := validateExclusiveRegister(first.Rt2); err != nil {
			return fmt.Errorf("exclusive second load result: %w", err)
		}
		if first.Rd == first.Rt2 {
			return fmt.Errorf("pair-exclusive load destinations overlap")
		}
	}

	endOffset := first.Offset + len(instructions)*4
	stores := 0
	for i := 1; i < len(instructions); i++ {
		inst := instructions[i]
		if inst.Offset != first.Offset+i*4 {
			return fmt.Errorf("exclusive region is not contiguous at offset 0x%x", inst.Offset)
		}
		op := Op(inst.Op)
		if isExclusiveLoadOp(op) {
			return fmt.Errorf("nested exclusive load is not a closed exclusive region")
		}
		if isExclusiveStoreOp(op) {
			if err := validateDecodedExclusiveInstruction(decoder, inst, op); err != nil {
				return err
			}
			if err := validateExclusiveBoundary(first, inst); err != nil {
				return err
			}
			stores++
			continue
		}
		switch op {
		case B, B_COND, CBZ, CBNZ:
			if err := validateExclusiveBranchInstruction(decoder, inst, first.Offset, endOffset); err != nil {
				return fmt.Errorf("exclusive region offset 0x%x: %w", inst.Offset, err)
			}
		case CLREX:
			decoded := decoder.Decode(inst.Raw, inst.Offset)
			if Op(decoded.Op) != CLREX {
				return fmt.Errorf("CLREX fields do not match raw encoding 0x%08x", inst.Raw)
			}
		default:
			if err := validateExclusiveBodyInstruction(decoder, inst, first.Rn); err != nil {
				return fmt.Errorf("exclusive region offset 0x%x: %w", inst.Offset, err)
			}
		}
	}
	if stores == 0 {
		return fmt.Errorf("exclusive region has no matching store-exclusive")
	}
	return nil
}

func validateExclusiveBranchInstruction(decoder *Decoder, inst vm.Instruction, startOffset, endOffset int) error {
	decoded := decoder.Decode(inst.Raw, inst.Offset)
	if decoded.Op != inst.Op || decoded.Imm != inst.Imm || decoded.Rd != inst.Rd || decoded.Cond != inst.Cond || decoded.SF != inst.SF {
		return fmt.Errorf("%s fields do not match raw encoding 0x%08x", OpName(Op(inst.Op)), inst.Raw)
	}
	if Op(inst.Op) == CBZ || Op(inst.Op) == CBNZ {
		if err := validateExclusiveRegister(inst.Rd); err != nil {
			return fmt.Errorf("exclusive branch operand: %w", err)
		}
	}
	target := inst.Offset + int(inst.Imm)
	if target < startOffset || target > endOffset || (target-startOffset)%4 != 0 {
		return fmt.Errorf("%s target 0x%x is outside closed region [0x%x,0x%x]", OpName(Op(inst.Op)), target, startOffset, endOffset)
	}
	if target == inst.Offset {
		return fmt.Errorf("%s self-loop is not a supported exclusive retry", OpName(Op(inst.Op)))
	}
	if target < inst.Offset && target != startOffset {
		return fmt.Errorf("%s backward target 0x%x does not retry the exclusive load at 0x%x", OpName(Op(inst.Op)), target, startOffset)
	}
	return nil
}

'''
replace(path, insert_at, helpers + insert_at)

old_validate = r'''func validateExclusiveRegion(region vm.ExclusiveRegion) ([]vm.Instruction, error) {
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
		return nil, fmt.Errorf("exclusive region must be bounded by supported load/store-exclusive instructions")
	}
	if err := validateExclusiveBoundary(first, last); err != nil {
		return nil, err
	}
	for i := 1; i < len(decoded)-1; i++ {
		if err := validateExclusiveBodyInstruction(decoder, decoded[i], first.Rn); err != nil {
			return nil, fmt.Errorf("exclusive region instruction %d: %w", i, err)
		}
	}
	return decoded, nil
}
'''
new_validate = r'''func validateExclusiveRegion(region vm.ExclusiveRegion) ([]vm.Instruction, error) {
	if !region.Valid() {
		return nil, fmt.Errorf("exclusive region has an invalid content identifier")
	}
	decoder := NewDecoder()
	decoded := make([]vm.Instruction, len(region.Instructions))
	for i, raw := range region.Instructions {
		decoded[i] = decoder.Decode(raw, i*4)
	}
	if err := validateExclusiveCFG(decoder, decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
'''
replace(path, old_validate, new_validate)

replace(path, '''\tcase ADD_REG, SUB_REG, SUBS_REG, SUBS_EXT, AND_REG, ORR_REG, EOR_REG, MUL,
\t\tCSEL, CSINC, CSINV, CSNEG:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tdefault:''', '''\tcase ADD_REG, SUB_REG, SUBS_REG, SUBS_EXT, AND_REG, ORR_REG, EOR_REG, MUL,
\t\tCSEL, CSINC, CSINV, CSNEG:
\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}
\tcase CBZ, CBNZ:
\t\treturn []exclusiveRegisterField{{register: inst.Rd, shift: 0}}
\tdefault:''')

# Clear the local exclusive monitor before returning to the interpreter. Branches
# that exit a copied compare/exchange region target this cleanup instruction.
replace("internal/runtime/exclusivegen.go", '''\t\tfor _, raw := range patchedByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  .inst 0x%08x\\n", raw)
\t\t}
\t\ts.WriteString("  mrs x17, nzcv\\n''', '''\t\tfor _, raw := range patchedByID[region.ID] {
\t\t\tfmt.Fprintf(&s, "  .inst 0x%08x\\n", raw)
\t\t}
\t\ts.WriteString("  clrex\\n")
\t\ts.WriteString("  mrs x17, nzcv\\n''')

# Phase 19 removes only the branchful-exclusive expectation.
compiler = Path("internal/arch/arm64/compiler_corpus_test.go")
text = compiler.read_text()
start = text.index('''\tif record.Profile == "base" && strings.HasPrefix(record.Function, "vmp_atomic") {''')
end = text.index('''\n\tif record.Profile == "lse" && record.Function == "vmp_atomic128"''', start)
text = text[:start] + text[end:]
text = text.replace('''for _, kind := range []string{"branchful-exclusive", "casp128", "machine-outliner"} {''', '''for _, kind := range []string{"casp128", "machine-outliner"} {''', 1)
compiler.write_text(text)

# Keep the type comment accurate without changing the content-addressing contract.
replace("internal/vm/types.go", '''// ExclusiveRegion is a complete, contiguous load-exclusive...store-exclusive
// sequence that must execute without returning to the interpreter.''', '''// ExclusiveRegion is a complete, contiguous exclusive-monitor CFG beginning
// at a load-exclusive instruction. It may contain retry branches, multiple
// store-exclusive paths, or CLREX, and must execute without returning to the interpreter.''')
