#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODE="${1:-}"
if [[ -n "$MODE" && "$MODE" != "--release" ]]; then
  echo "usage: $0 [--release]" >&2
  exit 2
fi

paths=()
if command -v git >/dev/null 2>&1 && git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  while IFS= read -r -d '' path; do
    paths+=("$path")
  done < <(git -C "$ROOT" ls-files --cached --others --exclude-standard -z)
else
  echo "[contract] git metadata unavailable; scanning the filesystem" >&2
  while IFS= read -r -d '' path; do
    paths+=("${path#"$ROOT"/}")
  done < <(find "$ROOT" -type f -print0)
fi

active_files=()
product_files=()
release_files=()
text_files=()
wails_configs=()
for path in "${paths[@]}"; do
  case "$path" in
    archive/*|.git/*|.omc/*|.omx/*|.codex/*|build/*|dist/*|*/node_modules/*|AGENTS.md|scripts/check-contract.sh|scripts/check-contract-test.sh)
      continue
      ;;
  esac

  case "$path" in
    *.go|*.c|*.h|*.sh|*.py|*.yml|*.yaml|*.json|Makefile)
      active_files+=("$ROOT/$path")
      case "$path" in
        demo/*|testdata/*)
          ;;
        *)
          product_files+=("$ROOT/$path")
          ;;
      esac
      case "$path" in
        .github/*|Makefile|scripts/*release*)
          release_files+=("$ROOT/$path")
          ;;
      esac
      ;;
  esac
  case "$path" in
    *.go|*.c|*.h|*.sh|*.py|*.yml|*.yaml|*.json|*.md|Makefile|NOTICE)
      text_files+=("$ROOT/$path")
      ;;
  esac
  case "$path" in
    wails.json|*/wails.json)
      wails_configs+=("$path")
      ;;
  esac
done

if (( ${#active_files[@]} == 0 || ${#text_files[@]} == 0 )); then
  echo "[contract] no active files found under $ROOT" >&2
  exit 2
fi

failures=0
check() {
  local label="$1"
  local pattern="$2"
  shift 2
  local output
  output="$(mktemp)"
  local status=0
  grep -EnI -- "$pattern" "$@" >"$output" || status=$?
  case "$status" in
    0)
      echo "[contract] $label" >&2
      while IFS= read -r line; do
        echo "  $line" >&2
      done <"$output"
      failures=$((failures + 1))
      ;;
    1)
      ;;
    *)
      echo "[contract] scanner error while checking $label" >&2
      while IFS= read -r line; do
        echo "  $line" >&2
      done <"$output"
      failures=$((failures + 1))
      ;;
  esac
  rm -f "$output"
}

check_release_hosts() {
  local output
  output="$(mktemp)"
  local status=0
  grep -EnI -- 'GOOS[=:][[:space:]]*(linux|windows)|runs-on:[[:space:]]*[^#]*(ubuntu|windows)|vmpacker-(linux|windows)|((linux|windows)[[:space:]_-]+release)' "${release_files[@]}" >"$output" || status=$?
  if (( status > 1 )); then
    echo "[contract] scanner error while checking release hosts" >&2
    failures=$((failures + 1))
  fi
  for file in "${release_files[@]}"; do
    case "$file" in
      *.yml|*.yaml)
        awk '
          /^[[:space:]]*goos[[:space:]]*:/ {
            base = match($0, /[^ ]/) - 1
            in_goos = 1
            if (tolower($0) ~ /\[[^]]*(linux|windows)/) print FILENAME ":" FNR ":" $0
            next
          }
          in_goos {
            if ($0 ~ /^[[:space:]]*($|#)/) next
            current = match($0, /[^ ]/) - 1
            if (current <= base) { in_goos = 0; next }
            if (tolower($0) ~ /^[[:space:]]*-[[:space:]]*(linux|windows)([[:space:]#]|$)/) print FILENAME ":" FNR ":" $0
          }
        ' "$file" >>"$output"
        ;;
    esac
  done
  if [[ -s "$output" ]]; then
    echo "[contract] Linux/Windows product release pattern" >&2
    while IFS= read -r line; do echo "  $line" >&2; done <"$output"
    failures=$((failures + 1))
  fi
  rm -f "$output"
}

check_release_artifacts() {
  local output file
  output="$(mktemp)"
  for file in "${release_files[@]}"; do
    awk '
      /^[[:space:]]*#/ { next }
      {
        lower = tolower($0)
        if (lower ~ /\.(apk|aab)([^[:alnum:]_]|$)/ || lower ~ /name:[[:space:]]*["'\'' ]*(apk|aab)([^[:alnum:]_]|$)/ || lower ~ /name:[[:space:]]*[^#]*[^[:alnum:]_](apk|aab)([^[:alnum:]_]|$)/)
          print FILENAME ":" FNR ":" $0
      }
    ' "$file" >>"$output"
  done
  if [[ -s "$output" ]]; then
    echo "[contract] active APK/AAB release artifact" >&2
    while IFS= read -r line; do echo "  $line" >&2; done <"$output"
    failures=$((failures + 1))
  fi
  rm -f "$output"
}

check "active APK/AAB CLI/import pattern" 'github\.com/vmpacker/.*/apkpack|apkpack\.|\.(String|StringVar|Func)[[:space:]]*\([^[:cntrl:]]*["`](apk|aab|lib|apk-sign|aab-sign)["`]|(^|[[:space:]])-(apk|aab)([[:space:]=]|$)|-(apk|aab)-sign' "${active_files[@]}"
check "active profile CLI/type/report pattern" '\.(String|StringVar|Func)[[:space:]]*\([^[:cntrl:]]*["`]profile["`]|(^|[^[:alnum:]_])-profile([^[:alnum:]_]|$)|ProfileKind|Profile(Compat|Balanced|Strong)|json:[[:space:]]*"profile"' "${active_files[@]}"
check "removed Phase 2 CLI flag declaration" '\.(String|StringVar|Bool|BoolVar|Func)[[:space:]]*\([^[:cntrl:]]*["`](target|android-mode|injector|token|debug)["`]' "${active_files[@]}"
check "speculative Decoder/Translator/Packer interface" 'type[[:space:]]+(Decoder|Translator|Packer)[[:space:]]+interface([[:space:]]|\{|$)' "${product_files[@]}"
check "computed-goto dispatch pattern" 'goto[[:space:]]+\*|(^|[=({,])[[:space:]]*&&[[:space:]]*[[:alpha:]_][[:alnum:]_]*|VM_INDIRECT_DISPATCH|(^|[^[:alnum:]_])dtab([^[:alnum:]_]|$)' "${product_files[@]}"
check "first-RET function inference pattern" 'cannot detect function size[^[:cntrl:]]*no RET|扫描到 RET|scan[^[:cntrl:]]*(first|until)[^[:cntrl:]]*RET' "${product_files[@]}"
check "active GUI build/reference" 'vmp-gui|GUI_DIR|gui-(windows|linux)|name:[[:space:]]*GUI|Build GUI|wails([[:space:]/.]|$)|wailsjsdir|frontend:(install|build)|make[[:space:]]+gui|^gui[[:space:]]*:' "${active_files[@]}"
if (( ${#release_files[@]} > 0 )); then
  check_release_hosts
  check_release_artifacts
fi
check "sync-public pattern" 'sync-public' "${text_files[@]}"
check "commercial-license offer" 'Commercial Licensing|contact[^.]*commercial license|commercial-license offer available|商业许可[^。]*(联系|获取)|闭源商用[^。]*(联系|获取)' "${text_files[@]}"
check "restrictive assent text" 'By downloading[^.]*agreed|read and agreed to the above|下载[^。]*(即表示|代表)[^。]*同意' "${text_files[@]}"
check "removed fixed runtime pipeline" 'go:embed[[:space:]]+vm_interp\.bin|cmd/vmpacker/vm_interp\.bin|scripts/make_stub_blob\.py|STUB_DIR[[:space:]]*=.*stub/linux/arm64|InterpBlob|parseRuntimeBlob|runtimeBlob' "${active_files[@]}"

if (( ${#wails_configs[@]} > 0 )); then
  echo "[contract] active Wails configuration" >&2
  for path in "${wails_configs[@]}"; do
    echo "  $path" >&2
  done
  failures=$((failures + 1))
fi

if [[ "$MODE" == "--release" ]]; then
  if [[ -z "${VMPACKER_RELEASE_EVIDENCE:-}" ]]; then
    echo "[contract] release evidence is required in VMPACKER_RELEASE_EVIDENCE" >&2
    failures=$((failures + 1))
  elif ! python3 "$ROOT/scripts/validate-release-evidence.py" "$VMPACKER_RELEASE_EVIDENCE" --root "$ROOT"; then
    echo "[contract] release evidence validation failed" >&2
    failures=$((failures + 1))
  fi
fi

if (( failures > 0 )); then
  echo "[contract] failed with $failures prohibited pattern group(s)" >&2
  exit 1
fi

echo "[contract] active product contract passed${MODE:+ ($MODE)}"
