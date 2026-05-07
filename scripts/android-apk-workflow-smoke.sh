#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_DIR="${1:-$ROOT/build/android}"
PACKER="${PACKER:-$ROOT/build/vmpacker.exe}"
OUT_DIR="${APK_WORKFLOW_DIR:-$BUILD_DIR/apk-workflow}"
LIB_SO="${APK_WORKFLOW_LIB_SO:-$BUILD_DIR/so_jni/libnative_demo.so}"
PKG="${APK_SMOKE_PACKAGE:-com.vmpacker.smoke}"
ACTIVITY_CLASS="${APK_SMOKE_ACTIVITY_CLASS:-SmokeActivity}"
NATIVE_LIB="${APK_SMOKE_LIBRARY_NAME:-native_demo}"
JNI_METHOD="${APK_SMOKE_JNI_METHOD:-checkLicense}"
EXPECTED_LOG="${APK_SMOKE_EXPECTED_LOG:-check(1234)=29711 check(1111)=19398}"

[[ -f "$LIB_SO" ]] || { echo "[!] missing workflow library: $LIB_SO" >&2; exit 1; }
[[ -x "$PACKER" || -f "$PACKER" ]] || { echo "[!] packer not found: $PACKER" >&2; exit 1; }
command -v adb >/dev/null 2>&1 || { echo "[!] adb not found" >&2; exit 1; }

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# Build an owned baseline APK with the unprotected repository fixture. The
# helper also installs/runs it once, proving the APK scaffold before repacking.
APK_SMOKE_EXPECTED_LOG="$EXPECTED_LOG" \
  scripts/android-build-smoke-apk.sh "$LIB_SO" "$OUT_DIR/baseline"

"$PACKER" \
  -apk "$OUT_DIR/baseline/smoke.apk" \
  -lib "lib${NATIVE_LIB}.so" \
  -func "Java_com_example_demo_NativeBridge_${JNI_METHOD}" \
  -injector auto \
  -profile compat \
  -apk-sign debug \
  -debug \
  -report "$OUT_DIR/protected.apk.report.json" \
  -o "$OUT_DIR/protected.apk"

adb install -r "$OUT_DIR/protected.apk" >/dev/null
adb logcat -c
adb shell am start -W -n "$PKG/$PKG.$ACTIVITY_CLASS" >/dev/null
sleep 1
adb logcat -d | grep 'VMPackerSmoke' | tail -5 | tee "$OUT_DIR/protected.logcat.txt"
grep -q "$EXPECTED_LOG" "$OUT_DIR/protected.logcat.txt"
grep -q '"status": "ok"' "$OUT_DIR/protected.apk.report.json"
echo "[+] APK workflow smoke passed: $OUT_DIR/protected.apk"

