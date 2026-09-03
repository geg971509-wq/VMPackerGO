#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

copy_tree() {
  local destination="$1"
  mkdir -p "$destination"
  tar -C "$ROOT" \
    --exclude='./.git' --exclude='./.omc' --exclude='./.omx' --exclude='./.codex' \
    --exclude='./build' --exclude='./dist' --exclude='./vmpacker' --exclude='*/node_modules' --exclude='*/__pycache__' \
    -cf - . | tar -C "$destination" -xf -
}

run_pass() {
  local name="$1"
  local directory="$2"
  local output="$TMP/$name.out"
  if ! bash "$directory/scripts/check-contract.sh" >"$output" 2>&1; then
    printf 'self-test %s unexpectedly failed:\n' "$name" >&2
    cat "$output" >&2
    exit 1
  fi
}

prepare_case() {
  local directory="$TMP/case"
  rm -rf "$directory"
  copy_tree "$directory"
  git -C "$directory" init -q
  git -C "$directory" add .
}

run_fail() {
  local name="$1"
  local expected="$2"
  local directory="$TMP/case"
  local output="$TMP/$name.out"
  if bash "$directory/scripts/check-contract.sh" >"$output" 2>&1; then
    printf 'self-test %s unexpectedly passed\n' "$name" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" "$output"; then
    printf 'self-test %s missed expected group %s:\n' "$name" "$expected" >&2
    cat "$output" >&2
    exit 1
  fi
}

metadata_free="$TMP/metadata-free"
copy_tree "$metadata_free"
run_pass metadata-free "$metadata_free"

clean="$TMP/clean"
copy_tree "$clean"
git -C "$clean" init -q
git -C "$clean" add .
run_pass clean "$clean"

prepare_case
chmod -x "$TMP/case/scripts/package-release.sh"
run_fail executable-mode "non-executable product shell entry point"

for method in String StringVar Func; do
  for flag_name in apk profile; do
    prepare_case
    case "$method" in
      String)
        printf 'package injected\nimport "flag"\nvar _ = flag.String("%s", "", "")\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
      StringVar)
        printf 'package injected\nimport "flag"\nfunc init() { flag.StringVar(new(string), "%s", "", "") }\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
      Func)
        printf 'package injected\nimport "flag"\nfunc init() { flag.Func("%s", "", func(string) error { return nil }) }\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
    esac
    run_fail "flag-$method-$flag_name" "active $([[ "$flag_name" == profile ]] && printf profile || printf APK/AAB) CLI"
  done
done

for method in String StringVar Func; do
  for flag_name in apk profile; do
    prepare_case
    case "$method" in
      String)
        printf 'package injected\nimport "flag"\nvar _ = flag.NewFlagSet("test", flag.ContinueOnError).String("%s", "", "")\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
      StringVar)
        printf 'package injected\nimport "flag"\nfunc init() { flag.NewFlagSet("test", flag.ContinueOnError).StringVar(new(string), "%s", "", "") }\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
      Func)
        printf 'package injected\nimport "flag"\nfunc init() { flag.NewFlagSet("test", flag.ContinueOnError).Func("%s", "", func(string) error { return nil }) }\n' "$flag_name" >"$TMP/case/contract_injected.go"
        ;;
    esac
    run_fail "flagset-$method-$flag_name" "active $([[ "$flag_name" == profile ]] && printf profile || printf APK/AAB) CLI"
  done
done

prepare_case
printf 'package injected\n// scan until first RET to infer function size\n' >"$TMP/case/contract_injected.go"
run_fail first-ret "first-RET function inference pattern"

for interface_name in Decoder Translator Packer; do
  prepare_case
  printf 'package injected\ntype %s interface { Run() error }\n' "$interface_name" >"$TMP/case/contract_injected.go"
  run_fail "interface-$interface_name" "speculative Decoder/Translator/Packer interface"
done

for pattern in goto label macro dtab; do
  prepare_case
  case "$pattern" in
    goto)
      printf 'void injected(void *target) { goto *target; }\n' >"$TMP/case/contract_injected.c"
      ;;
    label)
      printf 'void injected(void) { void *target = &&done; done: (void)target; }\n' >"$TMP/case/contract_injected.c"
      ;;
    macro)
      printf '#define VM_INDIRECT_DISPATCH 1\n' >"$TMP/case/contract_injected.c"
      ;;
    dtab)
      printf 'static void *dtab[1];\n' >"$TMP/case/contract_injected.c"
      ;;
  esac
  run_fail "computed-goto-$pattern" "computed-goto dispatch pattern"
done

normal_and="$TMP/normal-and"
copy_tree "$normal_and"
printf 'package injected\nfunc both(a, b bool) bool { return a && b }\n' >"$normal_and/contract_injected.go"
git -C "$normal_and" init -q
git -C "$normal_and" add .
run_pass normal-and "$normal_and"

prepare_case
printf 'package missing\n' >"$TMP/case/contract_missing.go"
git -C "$TMP/case" add contract_missing.go
rm "$TMP/case/contract_missing.go"
run_fail scanner-error "scanner error"

prepare_case
printf '{}\n' >"$TMP/case/wails.json"
run_fail wails "active Wails configuration"

prepare_case
mkdir -p "$TMP/case/.github/workflows"
printf 'jobs:\n  build:\n    strategy:\n      matrix:\n        goos: [linux, windows]\n' >"$TMP/case/.github/workflows/injected.yml"
run_fail workflow-goos "Linux/Windows product release pattern"

prepare_case
mkdir -p "$TMP/case/.github/workflows"
printf 'jobs:\n  build:\n    steps:\n      - uses: actions/upload-artifact@v4\n        with:\n          path: app.apk\n' >"$TMP/case/.github/workflows/injected.yml"
run_fail workflow-apk "active APK/AAB release artifact"

printf 'check-contract self-test passed\n'
