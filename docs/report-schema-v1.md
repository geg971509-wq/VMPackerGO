# Pack Report Schema Version 1

`SPDX-License-Identifier: AGPL-3.0-only`

## Status

This document defines the stable Phase 2 report emitted by the development CLI. A report accurately describes the current transformation, but it is not release evidence. `release_ready` remains `false` while later runtime, CFG, ASLR, ELF-writer, and physical-device gates are incomplete.

A report is one JSON object with `schema_version: 1`. Consumers must reject unsupported schema versions while tolerating additional fields.

## Top-level fields

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `schema_version` | integer | yes | Exactly `1`. |
| `tool` | object | yes | Git-injected `version` and `commit`; defaults are `dev` and `unknown`. |
| `input` | string | yes | Input path text exactly as supplied by the user. |
| `output` | string | yes | Requested output path text exactly as supplied by the user, or the documented default. |
| `mode` | string | yes | `auto`, `so`, or `native`. |
| `target_kind` | string | on classified runs | `android-so`, `android-pie`, or `android-exec`. |
| `development_strategy` | string | on classified runs | Accurate current internal strategy, currently `note` or `add-segment`; this is not a stable product choice. |
| `functions` | array | yes | Per-function selection, ABI, and transformation facts; never `null`. |
| `output_sha256` | string | on success | Lower-case SHA-256 of the exact artifact bytes. |
| `status` | string | yes | `ok` or `failed`. |
| `error` | string | on failure | Failure text. |
| `release_ready` | boolean | yes | `false` during the development phases. |
| `limitations` | array | yes | Current development limitations; never `null`. |

Reports never contain seed values, values derived from seeds, NDK paths, home-directory paths added by the tool, keys, or secret configuration. Raw input/output path text is preserved even when it contains relative components.

## Function object

Each function contains:

- `source`: `direct` or `manifest`.
- `selector`: the raw selector text.
- optional `name`, `address`, or `range` selector fields.
- `abi`: `params` and `result` using `i8`, `u8`, `i16`, `u16`, `i32`, `u32`, `i64`, `u64`, `ptr`, and result-only `void`.
- successful transformation facts: normalized `address`/`range`, optional `section` and `symbol_source` (`symtab` or `dynsym`), `size`, `instructions`, `translated`, and final `bytecode_bytes`.

The report does not include original function bytes. Mapping digests are omitted until the Phase 4 dynamic runtime mapping exists.

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
  "development_strategy": "note",
  "functions": [],
  "output_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "status": "ok",
  "release_ready": false,
  "limitations": ["development runtime and ELF rewriting are not release-ready"]
}
```
