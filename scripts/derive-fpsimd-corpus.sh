#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'usage: %s <exact-ndk-r29-root>\n' "$0" >&2
  exit 2
fi

ndk="$1"
expected='29.0.14206865'
properties="$ndk/source.properties"
[[ -f "$properties" ]] || { printf 'exact Android NDK r29 is required\n' >&2; exit 1; }
revision="$(awk -F= '$1 ~ /^[[:space:]]*Pkg.Revision[[:space:]]*$/ { gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit }' "$properties")"
[[ "$revision" == "$expected" ]] || { printf 'exact Android NDK revision %s is required\n' "$expected" >&2; exit 1; }

host="${ANDROID_HOST_TAG:-darwin-x86_64}"
if [[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 && -d "$ndk/toolchains/llvm/prebuilt/darwin-arm64" ]]; then
  host=darwin-arm64
fi
bin="$ndk/toolchains/llvm/prebuilt/$host/bin"
clang="$bin/aarch64-linux-android23-clang"
objdump="$bin/llvm-objdump"
[[ -x "$clang" && -x "$objdump" ]] || { printf 'required absolute r29 tools are unavailable\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

source_file='internal/corpus/testdata/fpsimd_r29.c'
[[ -f "$source_file" ]] || { printf 'FP/SIMD corpus source is missing\n' >&2; exit 1; }

records="$work/records.tsv"
: >"$records"
for optimization in O0 O2 Oz; do
  object="$work/fpsimd-$optimization.o"
  listing="$work/fpsimd-$optimization.txt"
  "$clang" -c -std=c17 -fPIC -ffreestanding -fno-builtin \
    -fno-stack-protector -march=armv8-a -"$optimization" \
    -o "$object" "$source_file"
  "$objdump" -d "$object" >"$listing"

  awk -v opt="$optimization" '
    /^[[:space:]]*[[:xdigit:]]+:/ {
      raw=$2
      mnemonic=$3
      if (raw !~ /^[[:xdigit:]]{8}$/ || mnemonic == "") next
      operands=""
      for (i=4; i<=NF; i++) operands=operands (i == 4 ? "" : " ") $i
      if (mnemonic ~ /^(f(add|sub|mul|div|neg|abs|cmp|cvt|mov)|movi|scvtf|ucvtf|fcvt[azmnpu]?s|add|sub|mul|and|orr|eor|mvn|not|ldr|str)$/ &&
          (mnemonic ~ /^f/ || mnemonic ~ /cvtf$/ || operands ~ /(^|[,{[:space:]])[bhsdqv][0-9]/)) {
        print opt "\t" raw "\t" mnemonic "\t" operands
      }
    }
  ' "$listing" >>"$records"
done

for optimization in O0 O2 Oz; do
  awk -F '\t' -v opt="$optimization" '$1 == opt { found=1 } END { exit found ? 0 : 1 }' "$records" || {
    printf 'no FP/SIMD instructions were derived for -%s\n' "$optimization" >&2
    exit 1
  }
done

printf 'optimization\traw\tmnemonic\toperands\n'
LC_ALL=C sort -u -k1,1 -k2,2 "$records"
