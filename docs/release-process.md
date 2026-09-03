# Release process

VMPackerGO remains development-stage until every release gate below has real evidence. Host tests do not substitute for physical Android execution, Apple notarization, or independent review.

## 1. Prepare the exact candidate

1. Merge only a fully green reviewed change into `main`.
2. Run the normal `Verification` workflow on `main`.
3. Create a clean v-prefixed SemVer tag, for example `v1.2.3`, on the exact verified commit.
4. Check out that tag with no local modifications.
5. Use the exact Go compiler from `.go-version` and Android NDK `29.0.14206865`.

## 2. Collect physical-device evidence

For each physical device:

```sh
scripts/android-device-check.sh --attest-physical --json qualification.json
python3 scripts/run-device-demo-matrix.py \
  --ndk "$ANDROID_NDK_ROOT" \
  --packer ./dist/vmpacker-darwin-arm64 \
  --qualification qualification.json \
  --out demo-evidence.json
python3 scripts/run-device-coverage-matrix.py \
  --ndk "$ANDROID_NDK_ROOT" \
  --packer ./dist/vmpacker-darwin-arm64 \
  --qualification qualification.json \
  --out coverage-evidence.json
```

Run the matrix on the physical devices needed to satisfy API, 4 KiB/16 KiB, BTI/PAC and CPU-feature coverage. Merge the fragments:

```sh
python3 scripts/merge-device-evidence.py --out device-evidence.json *.json
python3 scripts/validate-device-evidence.py device-evidence.json
```

The validator requires all 85 approved demos on both 4 KiB and 16 KiB physical devices, at least three matching baseline/packed executions per demo/device, and the semantic coverage tags defined in `device-evidence-schema-v1.md`.

## 3. Sign and notarize the standalone CLI

Store Apple notary credentials in the macOS Keychain using `notarytool store-credentials`; never put credentials in repository files or evidence JSON. Set only the profile name and signing identity in the environment:

```sh
export VMPACKER_SIGN_IDENTITY='Developer ID Application: ...'
export VMPACKER_NOTARY_PROFILE='vmpacker-notary'
export ANDROID_NDK_ROOT='/path/to/ndk/29.0.14206865'
scripts/package-release.sh device-evidence.json dist/release
```

The packager:

- verifies a clean exact SemVer tag;
- verifies exact Go and NDK revisions;
- validates physical-device evidence;
- builds the macOS ARM64 CLI;
- Developer-ID signs it with hardened runtime and secure timestamp;
- submits a ZIP containing the standalone binary with `notarytool --wait`;
- requires Apple status `Accepted`;
- verifies Gatekeeper acceptance with `spctl`;
- creates the exact tagged source archive and `SHA256SUMS`;
- writes `release-evidence-draft.json`.

Standalone command-line Mach-O binaries receive an Apple notarization ticket, but Apple currently does not support stapling that ticket directly to a standalone binary. The v1 evidence schema therefore records `ticket_mode: "online"` rather than claiming a nonexistent stapled ticket.

## 4. Independent review

A reviewer other than the release build process inspects:

- the exact tagged commit;
- the normal Verification run;
- physical-device evidence and validator output;
- code-signing and notarization result;
- source and binary hashes;
- remaining documented fail-closed product boundaries.

Only after approval may the reviewer record it:

```sh
python3 scripts/record-independent-review.py \
  dist/release/release-evidence-draft.json \
  --reviewer 'reviewer identity' \
  --approve \
  --out dist/release/release-evidence.json
```

The script only records an explicit human approval; it cannot generate or infer one.

## 5. Final release contract

```sh
VMPACKER_RELEASE_EVIDENCE="$PWD/dist/release/release-evidence.json" \
  scripts/check-contract.sh --release
```

The release contract recomputes hashes, validates device evidence against the exact checkout, verifies tag/commit/toolchain metadata, and on macOS rechecks the live Mach-O signing and Gatekeeper state. Missing or stale evidence fails closed with a concrete reason.

Publishing is intentionally outside the validator. A release artifact must never be uploaded before the final contract passes.
