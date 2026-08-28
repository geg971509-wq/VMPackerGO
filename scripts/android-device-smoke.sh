#!/usr/bin/env bash
set -euo pipefail

CHECK_ONLY=0
if [[ "${1:-}" == "--check-only" ]]; then
  CHECK_ONLY=1
fi

if ! command -v adb >/dev/null 2>&1; then
  echo "[!] adb not found"
  exit 1
fi

adb get-state >/dev/null
ABI="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
SDK="$(adb shell getprop ro.build.version.sdk | tr -d '\r')"
ID="$(adb shell id | tr -d '\r')"
SU="$(adb shell 'su -c id 2>/dev/null || true' | tr -d '\r')"

echo "[+] device ABI: ${ABI}"
echo "[+] Android SDK: ${SDK}"
echo "[+] adb shell: ${ID}"
if [[ -n "${SU}" ]]; then
  echo "[+] su: ${SU}"
else
  echo "[!] su unavailable from adb shell"
fi

if [[ "${ABI}" != "arm64-v8a" ]]; then
  echo "[!] expected arm64-v8a device"
  exit 1
fi

if [[ "${CHECK_ONLY}" == "1" ]]; then
  exit 0
fi

echo "[+] smoke target ready; run the independent AArch64 native fixture checks."
