#!/usr/bin/env python3
from pathlib import Path

path = Path("internal/arch/arm64/tr_exclusive.go")
text = path.read_text()
old = '''\tfor name, reg := range []struct {
\t\tname string
\t\treg  int
\t}{
\t\t{"address", first.Rn}, {"load result", first.Rd},
\t\t{"store value", last.Rd}, {"status", last.Rm},
\t} {
\t\tif err := validateExclusiveRegister(reg); err != nil {
\t\t\treturn fmt.Errorf("exclusive %s: %w", name, err)
\t\t}
\t}
'''
new = '''\tfor _, operand := range []struct {
\t\tname string
\t\treg  int
\t}{
\t\t{"address", first.Rn}, {"load result", first.Rd},
\t\t{"store value", last.Rd}, {"status", last.Rm},
\t} {
\t\tif err := validateExclusiveRegister(operand.reg); err != nil {
\t\t\treturn fmt.Errorf("exclusive %s: %w", operand.name, err)
\t\t}
\t}
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one generated operand loop, found {text.count(old)}")
path.write_text(text.replace(old, new, 1))
print("phase12 generated operand loop repaired")
