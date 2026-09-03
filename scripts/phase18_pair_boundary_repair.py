from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}: {old!r}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "internal/arch/arm64/decode_ldst.go",
    'Name: "STP", Mask: 0x3C400000, Value: 0x28000000, Op: STP,',
    'Name: "STP", Mask: 0x7C400000, Value: 0x28000000, Op: STP,',
)
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    'Name: "LDP", Mask: 0x3C400000, Value: 0x28400000, Op: LDP,',
    'Name: "LDP", Mask: 0x7C400000, Value: 0x28400000, Op: LDP,',
)
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    'Name: "LDPSW", Mask: 0x7C400000, Value: 0x28400000, Op: LDPSW,',
    'Name: "LDPSW", Mask: 0x7C400000, Value: 0x68400000, Op: LDPSW,',
)
replace_once(
    "internal/arch/arm64/policy.go",
    '''func validateImmediateAddressing(inst vm.Instruction) error {\n\tif inst.WB != 0 && inst.WB != 1 && inst.WB != 2 && inst.WB != 3 {\n\t\treturn fmt.Errorf("address writeback mode %d is invalid", inst.WB)\n\t}\n\t// Pair mode 2 is the architectural signed-offset form: it changes the\n\t// effective address but does not write back Rn. The stack pair\n\t// translators already treat every non-1/non-3 mode as offset-only.\n\tif inst.WB != 0 && inst.Rn == inst.Rd {\n\t\treturn fmt.Errorf("writeback base overlaps transfer register")\n\t}\n\tif (Op(inst.Op) == STP || Op(inst.Op) == LDP || Op(inst.Op) == LDPSW) &&\n\t\tinst.WB != 0 && inst.Rn == inst.Rm {\n\t\treturn fmt.Errorf("pair writeback base overlaps second transfer register")\n\t}\n\treturn nil\n}''',
    '''func validateImmediateAddressing(inst vm.Instruction) error {\n\top := Op(inst.Op)\n\tisPair := op == STP || op == LDP || op == LDPSW\n\tswitch inst.WB {\n\tcase 0, 1, 3:\n\tcase 2:\n\t\tif !isPair {\n\t\t\treturn fmt.Errorf("address mode 2 is only valid for pair signed-offset addressing")\n\t\t}\n\tdefault:\n\t\treturn fmt.Errorf("address writeback mode %d is invalid", inst.WB)\n\t}\n\n\t// Pair mode 2 is signed-offset addressing, not writeback. Only pre/post\n\t// indexed modes update Rn and therefore carry writeback-overlap rules.\n\twriteback := inst.WB == 1 || inst.WB == 3\n\tif writeback && inst.Rn == inst.Rd {\n\t\treturn fmt.Errorf("writeback base overlaps transfer register")\n\t}\n\tif isPair && writeback && inst.Rn == inst.Rm {\n\t\treturn fmt.Errorf("pair writeback base overlaps second transfer register")\n\t}\n\treturn nil\n}''',
)

p = Path("internal/arch/arm64/compiler_gap_regression_test.go")
text = p.read_text()
marker = '''func TestExactCompilerLogicalImmediateUsesUnsignedBitPattern(t *testing.T) {'''
if text.count(marker) != 1:
    raise SystemExit("regression test insertion marker mismatch")
addition = r'''func TestExactCompilerPairMaskSeparatesLDPSW(t *testing.T) {
	decoder := NewDecoder()
	inst := decoder.Decode(0x69400440, 0) // ldpsw x0, x1, [x2]
	if Op(inst.Op) != LDPSW || inst.Rd != 0 || inst.Rm != 1 || inst.Rn != 2 || inst.WB != 2 {
		t.Fatalf("LDPSW decoded as %s Rd=%d Rm=%d Rn=%d WB=%d", OpName(Op(inst.Op)), inst.Rd, inst.Rm, inst.Rn, inst.WB)
	}
	if err := validateInstructionPolicy(inst); err != nil {
		t.Fatalf("LDPSW policy: %v", err)
	}
	translator, err := NewTranslator(0x1800, 4, vm.IdentityOpcodeMap())
	if err != nil {
		t.Fatal(err)
	}
	result, err := translator.Translate([]vm.Instruction{inst})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("LDPSW translation unsupported=%v", result.Unsupported)
	}
}

func TestImmediateAddressMode2IsPairOnly(t *testing.T) {
	inst := vm.Instruction{Op: int(LDR_IMM), WB: 2, Rn: 0, Rd: 1}
	if err := validateImmediateAddressing(inst); err == nil {
		t.Fatal("single-register mode 2 addressing was accepted")
	}
	pair := vm.Instruction{Op: int(LDP), WB: 2, Rn: 0, Rd: 0, Rm: 1}
	if err := validateImmediateAddressing(pair); err != nil {
		t.Fatalf("pair signed-offset base overlap was treated as writeback: %v", err)
	}
}

'''
p.write_text(text.replace(marker, addition + marker, 1))
