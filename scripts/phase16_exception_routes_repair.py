#!/usr/bin/env python3
from pathlib import Path

path = Path("internal/elf/exception_routes_test.go")
text = path.read_text()
old = "if plan.Thunks[0] != originalPlan || !reflect.DeepEqual(result.Bytecode, originalBytecode) {"
new = "if !reflect.DeepEqual(plan.Thunks[0], originalPlan) || !reflect.DeepEqual(result.Bytecode, originalBytecode) {"
if text.count(old) != 1:
    raise SystemExit(f"expected one non-comparable thunk assertion, found {text.count(old)}")
path.write_text(text.replace(old, new, 1))
print("phase16 thunk immutability assertion repaired")
