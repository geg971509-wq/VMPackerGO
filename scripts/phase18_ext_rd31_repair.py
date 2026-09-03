from pathlib import Path

path = Path("internal/arch/arm64/decode_dp_reg.go")
text = path.read_text()
old = '''// postExtReg extended register: option→ShiftType, imm3→Shift, Rn=31→SP(保留), Rd=31→SP(保留), Rm→XZR\nfunc postExtReg(f map[string]int64, inst *vm.Instruction) {\n\t// Rd=31 在 extended register 中也是 SP (如 SUB SP, SP, Xm)，不做 XZR 替换\n\txzrReplace(&inst.Rm)\n\t// Rn=31 在 extended register 中是 SP, 不做 XZR 替换\n\tif option, ok := f["option"]; ok {\n\t\tinst.ShiftType = int(option) // 0=UXTB..7=SXTX\n\t}\n\tif imm3, ok := f["imm3"]; ok {\n\t\tinst.Shift = int(imm3) // 额外左移量 0-4\n\t}\n}'''
new = '''// postExtReg extended register: option→ShiftType, imm3→Shift. Rn=31 is SP.\n// Rd=31 is SP for non-flag ADD/SUB, but XZR for ADDS/SUBS (CMP/CMN aliases).\nfunc postExtReg(f map[string]int64, inst *vm.Instruction) {\n\txzrReplace(&inst.Rm)\n\tif Op(inst.Op) == ADDS_EXT || Op(inst.Op) == SUBS_EXT {\n\t\txzrReplace(&inst.Rd)\n\t}\n\tif option, ok := f["option"]; ok {\n\t\tinst.ShiftType = int(option) // 0=UXTB..7=SXTX\n\t}\n\tif imm3, ok := f["imm3"]; ok {\n\t\tinst.Shift = int(imm3) // extra left shift 0-4\n\t}\n}'''
count = text.count(old)
if count != 1:
    raise SystemExit(f"postExtReg anchor count={count}")
path.write_text(text.replace(old, new, 1))
