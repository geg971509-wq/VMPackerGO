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
    'Name: "STP", Mask: 0x1C400000, Value: 0x08000000, Op: STP,',
    'Name: "STP", Mask: 0x3C400000, Value: 0x28000000, Op: STP,',
)
replace_once(
    "internal/arch/arm64/decode_ldst.go",
    'Name: "LDP", Mask: 0x1C400000, Value: 0x08400000, Op: LDP,',
    'Name: "LDP", Mask: 0x3C400000, Value: 0x28400000, Op: LDP,',
)
replace_once(
    "internal/arch/arm64/policy.go",
    '''func validateImmediateDataProcessing(inst vm.Instruction) error {\n\tif inst.Imm < 0 {\n\t\treturn fmt.Errorf("negative data-processing immediate")\n\t}\n\treturn nil\n}''',
    '''func validateImmediateDataProcessing(inst vm.Instruction) error {\n\tswitch Op(inst.Op) {\n\tcase AND_IMM, ANDS_IMM, ORR_IMM, EOR_IMM:\n\t\t// Logical immediates are unsigned register-width bit patterns. For\n\t\t// 64-bit forms, a valid pattern with bit 63 set is represented in\n\t\t// Instruction.Imm as a negative int64; the translator deliberately\n\t\t// reinterprets it as uint64 when emitting the stack immediate.\n\t\treturn nil\n\t}\n\tif inst.Imm < 0 {\n\t\treturn fmt.Errorf("negative data-processing immediate")\n\t}\n\treturn nil\n}''',
)
replace_once(
    "internal/arch/arm64/policy.go",
    '''\tif inst.WB != 0 && inst.WB != 1 && inst.WB != 3 {\n\t\treturn fmt.Errorf("address writeback mode %d is invalid", inst.WB)\n\t}''',
    '''\tif inst.WB != 0 && inst.WB != 1 && inst.WB != 2 && inst.WB != 3 {\n\t\treturn fmt.Errorf("address writeback mode %d is invalid", inst.WB)\n\t}\n\t// Pair mode 2 is the architectural signed-offset form: it changes the\n\t// effective address but does not write back Rn. The stack pair\n\t// translators already treat every non-1/non-3 mode as offset-only.''',
)
