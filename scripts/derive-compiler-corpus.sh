#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

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

source_file='internal/corpus/testdata/compiler_r29.c'
[[ -f "$source_file" ]] || { printf 'compiler coverage corpus source is missing\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"
records="$work/records.tsv"
: >"$records"

compile_profile() {
  local optimization="$1"
  local profile="$2"
  local march="$3"
  local object="$work/compiler-${optimization}-${profile}.o"
  local listing="$work/compiler-${optimization}-${profile}.txt"

  "$clang" -c -std=c11 -fPIC -ffreestanding -fno-builtin -fno-stack-protector \
    -mno-outline-atomics "$march" -"$optimization" \
    -o "$object" "$source_file"
  "$objdump" -d "$object" >"$listing"

  awk -v opt="$optimization" -v profile="$profile" '
    /^[[:space:]]*[[:xdigit:]]+[[:space:]]+<[^>]+>:/ {
      function_name=$2
      sub(/^</, "", function_name)
      sub(/>:$/, "", function_name)
      next
    }
    /^[[:space:]]*[[:xdigit:]]+:/ {
      address=$1
      sub(/:$/, "", address)
      raw=$2
      mnemonic=$3
      if (function_name == "" || address !~ /^[[:xdigit:]]+$/ || raw !~ /^[[:xdigit:]]{8}$/ || mnemonic == "") next
      operands=""
      for (i=4; i<=NF; i++) operands=operands (i == 4 ? "" : " ") $i
      print opt "\t" profile "\t" function_name "\t" address "\t" raw "\t" mnemonic "\t" operands
    }
  ' "$listing" >>"$records"
}

for optimization in O0 O2 Oz; do
  compile_profile "$optimization" base -march=armv8-a
  compile_profile "$optimization" lse -march=armv8.1-a+lse
 done

for optimization in O0 O2 Oz; do
  for profile in base lse; do
    awk -F '\t' -v opt="$optimization" -v profile="$profile" \
      '$1 == opt && $2 == profile { found=1 } END { exit found ? 0 : 1 }' "$records" || {
      printf 'no compiler instructions were derived for -%s profile %s\n' "$optimization" "$profile" >&2
      exit 1
    }
  done
done

printf 'optimization\tprofile\tfunction\taddress\traw\tmnemonic\toperands\n'
cat "$records"
