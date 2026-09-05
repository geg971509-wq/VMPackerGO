#!/usr/bin/env bash
set -euo pipefail

vmpacker="${1:?usage: vmp_cmd.sh <vmpacker> <input> <output> <funcs>}"
input="${2:?usage: vmp_cmd.sh <vmpacker> <input> <output> <funcs>}"
output="${3:?usage: vmp_cmd.sh <vmpacker> <input> <output> <funcs>}"
funcs="${4:-main}"

if [ ! -x "$vmpacker" ]; then
    echo "VMPacker binary not found or not executable: $vmpacker" >&2
    exit 1
fi

if [ ! -f "$input" ]; then
    echo "cmd build output not found: $input" >&2
    exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/cmd-vmp.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

vm_output="$tmp_dir/cmd.vmp"
"$vmpacker" -func "$funcs" -o "$vm_output" "$input"

if [ ! -s "$vm_output" ]; then
    echo "VMPacker did not create output: $vm_output" >&2
    exit 1
fi

cp -f "$vm_output" "$output"
chmod 755 "$output"

"$(dirname "${BASH_SOURCE[0]}")/audit_vmp_cmd.sh" "$output" "$input" "$funcs"
