#!/usr/bin/env python3
from pathlib import Path


def main():
    path = Path("internal/elf/rewrite_plan.go")
    text = path.read_text()
    old = '''\tif !found {
\t\treturn nil
\t}
'''
    new = '''\tif !found {
\t\tif planner.preparation != nil && len(planner.preparation.ExceptionBridges) != 0 {
\t\t\treturn fmt.Errorf("exception bridge requires a discoverable PT_GNU_EH_FRAME unwind index")
\t\t}
\t\treturn nil
\t}
'''
    if text.count(old) != 1:
        raise SystemExit("reserveGNUUnwindIndex missing-header block changed")
    path.write_text(text.replace(old, new, 1))


if __name__ == "__main__":
    main()
