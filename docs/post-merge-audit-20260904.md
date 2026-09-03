# Post-merge closure audit

Date: 2026-09-04

This document records the final audit pass after the main productization closure was merged. It separates required corrections from intentionally unsupported or external-evidence boundaries, then records the audited execution plan.

## 1. Necessity audit

| Finding | Decision | Reason |
| --- | --- | --- |
| `-info` validates Android NDK state before read-only inspection | MUST FIX | Information mode does not build a runtime and must remain usable without an installed/configured NDK. |
| Device evidence accepts baseline/packed pairs that are equal but both failed | MUST FIX | Behavioral equality is insufficient for normal demo/coverage cases; a paired failure must not become release evidence. |
| Malformed-input evidence can be mixed with success coverage | MUST FIX | Rejection evidence has opposite expected outcome and must be isolated from successful semantic coverage. |
| Release evidence tag/file validation is permissive | MUST FIX | Release evidence must bind to one exact SemVer tag and safe sibling files; ambiguous paths or malformed prerelease identifiers weaken provenance. |
| Release packaging deletes an existing output directory | MUST FIX | Release tooling must never destructively clean an existing release path. Staging must be atomic/non-destructive. |
| Device demo matrix fails when rerun over an existing packed artifact | MUST FIX | Verification harnesses must be replayable; their own previous outputs must not create false failures. |
| Complete FP/vector/aggregate/variadic protected-entry inference | NOT A REPAIR | Current contract intentionally requires explicit bounded entry ABI metadata; guessing stripped-binary types would weaken correctness. |
| Generic native external tail transfer | NOT A REPAIR | Current product deliberately fails closed because call+return is not LR/PAC/unwind equivalent to a real tail branch. |
| O(log n) token lookup / decrypted bytecode cache | DEFER | Performance-only; bounded O(n) lookup is within the 4096-function product limit, while caching decrypted code increases complexity and lifetime. |
| Physical Android results, Developer ID/notarization, independent approval | EXTERNAL EVIDENCE | The repository provides harnesses and strict validators. These facts cannot be manufactured in source code. |

## 2. Initial repair plan

1. Make `-info` independent of NDK validation and add focused regression coverage.
2. Require successful executions for ordinary demo/semantic evidence and deterministic non-zero rejection for malformed-input cases.
3. Harden release evidence tag grammar, sibling-file constraints and associated negative tests.
4. Make release packaging stage into a private sibling directory and refuse to overwrite an existing destination.
5. Make the device demo matrix safely rerunnable without weakening the packer's default no-clobber behavior.
6. Run the canonical Verification workflow on the exact branch head.
7. Merge only that exact green head into `main`, then require the `main` push Verification to pass.
8. Remove obsolete audit/productization branches only after their unique useful changes are present on `main`.

## 3. Plan audit / consensus

Accepted:

- All six source/tooling findings are correctness, provenance or replayability defects and should be fixed.
- The fixes must preserve fail-closed behavior and the existing product scope.
- Release packaging must not solve rerun behavior by deleting user-owned output; staging and explicit refusal are safer.
- Malformed-input evidence must be modeled as an expected rejection, never as a generic successful coverage case.

Rejected alternatives:

- Do not make `-info` silently ignore malformed ELF input; it only bypasses unrelated NDK validation.
- Do not treat equal non-zero demo results as valid differential success.
- Do not loosen no-clobber globally just to make a test harness rerunnable; the harness explicitly opts into `-force` for its own generated destination.
- Do not fold optional ABI/ISA expansion or runtime performance work into this correction batch.

## 4. Corrected final plan

The final implementation is the seven-commit audit branch beginning from `main` commit `43d4296b35852cff3e34194eea5c9692a2a063dc`, plus this audit record. Completion requires:

- focused tests for NDK-independent info mode;
- device-evidence negative tests for equivalent failures, malformed success and mixed rejection/success tags;
- release-evidence negative tests for tag/path/provenance boundaries;
- non-destructive release staging;
- rerunnable demo-matrix packing;
- full canonical Verification on the exact PR head;
- exact-head merge into `main` and successful post-merge Verification.

External device, signing/notarization and independent-review evidence remain release gates, not unfinished source-code repairs.
