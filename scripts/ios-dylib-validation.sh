#!/usr/bin/env bash
set -euo pipefail

# This gate intentionally runs only on macOS. Apple's clang/ld64 are needed to
# produce a real arm64 iPhoneOS MH_DYLIB; a synthetic byte fixture is not a
# substitute for exercising the command-line packer against a linker output.
if [[ "$(uname -s)" != "Darwin" ]]; then
  printf '%s\n' 'iOS dylib validation skipped: Apple toolchain is only available on macOS.'
  exit 0
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v go >/dev/null
command -v xcrun >/dev/null
command -v nm >/dev/null
command -v otool >/dev/null
command -v codesign >/dev/null

sdk="$(xcrun --sdk iphoneos --show-sdk-path)"
clang="$(xcrun --sdk iphoneos --find clang)"
[[ -d "$sdk" && -x "$clang" ]] || {
  printf '%s\n' 'iOS dylib validation requires an installed iPhoneOS SDK and clang.' >&2
  exit 1
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/vmpacker-ios-dylib.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fixture="$root/testdata/ios/dylib_fixture.c"
[[ -f "$fixture" ]] || { printf 'missing iOS fixture: %s\n' "$fixture" >&2; exit 1; }

# Keep the fixture deliberately position-independent and free of external
# references. The implementation currently rejects PC-relative/branch
# relocation cases; this fixture proves the supported direct-entry lane against
# an actual Apple linker output and leaves those harder cases fail-closed.
# iOS 12 is intentional: Apple enables chained fixups for newer deployment
# targets, while current ld_prime removed the historical -no_chained_fixups
# switch. Production inputs with chained fixups remain rejected until the
# Mach-O writer updates their segment metadata.
"$clang" \
  -target arm64-apple-ios12.0 \
  -isysroot "$sdk" \
  -dynamiclib \
  -O2 \
  -fvisibility=hidden \
  -fno-stack-protector \
  -fno-unwind-tables \
  -fno-asynchronous-unwind-tables \
  -Wl,-headerpad_max_install_names \
  -Wl,-no_compact_unwind \
  -Wl,-exported_symbol,_vmp_fixture_add \
  -o "$tmp/libvmp_fixture.dylib" \
  "$fixture"

file_info="$(otool -hv "$tmp/libvmp_fixture.dylib")"
grep -Eq 'ARM64|arm64' <<<"$file_info"
grep -Eq 'MH_DYLIB|DYLIB' <<<"$file_info"
nm -arch arm64 -gU "$tmp/libvmp_fixture.dylib" | grep -Eq '[[:space:]]_vmp_fixture_add$'

go run ./cmd/vmpacker \
  -mode ios \
  -func _vmp_fixture_add \
  -abi 'i32(i32,i32)' \
  -o "$tmp/libvmp_fixture.packed.dylib" \
  -report "$tmp/report.json" \
  "$tmp/libvmp_fixture.dylib"

python3 - "$tmp/report.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as f:
    report = json.load(f)
assert report["schema_version"] == 1
assert report["status"] == "ok", report
assert report["target_kind"] == "ios-dylib", report
assert report["runtime_strategy"] == "ios-arm64-relocated-entry", report
assert report["release_ready"] is False, report
PY

packed_info="$(otool -hv "$tmp/libvmp_fixture.packed.dylib")"
grep -Eq 'ARM64|arm64' <<<"$packed_info"
grep -Eq 'MH_DYLIB|DYLIB' <<<"$packed_info"
otool -l "$tmp/libvmp_fixture.packed.dylib" | grep -A4 -E 'segname __VMPACK|__VMPACK' >/dev/null
nm -arch arm64 -gU "$tmp/libvmp_fixture.packed.dylib" | grep -Eq '[[:space:]]_vmp_fixture_add$'

# The packer deliberately invalidates any old signature. Ad-hoc signing is
# enough to prove the rewritten load-command table is consumable by Apple's
# signing tooling; production builds still require the containing app's real
# distribution identity and entitlements.
codesign --force --sign - --timestamp=none "$tmp/libvmp_fixture.packed.dylib"
codesign --verify --strict "$tmp/libvmp_fixture.packed.dylib"

printf '%s\n' 'iOS arm64 MH_DYLIB fixture pack/re-sign validation passed.'
