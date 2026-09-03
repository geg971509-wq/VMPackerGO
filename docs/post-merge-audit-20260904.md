# Post-merge product-release audit

Date: 2026-09-04

This document records the product-release audit after the main productization closure was merged. It separates required corrections from intentionally unsupported or external-evidence boundaries and records the corrected execution plan. Historical phase/closure documents remain historical records rather than the current product contract.

## 1. Necessity audit

| Finding | Decision | Reason |
| --- | --- | --- |
| `-info` validates Android NDK state before read-only inspection | MUST FIX | Information mode does not build a runtime and must remain usable without an installed/configured NDK. |
| Device evidence accepts baseline/packed pairs that are equal but both failed | MUST FIX | Behavioral equality is insufficient for normal demo/coverage cases; a paired failure must not become release evidence. |
| Malformed-input evidence can be mixed with success coverage | MUST FIX | Rejection evidence has the opposite expected outcome and must be isolated from successful semantic coverage. |
| Release evidence tag/file validation is permissive | MUST FIX | Release evidence must bind to one exact SemVer tag and real sibling files; malformed prerelease identifiers or symlink indirection weaken provenance. |
| Release packaging deletes an existing output directory | MUST FIX | Release tooling must never destructively clean an arbitrary caller-selected path. Staging must be atomic and non-destructive. |
| Device demo/coverage matrices fail when rerun over their own packed artifacts | MUST FIX | Verification harnesses must be replayable; their own previous outputs must not create false failures. |
| Public shell entry points are committed without executable Git modes | MUST FIX | `make android-device-check` and the documented direct release command must work from a clean checkout; CI must prevent mode regression. |
| Release/device Make targets exist but are absent from `make help` | FIX | Release-facing command discovery should describe the supported targets instead of hiding them. |
| Complete FP/vector/aggregate/variadic protected-entry inference | NOT A REPAIR | Current contract intentionally requires explicit bounded entry ABI metadata; guessing stripped-binary types would weaken correctness. |
| Generic native external tail transfer | NOT A REPAIR | Current product deliberately fails closed because call+return is not LR/PAC/unwind equivalent to a real tail branch. |
| O(log n) token lookup / decrypted bytecode cache | DEFER | Performance-only; bounded O(n) lookup is within the 4096-function product limit, while caching decrypted code increases complexity and lifetime. |
| Physical Android results, Developer ID/notarization, independent approval | EXTERNAL EVIDENCE | The repository provides harnesses and strict validators. These facts cannot be manufactured in source code. |

## 2. Initial repair plan

1. Make `-info` independent of NDK validation and add focused regression coverage.
2. Require successful executions for ordinary demo/semantic evidence and deterministic non-zero rejection for isolated malformed-input cases.
3. Harden release evidence tag grammar, sibling-file constraints and associated negative tests.
4. Make release packaging stage into a private sibling directory and refuse to overwrite an existing destination.
5. Make both physical-device matrices safely rerunnable without weakening the packer's default no-clobber behavior.
6. Restore executable Git modes for active shell entry points and enforce that contract in CI.
7. Synchronize the evidence/release documentation and Make target discovery with the actual behavior.
8. Run the canonical Verification workflow on the exact PR head.
9. Merge only that exact green head into `main`, then require the `main` push Verification to pass.

## 3. Plan audit / consensus

Accepted without a product-direction decision:

- All identified source/tooling findings are correctness, provenance, operability or replayability defects and should be fixed.
- The fixes preserve fail-closed behavior and the existing CLI-only product scope.
- Release packaging must not solve rerun behavior by deleting user-owned output; staging and explicit refusal are safer.
- Malformed-input evidence must be modeled as an expected rejection, never as a generic successful coverage case.
- A verification harness may opt into `-force` only for destinations it creates and owns; the product's default no-clobber contract remains unchanged.
- Script executable modes are part of the public command contract and therefore belong in automated verification.

Rejected alternatives:

- Do not make `-info` silently ignore malformed ELF input; it only bypasses unrelated NDK validation.
- Do not treat equal non-zero demo results as valid differential success.
- Do not loosen no-clobber globally just to make a test harness rerunnable.
- Do not recursively delete caller-selected release paths for convenience.
- Do not split large ELF/runtime files merely to reduce line count while their responsibilities remain domain-cohesive.
- Do not fold optional ABI/ISA expansion, GUI work, or runtime performance work into this correction batch.

## 4. Corrected final plan

The audit branch starts from `main` commit `43d4296b35852cff3e34194eea5c9692a2a063dc`. Completion requires:

- focused regression coverage for NDK-independent info mode;
- device-evidence negative tests for equivalent failures, malformed success and mixed rejection/success tags;
- release-evidence negative tests for SemVer/path/provenance boundaries;
- non-destructive private release staging;
- rerunnable demo/coverage matrices;
- executable-mode contract coverage for active shell entry points;
- documentation synchronized to the enforced evidence and packaging semantics;
- full canonical Verification on the exact PR head;
- exact-head merge into `main` and successful post-merge Verification.

External physical-device, Apple signing/notarization and independent-review evidence remain release gates, not unfinished source-code repairs.

## 5. Scope and organization conclusions

- Active product scope is command-line only. Historical Wails/APK material remains under `archive/` and the contract gate rejects reintroduction into active product paths.
- No active public/third-party library implementation was modified as part of this audit.
- Large ELF-rewrite/runtime files were reviewed as domain units; no split was justified solely by file size.
- No additional active file was confirmed redundant strongly enough to move into `archive/`. In particular, tracked `.omx/` planning/context material is excluded from the product contract but may still serve repository tooling/history, so it is not deleted or relocated without evidence that those consumers are gone.
