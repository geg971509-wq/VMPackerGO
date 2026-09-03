#!/usr/bin/env bash
set -euo pipefail

JSON_OUT=""
ATTEST_PHYSICAL=0
while (($#)); do
  case "$1" in
    --json)
      [[ $# -ge 2 ]] || { echo "usage: $0 --attest-physical [--json path]" >&2; exit 2; }
      JSON_OUT="$2"; shift 2 ;;
    --attest-physical)
      ATTEST_PHYSICAL=1; shift ;;
    *)
      echo "usage: $0 --attest-physical [--json path]" >&2; exit 2 ;;
  esac
done

command -v adb >/dev/null 2>&1 || { echo "adb not found" >&2; exit 1; }
(( ATTEST_PHYSICAL == 1 )) || {
  echo "physical-device qualification requires explicit --attest-physical" >&2
  exit 1
}

ADB=(adb)
if [[ -n "${ANDROID_SERIAL:-}" ]]; then
  ADB+=( -s "$ANDROID_SERIAL" )
fi
"${ADB[@]}" get-state >/dev/null

ABI="$("${ADB[@]}" shell getprop ro.product.cpu.abi | tr -d '\r')"
API="$("${ADB[@]}" shell getprop ro.build.version.sdk | tr -d '\r')"
QEMU_KERNEL="$("${ADB[@]}" shell getprop ro.kernel.qemu | tr -d '\r')"
QEMU_BOOT="$("${ADB[@]}" shell getprop ro.boot.qemu | tr -d '\r')"
MODEL="$("${ADB[@]}" shell getprop ro.product.model | tr -d '\r')"
PRODUCT="$("${ADB[@]}" shell getprop ro.product.name | tr -d '\r')"
PAGE_SIZE="$("${ADB[@]}" shell 'getconf PAGESIZE 2>/dev/null || getconf PAGE_SIZE 2>/dev/null || true' | tr -d '\r' | tail -n 1)"
FEATURES="$("${ADB[@]}" shell 'grep -m1 -E "^(Features|flags)[[:space:]]*:" /proc/cpuinfo 2>/dev/null || true' | tr -d '\r' | tr '[:upper:]' '[:lower:]')"
SERIAL="$(${ADB[@]} get-serialno | tr -d '\r')"

[[ "$ABI" == "arm64-v8a" ]] || { echo "expected arm64-v8a; got $ABI" >&2; exit 1; }
[[ "$API" =~ ^[0-9]+$ ]] && (( API >= 23 )) || { echo "Android API 23+ is required" >&2; exit 1; }
[[ "$PAGE_SIZE" == "4096" || "$PAGE_SIZE" == "16384" ]] || {
  echo "device page size must be 4096 or 16384; got ${PAGE_SIZE:-unknown}" >&2; exit 1;
}
if [[ "$QEMU_KERNEL" == "1" || "$QEMU_BOOT" == "1" || "${MODEL,,}" == *emulator* ||
      "${MODEL,,}" == *sdk_gphone* || "${PRODUCT,,}" == *sdk_gphone* || "${PRODUCT,,}" == *emulator* ]]; then
  echo "emulator/virtual-device markers detected; physical evidence is required" >&2
  exit 1
fi

ID_HASH="$(printf '%s' "$SERIAL" | shasum -a 256 | awk '{print $1}')"
BTI=false
PAC=false
if grep -Eq '(^|[[:space:]])bti([[:space:]]|$)' <<<"$FEATURES"; then BTI=true; fi
if grep -Eq '(^|[[:space:]])paca([[:space:]]|$)|(^|[[:space:]])pacg([[:space:]]|$)' <<<"$FEATURES"; then PAC=true; fi

echo "qualified physical Android candidate: abi=$ABI api=$API page_size=$PAGE_SIZE bti=$BTI pac=$PAC id_hash=${ID_HASH:0:12}..."

if [[ -n "$JSON_OUT" ]]; then
  umask 077
  ID_HASH="$ID_HASH" ABI="$ABI" API="$API" PAGE_SIZE="$PAGE_SIZE" BTI="$BTI" PAC="$PAC" \
  python3 - "$JSON_OUT" <<'PY'
import json, os, pathlib, sys
out = pathlib.Path(sys.argv[1])
data = {
    "id_hash": os.environ["ID_HASH"],
    "physical": True,
    "abi": os.environ["ABI"],
    "api": int(os.environ["API"]),
    "page_size": int(os.environ["PAGE_SIZE"]),
    "bti": os.environ["BTI"] == "true",
    "pac": os.environ["PAC"] == "true",
}
out.write_text(json.dumps(data, sort_keys=True, indent=2) + "\n")
out.chmod(0o600)
PY
fi
