#!/usr/bin/env python3
"""Convert ELF64 PT_NOTE program headers to PT_NULL for no-note fixtures.

This is a test-fixture helper for validating the plan-first Phase 8 writer. It does
not alter section contents; it only removes runtime PT_NOTE program-header slots
so the packer must use a non-note payload segment strategy.
"""

from __future__ import annotations

import argparse
import struct
from pathlib import Path

PT_NULL = 0
PT_NOTE = 4


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("input")
    ap.add_argument("output")
    args = ap.parse_args()

    data = bytearray(Path(args.input).read_bytes())
    if len(data) < 64 or data[:4] != b"\x7fELF":
        raise SystemExit("not an ELF file")
    if data[4] != 2 or data[5] != 1:
        raise SystemExit("expected ELF64 little-endian input")

    phoff = struct.unpack_from("<Q", data, 0x20)[0]
    phentsize = struct.unpack_from("<H", data, 0x36)[0]
    phnum = struct.unpack_from("<H", data, 0x38)[0]
    changed = 0
    for i in range(phnum):
        off = phoff + i * phentsize
        if off + 4 > len(data):
            raise SystemExit(f"program header {i} is outside file")
        p_type = struct.unpack_from("<I", data, off)[0]
        if p_type == PT_NOTE:
            struct.pack_into("<I", data, off, PT_NULL)
            changed += 1

    if changed == 0:
        raise SystemExit("no PT_NOTE program header found")

    Path(args.output).write_bytes(data)
    print(f"[+] {args.output}: converted {changed} PT_NOTE header(s) to PT_NULL")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

