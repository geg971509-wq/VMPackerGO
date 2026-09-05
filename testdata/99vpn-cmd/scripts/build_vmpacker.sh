#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "usage: build_vmpacker.sh <VMPackerOLLVM-dir> [make-target] [vmpacker-bin]" >&2
}

vmpacker_dir="${1:-}"
make_target="${2:-packer-omvll}"
vmpacker_bin="${3:-}"

if [ -z "$vmpacker_dir" ]; then
    usage
    exit 2
fi
if [ -z "$vmpacker_bin" ]; then
    vmpacker_bin="$vmpacker_dir/build/vmpacker.exe"
fi

if [ ! -d "$vmpacker_dir" ]; then
    echo "VMPackerOLLVM directory not found: $vmpacker_dir" >&2
    exit 1
fi
if [ ! -f "$vmpacker_dir/Makefile" ]; then
    echo "VMPackerOLLVM Makefile not found: $vmpacker_dir/Makefile" >&2
    exit 1
fi

# Build the requested packer target in the external OLLVM tree. The default
# target is `packer-omvll`, which rebuilds the O-MVLL-obfuscated VM stub and
# embeds it into build/vmpacker.exe.
make -C "$vmpacker_dir" "$make_target"

if [ ! -x "$vmpacker_bin" ]; then
    echo "VMPacker binary not found or not executable after $make_target: $vmpacker_bin" >&2
    exit 1
fi

blob="$vmpacker_dir/cmd/vmpacker/vm_interp.bin"
if [ ! -s "$blob" ]; then
    echo "Embedded VM stub blob missing after $make_target: $blob" >&2
    exit 1
fi

blob_size="$(wc -c < "$blob" | tr -d '[:space:]')"
# The plain stub is ~13 KiB in this workspace; the current O-MVLL stub is ~41 KiB.
# Keep the threshold conservative to catch accidental fallback to the plain stub.
if [ "${CMD_REQUIRE_OMVLL_STUB:-1}" != "0" ] && [ "$blob_size" -lt 30000 ]; then
    echo "VM stub blob looks like the plain, non-O-MVLL build: $blob_size bytes ($blob)" >&2
    echo "Expected the O-MVLL stub from make $make_target; set CMD_REQUIRE_OMVLL_STUB=0 only for debugging." >&2
    exit 1
fi

printf '[+] using VMPacker: %s\n' "$vmpacker_bin"
printf '[+] embedded VM stub: %s bytes (%s)\n' "$blob_size" "$blob"
