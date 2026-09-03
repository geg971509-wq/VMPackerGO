#!/usr/bin/env python3
from pathlib import Path

p = Path("scripts/phase17_invoke_runtime_patch.py")
text = p.read_text()
old = 'len(image.ExclusiveRegions) != 2'
count = text.count(old)
if count == 0:
    raise SystemExit("expected at least one legacy two-exclusive fixture assertion")
text = text.replace(old, 'len(image.ExclusiveRegions) != 3')
p.write_text(text)
print(f"phase17 current-baseline repair updated {count} exclusive fixture assertions")
