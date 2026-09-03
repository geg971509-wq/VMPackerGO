# Release evidence schema v1

Release evidence is generated only after the exact release checkout has passed host and physical-device gates. It binds the release tag, commit, device evidence, signed/notarized binary, corresponding source archive, checksums, and an independent review.

The canonical validator is `scripts/validate-release-evidence.py`.

```json
{
  "schema_version": 1,
  "tag": "v1.2.3",
  "commit_sha": "40 lowercase hex",
  "go_version": "go1.26.0",
  "ndk_revision": "29.0.14206865",
  "device_evidence": {
    "file": "device-evidence.json",
    "sha256": "64 lowercase hex"
  },
  "artifact": {
    "file": "vmpacker-darwin-arm64",
    "sha256": "64 lowercase hex"
  },
  "source": {
    "file": "vmpacker-v1.2.3-source.tar.gz",
    "sha256": "64 lowercase hex"
  },
  "signing": {
    "kind": "Developer ID Application",
    "hardened_runtime": true,
    "timestamped": true,
    "codesign_valid": true
  },
  "notarization": {
    "status": "Accepted",
    "submission_id": "non-empty Apple notary submission identifier",
    "stapled": true,
    "spctl_accepted": true
  },
  "independent_review": {
    "reviewer": "non-empty reviewer identity",
    "reviewed_commit": "same commit_sha",
    "result": "approved"
  }
}
```

Rules:

- `tag` must be a clean SemVer release tag and must point exactly at `commit_sha`.
- `commit_sha` must be the current clean checkout HEAD.
- `go_version` is the exact `.go-version` compiler, currently `go1.26.0`.
- `ndk_revision` must be exactly `29.0.14206865`.
- all three referenced files are basenames located beside the release-evidence JSON; absolute paths are rejected.
- hashes are recomputed by the validator; recorded strings are not trusted.
- the device evidence must independently pass `validate-device-evidence.py` for the same commit.
- the artifact must be a macOS ARM64 Mach-O and its live codesign/spctl state is rechecked on macOS; JSON booleans cannot substitute for those checks.
- notarization status must be `Accepted`; the release packager writes the submission identifier returned by Apple.
- independent review is deliberately not produced by the build script. A distinct reviewer must approve the exact commit after inspecting the candidate and evidence.

Credentials, notary profiles, raw device identifiers, seeds and encryption keys are never fields in release evidence.
