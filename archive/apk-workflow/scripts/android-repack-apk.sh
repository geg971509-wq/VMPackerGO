#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  cat >&2 <<'USAGE'
Usage: scripts/android-repack-apk.sh input.apk arm64-v8a/libname.so protected-lib.so output.apk

Replaces one APK library entry for authorized local testing. The output is unsigned;
run zipalign/apksigner with your own debug or release key before installation.
USAGE
  exit 2
fi

IN_APK="$1"
APK_LIB_PATH="lib/$2"
PROTECTED_SO="$3"
OUT_APK="$4"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cp "$IN_APK" "$TMP/base.apk"
mkdir -p "$TMP/$(dirname "$APK_LIB_PATH")"
cp "$PROTECTED_SO" "$TMP/$APK_LIB_PATH"
(cd "$TMP" && zip -q -d base.apk "$APK_LIB_PATH" >/dev/null 2>&1 || true)
(cd "$TMP" && zip -q -u base.apk "$APK_LIB_PATH")
cp "$TMP/base.apk" "$OUT_APK"
echo "[+] wrote unsigned APK: $OUT_APK"
echo "[i] next: zipalign -f -p 4 $OUT_APK aligned.apk && apksigner sign --ks <debug.keystore> aligned.apk"
