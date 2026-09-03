from pathlib import Path

path = Path("internal/arch/arm64/tr_exclusive.go")
text = path.read_text()
old = '''\tcase ADD_REG, SUB_REG, SUBS_REG, AND_REG, ORR_REG, EOR_REG, MUL,\n\t\tCSEL, CSINC, CSINV, CSNEG:\n\t\tregisters = []int{inst.Rd, inst.Rn, inst.Rm}'''
new = '''\tcase ADD_REG, SUB_REG, SUBS_REG, SUBS_EXT, AND_REG, ORR_REG, EOR_REG, MUL,\n\t\tCSEL, CSINC, CSINV, CSNEG:\n\t\tregisters = []int{inst.Rd, inst.Rn, inst.Rm}'''
if text.count(old) != 1:
    raise SystemExit(f"exclusive body whitelist anchor count={text.count(old)}")
text = text.replace(old, new, 1)
old = '''\tcase ADD_REG, SUB_REG, SUBS_REG, AND_REG, ORR_REG, EOR_REG, MUL,\n\t\tCSEL, CSINC, CSINV, CSNEG:\n\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}'''
new = '''\tcase ADD_REG, SUB_REG, SUBS_REG, SUBS_EXT, AND_REG, ORR_REG, EOR_REG, MUL,\n\t\tCSEL, CSINC, CSINV, CSNEG:\n\t\treturn []exclusiveRegisterField{{register: inst.Rm, shift: 16}, {register: inst.Rn, shift: 5}, {register: inst.Rd, shift: 0}}'''
if text.count(old) != 1:
    raise SystemExit(f"exclusive remap whitelist anchor count={text.count(old)}")
path.write_text(text.replace(old, new, 1))
