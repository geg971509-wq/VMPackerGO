#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${1:-$ROOT/build/android}"
ANDROID_API="${ANDROID_API:-23}"
ANDROID_NDK="${ANDROID_NDK:-${ANDROID_NDK_HOME:-${NDK_HOME:-}}}"

if [[ -z "$ANDROID_NDK" ]]; then
  for candidate in \
    "$HOME/Library/Android/sdk/ndk/current" \
    /opt/homebrew/Caskroom/android-ndk/*/AndroidNDK*.app/Contents/NDK \
    /usr/local/Caskroom/android-ndk/*/AndroidNDK*.app/Contents/NDK; do
    if [[ -d "$candidate/toolchains/llvm/prebuilt" ]]; then
      ANDROID_NDK="$candidate"
      break
    fi
  done
fi

HOST_TAG="${ANDROID_HOST_TAG:-darwin-x86_64}"
if [[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" && -d "$ANDROID_NDK/toolchains/llvm/prebuilt/darwin-arm64" ]]; then
  HOST_TAG="darwin-arm64"
fi
TC="$ANDROID_NDK/toolchains/llvm/prebuilt/$HOST_TAG/bin"
CC="$TC/aarch64-linux-android${ANDROID_API}-clang"

[[ -x "$CC" ]] || { echo "[!] Android clang not found: $CC" >&2; exit 1; }

mkdir -p "$OUT_DIR/so_jni" "$OUT_DIR/native_bin"
"$CC" -shared -fPIC -O0 -g -Wl,--build-id \
  -o "$OUT_DIR/so_jni/libnative_demo.so" \
  "$ROOT/testdata/android/so_jni/native_demo.c"
python3 "$ROOT/scripts/make_elf_no_note.py" \
  "$OUT_DIR/so_jni/libnative_demo.so" \
  "$OUT_DIR/so_jni/libnative_demo.nonote.so"
"$CC" -O0 -g -fPIE -pie \
  -o "$OUT_DIR/so_jni/runner" \
  "$ROOT/testdata/android/so_jni/runner.c" -ldl
"$CC" -O0 -g -fPIE -pie -Wl,--build-id \
  -o "$OUT_DIR/native_bin/native_bin" \
  "$ROOT/testdata/android/native_bin/native_bin.c"
python3 "$ROOT/scripts/make_elf_no_note.py" \
  "$OUT_DIR/native_bin/native_bin" \
  "$OUT_DIR/native_bin/native_bin.nonote"

echo "[+] Android fixtures built under $OUT_DIR"
