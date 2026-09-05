#!/usr/bin/env bash
set -euo pipefail

protected="${1:?usage: audit_vmp_cmd.sh <protected-elf> <symbol-elf> <funcs>}"
symbol_elf="${2:?usage: audit_vmp_cmd.sh <protected-elf> <symbol-elf> <funcs>}"
funcs="${3:?usage: audit_vmp_cmd.sh <protected-elf> <symbol-elf> <funcs>}"

if [ ! -f "$protected" ] || [ ! -f "$symbol_elf" ]; then
    echo "VMP audit input missing" >&2
    exit 1
fi

find_tool() {
    local override="$1"
    shift
    if [ -n "$override" ] && [ -x "$override" ]; then
        printf '%s\n' "$override"
        return 0
    fi

    local name
    for name in "$@"; do
        if command -v "$name" >/dev/null 2>&1; then
            command -v "$name"
            return 0
        fi
    done

    local root
    for root in \
        "${ANDROID_NDK_HOME:-}" \
        "${ANDROID_NDK_ROOT:-}" \
        "/opt/homebrew/share/android-commandlinetools/ndk/26.3.11579264" \
        "/opt/homebrew/share/android-ndk" \
        "/opt/homebrew/Caskroom/android-ndk/29/AndroidNDK14206865.app/Contents/NDK"; do
        [ -n "$root" ] && [ -d "$root/toolchains/llvm/prebuilt" ] || continue
        for name in "$@"; do
            local found
            found="$(find "$root/toolchains/llvm/prebuilt" -maxdepth 3 \( -type f -o -type l \) -name "$name" -print -quit 2>/dev/null || true)"
            if [ -n "$found" ] && [ -x "$found" ]; then
                printf '%s\n' "$found"
                return 0
            fi
        done
    done

    return 1
}

readelf_bin="$(find_tool "${READELF:-}" readelf llvm-readelf)" || {
    echo "readelf/llvm-readelf not found; set READELF=/path/to/llvm-readelf" >&2
    exit 1
}
nm_bin="$(find_tool "${NM:-}" nm llvm-nm)" || {
    echo "nm/llvm-nm not found; set NM=/path/to/llvm-nm" >&2
    exit 1
}

python3 - "$protected" "$symbol_elf" "$funcs" "$readelf_bin" "$nm_bin" <<'PY'
import re
import struct
import subprocess
import sys

protected, symbol_elf, funcs, readelf_bin, nm_bin = sys.argv[1:6]
func_names = [name.strip() for name in funcs.split(",") if name.strip()]

phdr_text = subprocess.check_output([readelf_bin, "-l", protected], text=True)
load_count = len(re.findall(r"^\s+LOAD\s+", phdr_text, flags=re.MULTILINE))
if load_count < 5:
    raise SystemExit(f"VMP audit failed: expected injected LOAD segment, got {load_count} LOAD segments")

nm_text = subprocess.check_output([nm_bin, "-S", symbol_elf], text=True)
symbols = {}
for line in nm_text.splitlines():
    parts = line.split()
    if len(parts) >= 4 and parts[2].lower() == "t":
        symbols[parts[3]] = int(parts[0], 16)

missing = [name for name in func_names if name not in symbols]
if missing:
    raise SystemExit("VMP audit failed: missing symbols: " + ", ".join(missing))

readelf_text = subprocess.check_output([readelf_bin, "-l", protected], text=True)
loads = []
lines = readelf_text.splitlines()
for idx, line in enumerate(lines):
    if " LOAD " not in line:
        continue
    nums = re.findall(r"0x[0-9a-fA-F]+", line)
    if len(nums) < 3 or idx + 1 >= len(lines):
        continue
    more = re.findall(r"0x[0-9a-fA-F]+", lines[idx + 1])
    if len(more) < 2:
        continue
    loads.append({
        "off": int(nums[0], 16),
        "vaddr": int(nums[1], 16),
        "filesz": int(more[0], 16),
    })

data = open(protected, "rb").read()
verified = []
for name in func_names:
    addr = symbols[name]
    file_off = None
    for load in loads:
        start = load["vaddr"]
        end = start + load["filesz"]
        if start <= addr + 11 < end:
            file_off = load["off"] + (addr - start)
            break
    if file_off is None:
        raise SystemExit(f"VMP audit failed: cannot map {name} at 0x{addr:x}")

    chunk = data[file_off:file_off + 16]
    if len(chunk) < 12:
        raise SystemExit(f"VMP audit failed: short read for {name}")
    words = struct.unpack("<" + "I" * (len(chunk) // 4), chunk[: (len(chunk) // 4) * 4])

    # VMPackerOLLVM token trampoline shape is either the legacy 3-insn form or
    # the PIE-safe 4-insn form:
    #   ADR  X17, .                    ; runtime function entry / load-bias anchor
    #   MOV  W16, #token_lo16
    #   MOVK W16, #token_hi16, LSL #16 ; random per-function token key accepted
    #   B    vm_entry_token
    if words[0] == 0x10000011 and len(words) >= 4:
        w_adr, w_mov, w_movk, w_b = words[:4]
        is_trampoline = (
            (w_mov & 0xFFE0001F) == 0x52800010
            and (w_movk & 0xFFE0001F) == 0x72A00010
            and (w_b & 0xFC000000) == 0x14000000
        )
        shown = f"{w_adr:08x} {w_mov:08x} {w_movk:08x} {w_b:08x}"
    else:
        w_mov, w_movk, w_b = words[:3]
        is_trampoline = (
            (w_mov & 0xFFE0001F) == 0x52800010
            and (w_movk & 0xFFE0001F) == 0x72A00010
            and (w_b & 0xFC000000) == 0x14000000
        )
        shown = f"{w_mov:08x} {w_movk:08x} {w_b:08x}"
    if not is_trampoline:
        raise SystemExit(
            f"VMP audit failed: {name} is not a token trampoline "
            f"at 0x{addr:x}: {shown}"
        )
    verified.append(name)

print("VMP_AUDIT: PASS funcs=" + ",".join(verified) + f" load_segments={load_count}")
PY
