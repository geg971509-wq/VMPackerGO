#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEVICE_EVIDENCE="${1:-}"
OUT_DIR="${2:-$ROOT/dist/release}"

if [[ -z "$DEVICE_EVIDENCE" ]]; then
  echo "usage: VMPACKER_SIGN_IDENTITY='Developer ID Application: ...' VMPACKER_NOTARY_PROFILE=profile $0 device-evidence.json [out-dir]" >&2
  exit 2
fi
: "${VMPACKER_SIGN_IDENTITY:?VMPACKER_SIGN_IDENTITY is required}"
: "${VMPACKER_NOTARY_PROFILE:?VMPACKER_NOTARY_PROFILE is required}"

[[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]] || {
  echo "release packaging requires macOS ARM64" >&2; exit 1;
}
command -v codesign >/dev/null
command -v spctl >/dev/null
command -v xcrun >/dev/null
command -v ditto >/dev/null

TAG="$(git -C "$ROOT" describe --tags --exact-match HEAD 2>/dev/null || true)"
[[ "$TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]] || {
  echo "release HEAD must have an exact v-prefixed SemVer tag" >&2; exit 1;
}
COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
[[ -z "$(git -C "$ROOT" status --porcelain)" ]] || {
  echo "release checkout must be clean" >&2; exit 1;
}
[[ "$(git -C "$ROOT" rev-list -n 1 "$TAG")" == "$COMMIT" ]] || {
  echo "release tag does not point at HEAD" >&2; exit 1;
}

EXPECTED_GO="go$(tr -d '[:space:]' < "$ROOT/.go-version")"
[[ "$(go env GOVERSION)" == "$EXPECTED_GO" ]] || {
  echo "release requires $EXPECTED_GO; found $(go env GOVERSION)" >&2; exit 1;
}

NDK="${ANDROID_NDK_ROOT:-${ANDROID_NDK_HOME:-${ANDROID_NDK:-${NDK_HOME:-}}}}"
[[ -n "$NDK" && -f "$NDK/source.properties" ]] || {
  echo "exact Android NDK 29.0.14206865 is required in ANDROID_NDK_ROOT/ANDROID_NDK_HOME/ANDROID_NDK/NDK_HOME" >&2; exit 1;
}
REVISION="$(awk -F= '$1 ~ /^[[:space:]]*Pkg.Revision[[:space:]]*$/ { gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit }' "$NDK/source.properties")"
[[ "$REVISION" == "29.0.14206865" ]] || { echo "Android NDK 29.0.14206865 is required" >&2; exit 1; }

python3 "$ROOT/scripts/validate-demo-cases.py"
python3 "$ROOT/scripts/validate-device-evidence.py" "$DEVICE_EVIDENCE" --root "$ROOT"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
"$ROOT/build.sh"
ARTIFACT="$OUT_DIR/vmpacker-darwin-arm64"
cp "$ROOT/dist/vmpacker-darwin-arm64" "$ARTIFACT"
chmod 0755 "$ARTIFACT"

codesign --force --options runtime --timestamp --sign "$VMPACKER_SIGN_IDENTITY" "$ARTIFACT"
codesign --verify --deep --strict --verbose=2 "$ARTIFACT"
CODESIGN_DETAILS="$(codesign -dvv "$ARTIFACT" 2>&1)"
grep -q 'Authority=Developer ID Application' <<<"$CODESIGN_DETAILS" || {
  echo "release artifact is not signed by Developer ID Application" >&2; exit 1;
}
if ! grep -Eq 'Runtime Version|flags=0x10000\(runtime\)' <<<"$CODESIGN_DETAILS"; then
  echo "release artifact does not have hardened runtime" >&2; exit 1
fi
if ! grep -q 'Timestamp=' <<<"$CODESIGN_DETAILS"; then
  echo "release artifact signature is not timestamped" >&2; exit 1
fi

NOTARY_ZIP="$OUT_DIR/notary-submission.zip"
ditto -c -k --keepParent "$ARTIFACT" "$NOTARY_ZIP"
NOTARY_JSON="$OUT_DIR/notary-result.json"
xcrun notarytool submit "$NOTARY_ZIP" --keychain-profile "$VMPACKER_NOTARY_PROFILE" --wait --output-format json > "$NOTARY_JSON"
read -r NOTARY_STATUS NOTARY_ID < <(python3 - "$NOTARY_JSON" <<'PY'
import json, sys
obj=json.load(open(sys.argv[1]))
print(obj.get('status',''), obj.get('id',''))
PY
)
[[ "$NOTARY_STATUS" == "Accepted" && -n "$NOTARY_ID" ]] || {
  echo "Apple notarization was not accepted; inspect $NOTARY_JSON" >&2; exit 1;
}
# Standalone Mach-O binaries receive tickets, but Apple currently does not
# support stapling those tickets directly. Gatekeeper retrieves the ticket online.
spctl --assess --type execute --verbose=2 "$ARTIFACT"

SOURCE="$OUT_DIR/vmpacker-$TAG-source.tar.gz"
git -C "$ROOT" archive --format=tar.gz --prefix="VMPackerGO-${TAG#v}/" "$TAG" > "$SOURCE"
DEVICE_COPY="$OUT_DIR/device-evidence.json"
cp "$DEVICE_EVIDENCE" "$DEVICE_COPY"
chmod 0600 "$DEVICE_COPY"

sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
ARTIFACT_SHA="$(sha256 "$ARTIFACT")"
SOURCE_SHA="$(sha256 "$SOURCE")"
DEVICE_SHA="$(sha256 "$DEVICE_COPY")"

cat > "$OUT_DIR/release-evidence-draft.json" <<EOF
{
  "schema_version": 1,
  "tag": "$TAG",
  "commit_sha": "$COMMIT",
  "go_version": "$EXPECTED_GO",
  "ndk_revision": "29.0.14206865",
  "device_evidence": {"file": "device-evidence.json", "sha256": "$DEVICE_SHA"},
  "artifact": {"file": "vmpacker-darwin-arm64", "sha256": "$ARTIFACT_SHA"},
  "source": {"file": "vmpacker-$TAG-source.tar.gz", "sha256": "$SOURCE_SHA"},
  "signing": {"kind": "Developer ID Application", "hardened_runtime": true, "timestamped": true, "codesign_valid": true},
  "notarization": {"status": "Accepted", "submission_id": "$NOTARY_ID", "ticket_mode": "online", "spctl_accepted": true},
  "independent_review": {"reviewer": "", "reviewed_commit": "$COMMIT", "result": "pending"}
}
EOF
chmod 0600 "$OUT_DIR/release-evidence-draft.json"

(
  cd "$OUT_DIR"
  shasum -a 256 vmpacker-darwin-arm64 "vmpacker-$TAG-source.tar.gz" device-evidence.json > SHA256SUMS
)
rm -f "$NOTARY_ZIP"

echo "release candidate packaged under $OUT_DIR"
echo "independent review remains required; finalize release-evidence-draft.json only after that review"
