# Physical Android device evidence schema v1

Release evidence is an external fact. This schema records physical-device executions; it does not permit host or emulator results to satisfy a release gate.

The canonical validator is `scripts/validate-device-evidence.py`.

## Root object

```json
{
  "schema_version": 1,
  "commit_sha": "40 lowercase hexadecimal characters",
  "ndk_revision": "29.0.14206865",
  "manifest_sha256": "sha256(demo/manifest.json)",
  "devices": [],
  "demo_runs": [],
  "coverage_runs": []
}
```

`commit_sha` must equal the exact checkout being certified. `manifest_sha256` prevents an evidence file from silently being reused after the approved 85-demo inventory changes.

## Device record

Each device has:

- `id_hash`: 64-hex SHA-256 pseudonymous identifier; never store a raw serial.
- `physical`: must be `true`.
- `abi`: must be `arm64-v8a`.
- `api`: integer, at least 23.
- `page_size`: exactly `4096` or `16384`.
- `bti`: boolean indicating usable BTI capability for the recorded image/device path.
- `pac`: boolean indicating usable PAC capability for the recorded image/device path.

The release matrix requires an API-23 device, at least one later API device, both 4 KiB and 16 KiB page-size devices, and recorded BTI/PAC-capable coverage.

## Execution result

Each baseline/packed result is:

```json
{
  "exit_code": 0,
  "signal": null,
  "stdout_sha256": "64 hex",
  "stderr_sha256": "64 hex",
  "side_effect_sha256": "64 hex"
}
```

`signal` is `null` or a non-empty signal name. Hashes are produced after the harness's documented deterministic normalization (for example CRLF normalization); normalization must be identical for baseline and packed execution. `side_effect_sha256` hashes a deterministic side-effect bundle, even when that bundle is empty.

For ordinary demo and semantic-coverage executions, equivalence alone is not sufficient: every baseline/packed attempt must be identical, exit with code `0`, and have `signal: null`. A pair of identical crashes or identical non-zero exits is not passing release evidence.

The sole inverse case is `malformed_reject`. It records deterministic rejection: baseline/packed rejection metadata must match, every attempt must have a non-zero exit code and `signal: null`, and the run must not carry any success-coverage tag.

## Demo run

A demo run contains:

```json
{
  "demo_id": "demo_insn_add",
  "device_id": "<device id_hash>",
  "attempts": [
    {"baseline": {"...": "..."}, "packed": {"...": "..."}}
  ]
}
```

At least three attempts are required. Every attempt must have byte-for-byte equivalent normalized baseline and packed result metadata and must complete successfully. Every ID in `demo/manifest.json` must have a passing run on both a 4 KiB and a 16 KiB physical device.

## Coverage run

Coverage runs use the same `device_id` and `attempts` structure plus:

```json
{
  "case_id": "fixture-unwind",
  "tags": ["shared_object", "dynamic_load", "exception_throw"],
  "attempts": []
}
```

Across all passing coverage runs the evidence must contain these tags:

- `shared_object`, `pie`, `et_exec`;
- `dynamic_load`, `aslr`;
- `bti`, `pac`;
- `atomics_contention`;
- `exception_throw`, `exception_catch`, `exception_destructor`, `exception_rethrow`;
- `malformed_reject`.

Tags within one run must be unique. A coverage run tagged `atomics_contention` additionally records `threads >= 2` and `iterations >= 1`; its executions must succeed and match baseline/packed behavior. A `malformed_reject` run must contain only that tag and records deterministic non-zero rejection rather than successful execution.

## Security/privacy

Evidence files must not contain:

- raw device serials;
- NDK or home-directory absolute paths;
- opcode maps, seeds or encryption keys;
- signing credentials or Apple authentication material.

The release gate accepts only evidence that passes the canonical validator against the exact release checkout.
