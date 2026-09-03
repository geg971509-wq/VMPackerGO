from pathlib import Path

path = Path("internal/arch/arm64/tr_exclusive.go")
text = path.read_text()
old = '''// trExclusiveRegion lowers one complete scalar/pair load-exclusive...\n// store-exclusive sequence to a single bytecode operation. The generated runtime executes the exact\n// instruction words in one leaf thunk, so no interpreter memory access can\n// break the host exclusive monitor between the load and store.\n'''
new = '''// trExclusiveRegion lowers the shortest validated scalar/pair exclusive-monitor\n// CFG to one bytecode operation. The generated runtime executes the exact raw\n// block in one leaf thunk, so retry branches, store-exclusive paths, and CLREX\n// termination cannot be interrupted by interpreter memory access.\n'''
if old not in text:
    raise SystemExit("expected trExclusiveRegion comment not found")
path.write_text(text.replace(old, new, 1))
