# Pack Report Schema Version 1

`SPDX-License-Identifier: AGPL-3.0-only`

## Status

This document defines the stable report emitted by the development CLI. A report accurately describes the current host transformation, but it is not release evidence. `release_ready` remains `false` while physical-device, signing/notarization, and independent-review gates are incomplete.

A report is one JSON object with `schema_version: 1`. Consumers must reject unsupported schema versions while tolerating additional fields.

## Top-level fields

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `schema_version` | integer | yes | Exactly `1`. |
| `tool` | object | yes | Git-injected `version` and `commit`; defaults are `dev` and `unknown`. |
| `input` | string | yes | Input path text exactly as supplied by the user. |
| `output` | string | yes | Requested output path text exactly as supplied by the user, or the documented default. |
| `mode` | string | yes | `auto`, `so`, `native`, or `ios`. |
| `target_kind` | string | on classified runs | `android-so`, `android-pie`, `android-exec`, or `ios-dylib`. |
| `development_strategy` | string | on classified runs | Last completed internal boundary: `rewrite-artifact-ready` on success, or `rewrite-plan-ready` when final application/reparse fails after planning. |
| `opcode_map_digest` | string | after map creation | Lower-case SHA-256 of the semantic-to-wire byte sequence. This is the only allowed one-way opcode-map derivative. |
| `runtime_strategy` | string | after runtime validation | Accurate runtime build/validation strategy, currently `ndk-r29-et-rel-validated` for Android or `ios-arm64-relocated-entry` for the limited iOS lane. |
| `segment_strategy` | string | reserved | Reserved for a stable final segment-layout reporting contract; intentionally omitted by the current host writer. |
| `veneer_strategy` | string | reserved | Reserved for the final far-branch veneer contract; intentionally omitted until veneer-island integration exists. |
| `unwind_strategy` | string | reserved | Reserved for the final unwind integration contract; intentionally omitted until FDE/LSDA publication exists. |
| `functions` | array | yes | Per-function selection, ABI, and transformation facts; never `null`. |
| `output_sha256` | string | on success | Lower-case SHA-256 of the exact artifact bytes. |
| `status` | string | yes | `ok` or `failed`. |
| `error` | string | on failure | Failure text. |
| `release_ready` | boolean | yes | `false` until all external release gates pass. |
| `limitations` | array | yes | Current development limitations; never `null`. |

Reports never contain seed values, the raw opcode map, NDK paths, home-directory paths added by the tool, temporary paths, encryption keys, signing credentials, or secret configuration. The one-way `opcode_map_digest` is explicitly allowed. Raw input/output path text is preserved even when it contains relative components.

The current host writer validates PHDR placement internally but does not promote that implementation detail into `segment_strategy`. The three reserved strategy fields stay absent rather than publishing unstable values that consumers could mistake for a finalized contract.

## Function object

Each function contains:

- `source`: `direct` or `manifest`.
- `selector`: the raw selector text.
- optional `name`, `address`, or `range` selector fields.
- `abi`: `params` and `result` using `i8`, `u8`, `i16`, `u16`, `i32`, `u32`, `i64`, `u64`, `ptr`, and result-only `void`.
- successful transformation facts: normalized `address`/`range`, optional `section` and `symbol_source` (`symtab` or `dynsym`), `size`, `instructions`, `translated`, and final `bytecode_bytes`.

The report does not include original function bytes. The opcode-map digest cannot reconstruct the seed or map and is included only after the map is created.

## Path, publication, and failure behavior

Input, output, report, and debug-map paths are pairwise distinct after clean/absolute resolution and existing-file identity checks. Parent directories must already exist. Default publication is no-clobber. `-force` uses same-directory temporary files and atomic rename on Darwin. To keep rollback unambiguous, one invocation rejects `-force` when more than one destination already exists.

All destination temporary files are prepared first. Debug-map and report outputs publish before the artifact, which is the success marker. Newly published files are removed if a later publication fails. The implementation does not claim a filesystem-wide transaction.

A failed transform never publishes an artifact or debug map. When `-report` was requested and its destination is safe, the CLI may publish a failure report.

## Example

```json
{
  "schema_version": 1,
  "tool": {"version": "dev", "commit": "unknown"},
  "input": "libdemo.so",
  "output": "libdemo.vmp.so",
  "mode": "so",
  "target_kind": "android-so",
  "development_strategy": "rewrite-artifact-ready",
  "opcode_map_digest": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "runtime_strategy": "ndk-r29-et-rel-validated",
  "functions": [],
  "output_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "status": "ok",
  "release_ready": false,
  "limitations": ["physical-device and release evidence gates remain external"]
}
```
