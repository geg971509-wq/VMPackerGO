#!/usr/bin/env bash
set -euo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly ARCH_OUTPUT="$REPO_ROOT/dist/vmpacker-darwin-arm64"
readonly FINAL_OUTPUT="$REPO_ROOT/dist/vmpacker"

die() {
  printf 'build: %s\n' "$*" >&2
  exit 1
}

for command_name in git go make file cmp; do
  command -v "$command_name" >/dev/null 2>&1 ||
    die "required command not found: $command_name"
done

git_root="$(git -C "$REPO_ROOT" rev-parse --show-toplevel 2>/dev/null)" ||
  die "repository metadata is unavailable"
[[ "$git_root" == "$REPO_ROOT" ]] ||
  die "build.sh must remain in the Git repository root"

make -C "$REPO_ROOT" mac-cli

for output in "$ARCH_OUTPUT" "$FINAL_OUTPUT"; do
  [[ -s "$output" ]] || die "expected executable was not produced: $output"
  [[ -x "$output" ]] || die "output is not executable: $output"
done

file_description="$(file -b "$ARCH_OUTPUT")"
[[ "$file_description" == *Mach-O* && "$file_description" == *arm64* ]] ||
  die "unexpected output format for $ARCH_OUTPUT: $file_description"

cmp -s "$ARCH_OUTPUT" "$FINAL_OUTPUT" ||
  die "final executable does not match the architecture-named artifact"

printf 'Built macOS ARM64 executable:\n  %s\n  %s\n' \
  "$ARCH_OUTPUT" "$FINAL_OUTPUT"
