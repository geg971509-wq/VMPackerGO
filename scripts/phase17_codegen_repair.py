#!/usr/bin/env python3
from pathlib import Path

p = Path("scripts/phase17_invoke_runtime_patch.py")
text = p.read_text()

# Keep invoke function prefix exclusive to functions so unwind validation does
# not confuse LSDA object symbols with executable FDE-bearing symbols.
text = text.replace("vm_invoke_lsda_", "vm_lsda_invoke_")

old = '''\tif cfg.Plan.Thunks[0] != before {
\t\tt.Fatal("invoke generator mutated input plan")
\t}
'''
new = '''\tafter := cfg.Plan.Thunks[0]
\tif after.ID != before.ID || after.OriginalPC != before.OriginalPC || after.OriginalLandingPad != before.OriginalLandingPad || after.VMCallOffset != before.VMCallOffset || after.VMLandingPad != before.VMLandingPad || after.Action != before.Action || len(after.Actions) != len(before.Actions) {
\t\tt.Fatal("invoke generator mutated input plan")
\t}
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one non-comparable thunk assertion, found {text.count(old)}")
text = text.replace(old, new, 1)

old = '''\tif !bytes.Equal(got[0].LSDA.Bytes, wantLSDA.Bytes) || len(got[0].LSDA.Relocations) != len(wantLSDA.Relocations) {
'''
new = '''\tif string(got[0].LSDA.Bytes) != string(wantLSDA.Bytes) || len(got[0].LSDA.Relocations) != len(wantLSDA.Relocations) {
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one bytes.Equal assertion, found {text.count(old)}")
text = text.replace(old, new, 1)

old = '''strings.HasPrefix(symbol.Name, "vm_fpsimd_") || strings.HasPrefix(symbol.Name, "vm_invoke_") {
'''
new = '''strings.HasPrefix(symbol.Name, "vm_fpsimd_") || (strings.HasPrefix(symbol.Name, "vm_invoke_") && elf.ST_TYPE(symbol.Info) == elf.STT_FUNC) {
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one invoke unwind-prefix clause, found {text.count(old)}")
text = text.replace(old, new, 1)

p.write_text(text)
print("phase17 codegen boundaries repaired")
