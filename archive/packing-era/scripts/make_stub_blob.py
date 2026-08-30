#!/usr/bin/env python3
"""Build cmd/vmpacker/vm_interp.bin from a flat interpreter blob and nm symbols."""

import argparse
import struct
import subprocess


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--nm", required=True)
    parser.add_argument("--elf", required=True)
    parser.add_argument("--raw", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    nm = subprocess.check_output([args.nm, args.elf], text=True)

    def sym(name: str) -> int:
        for line in nm.splitlines():
            parts = line.split()
            if len(parts) >= 3 and parts[2] == name:
                return int(parts[0], 16)
        raise SystemExit(f"symbol not found: {name}")

    entry = sym("vm_entry")
    token_entry = sym("vm_entry_token")
    token_table = sym("_token_table_va")
    image_file = sym("_image_file_va")
    raw = open(args.raw, "rb").read()
    blob = struct.pack("<QQQQ", entry, token_entry, token_table, image_file) + raw
    open(args.out, "wb").write(blob)
    print(
        f"[+] vm_interp.bin: {len(blob)} bytes "
        f"(vm_entry=0x{entry:X} vm_entry_token=0x{token_entry:X} "
        f"_token_table_va=0x{token_table:X} _image_file_va=0x{image_file:X})"
    )


if __name__ == "__main__":
    main()
