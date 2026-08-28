#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_DIR="${1:-$ROOT/build/android}"
PACKER="${PACKER:-$ROOT/build/vmpacker}"
REMOTE_DIR="${ANDROID_REMOTE_DIR:-/data/local/tmp/vmpacker-arm64}"
EXPECTED="${ANDROID_NATIVE_EXPECTED:-calc(321)=13340 calc(654)=1365}"
INPUT_NAME="${ANDROID_NATIVE_INPUT_NAME:-native_bin}"
INPUT="$BUILD_DIR/native_bin/$INPUT_NAME"
OUTPUT="$BUILD_DIR/native_bin/$INPUT_NAME.vmp"

[[ -x "$PACKER" || -f "$PACKER" ]] || { echo "[!] packer not found: $PACKER" >&2; exit 1; }
[[ -f "$INPUT" ]] || { echo "[!] fixture missing: $INPUT" >&2; exit 1; }
[[ "$INPUT_NAME" != */* && "$INPUT_NAME" != *"'"* ]] || { echo "[!] unsafe ANDROID_NATIVE_INPUT_NAME: $INPUT_NAME" >&2; exit 1; }
command -v adb >/dev/null 2>&1 || { echo "[!] adb not found" >&2; exit 1; }

"$PACKER" -mode native -func protected_calc -abi 'i32(i32)' \
  -debug-map "$OUTPUT.debug.txt" -report "$OUTPUT.report.json" \
  -o "$OUTPUT" "$INPUT" > "$BUILD_DIR/native_bin/$INPUT_NAME.pack.log"

adb shell "rm -rf '$REMOTE_DIR' && mkdir -p '$REMOTE_DIR'"
adb push "$INPUT" "$REMOTE_DIR/$INPUT_NAME" >/dev/null
adb push "$OUTPUT" "$REMOTE_DIR/$INPUT_NAME.vmp" >/dev/null
adb shell "chmod 755 '$REMOTE_DIR/$INPUT_NAME' '$REMOTE_DIR/$INPUT_NAME.vmp'"
adb shell "cd '$REMOTE_DIR' && './$INPUT_NAME'" | tee "$BUILD_DIR/native_bin/$INPUT_NAME.device-baseline.log"
adb shell "cd '$REMOTE_DIR' && './$INPUT_NAME.vmp'" | tee "$BUILD_DIR/native_bin/$INPUT_NAME.device-packed.log"
grep -q "$EXPECTED" "$BUILD_DIR/native_bin/$INPUT_NAME.device-baseline.log"
grep -q "$EXPECTED" "$BUILD_DIR/native_bin/$INPUT_NAME.device-packed.log"
echo "[+] Android native smoke passed: $EXPECTED"
