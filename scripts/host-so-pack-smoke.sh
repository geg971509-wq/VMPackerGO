#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_DIR="${1:-$ROOT/build/android}"
PACKER="${PACKER:-$ROOT/dist/vmpacker}"
INPUT_SO="${HOST_SO_INPUT:-$BUILD_DIR/so_jni/libnative_demo.so}"
OUTPUT_SO="${HOST_SO_OUTPUT:-$BUILD_DIR/so_jni/libnative_demo.mac.vmp.so}"
REPORT="${HOST_SO_REPORT:-$OUTPUT_SO.report.json}"
FUNC="${HOST_SO_FUNC:-Java_com_example_demo_NativeBridge_checkLicense}"

[[ -x "$PACKER" ]] || { echo "[!] standalone packer not executable: $PACKER" >&2; exit 1; }
[[ -f "$INPUT_SO" ]] || { echo "[!] input .so missing: $INPUT_SO" >&2; exit 1; }

rm -f "$OUTPUT_SO" "$REPORT" "$OUTPUT_SO.debug.txt"
"$PACKER" \
  -mode so \
  -report "$REPORT" \
  -func "$FUNC" \
  -abi 'i32(ptr,ptr,i32)' \
  -debug-map "$OUTPUT_SO.debug.txt" \
  -o "$OUTPUT_SO" \
  "$INPUT_SO"

[[ -f "$OUTPUT_SO" ]] || { echo "[!] output .so not produced: $OUTPUT_SO" >&2; exit 1; }
[[ -s "$OUTPUT_SO" ]] || { echo "[!] output .so is empty: $OUTPUT_SO" >&2; exit 1; }
python3 - "$REPORT" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as f:
    report = json.load(f)
assert report["status"] == "ok", report
assert report["target_kind"] == "android-so", report
assert report["schema_version"] == 1, report
assert report["release_ready"] is False, report
assert report["development_strategy"] in ("note", "add-segment"), report
print(f"[+] host .so pack report ok: {path}")
PY

echo "[+] host .so pack smoke passed: $OUTPUT_SO"

