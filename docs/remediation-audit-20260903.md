# Remaining-gap remediation audit

Date: 2026-09-03

This document is the authoritative execution record for the second-pass audit requested after the first productization closure branch. Each previously open item is re-evaluated before implementation; an item is not implemented merely because it appeared in an earlier brainstorm.

## 1. Necessity audit

| Item | Decision | Reason |
| --- | --- | --- |
| Runtime silent faults / stack / nested-call resource handling | MUST FIX | Correctness defect: failures could previously become valid-looking results. |
| Temporary self-modifying workflows | MUST FIX | They are development scaffolding and can break the canonical verification contract. |
| Canonical public Go module path | MUST FIX | Explicit release blocker in the product contract and required for maintainable public imports. |
| Exact release Go toolchain | MUST FIX | Release reproducibility requires a concrete compiler, separate from the source-language baseline. |
| Historical handoff in repository root | MUST FIX | It contains stale limits and old repository identity; it must be retained only as an archive snapshot. |
| Far B/BL entry transfer / veneer planning | MUST FIX | The product contract explicitly promises ASLR-safe out-of-range transfer; direct imm26 failure is not sufficient. |
| C++ exception / unwind bridge | MUST FIX | The product contract explicitly promises throw/catch/destructor semantics. Host-only parsing is not closure. |
| Protected-entry FP/vector/aggregate/variadic ABI expansion | NOT A CURRENT REPAIR | The active contract intentionally limits protected entry ABI. A stripped ELF generally has no trustworthy function type information, so automatic expansion would require guessing. A future explicit ABI manifest is a product feature, not a correctness repair. |
| Architecture capability matrix | MUST FIX AS DOCUMENTED POLICY | The existing tri-state policy remains implementation truth; a generated/maintained support matrix is needed so compiler corpora prove rather than define support. Full SVE/SVE2/SME implementation is not required. |
| Physical Android device automation and evidence schema | MUST FIX AUTOMATION; EXTERNAL EVIDENCE REQUIRED | The code path and validator can be completed in-repo. Real device results cannot be fabricated. |
| Exact 85-demo build/pack/run/compare runner | MUST FIX AUTOMATION; EXTERNAL EVIDENCE REQUIRED | Inventory-only validation does not prove execution semantics. |
| Differential AArch64 runtime testing | MUST FIX HARNESS; EXTERNAL EVIDENCE REQUIRED | High-risk flags/FP/atomics/unwind behavior needs native comparison. |
| Parser/planner/unwind fuzz seeds | MUST FIX | Cheap robustness coverage for hostile/malformed ELF inputs. |
| Aggregate output/memory budget | MUST FIX | Per-function limits alone do not bound whole-pack resource expansion. |
| Evidence-driven release gate | MUST FIX | A permanently failing gate is a placeholder, not a release contract. Missing evidence must fail for explicit reasons. |
| Apple signing/notarization | MUST FIX PIPELINE; EXTERNAL CREDENTIALS REQUIRED | Automation can be complete, but acceptance cannot be asserted without real credentials and Apple response. |
| Default branch / merge / branch cleanup | MUST FIX LAST | Governance is finalized only after the exact reviewed head is green. |
| Packed descriptor O(n) lookup | SHOULD FIX IF ORDERING INVARIANT IS PROVABLE | Performance only. Do not add an index structure unless descriptor ordering can remain simple and auditable. |
| Decrypted bytecode cache | DO NOT FIX NOW | Performance optimization, not correctness or release closure; caching decrypted protected code increases lifetime/attack surface and complexity. |

## 2. Initial execution plan

1. Restore a clean canonical PR verification baseline and remove one-shot development workflows.
2. Canonicalize module identity, release toolchain and historical documentation placement.
3. Add living support/release/device evidence documentation and machine-readable validation.
4. Add 85-demo/differential device harness entry points and fuzz/resource-budget gates.
5. Implement immutable far-transfer veneer planning before writer mutation.
6. Complete the host exception/unwind bridge and keep physical unwinder proof as a hard release requirement.
7. Run full hosted verification on the exact final PR merge commit.
8. Merge by expected head SHA only; then set `main` as default, enable canonical protection where the repository plan permits it, verify the `main` push, and delete obsolete branches.

## 3. Plan audit / consensus

Accepted:

- Preserve the existing plan-first ELF writer boundary.
- Preserve fail-closed semantics; no warning-based degradation is allowed.
- Keep protected entry ABI deliberately bounded for this release. Expanding it without explicit type metadata would weaken correctness.
- Do not implement SVE/SVE2/SME merely to claim instruction completeness; unsupported profiles must reject deterministically.
- Do not add a decrypted-bytecode cache during correctness closure.
- Device, signing, notarization and independent-review evidence must remain external facts, not generated placeholders.

Corrected after review:

- Far-transfer handling belongs in the immutable planner, not in the writer and not as a late runtime patch.
- Release readiness must be based on versioned evidence documents tied to an exact commit and artifact digest.
- The 85-demo gate must compare baseline and packed behavior, not merely confirm source-file presence.
- A physical-device qualification script is not itself evidence; evidence must contain execution cases and comparison results.
- The exact Go release toolchain is pinned independently of the `go` language-version directive.

## 4. Final repair plan and completion criteria

A code item is complete only when the exact branch head passes the normal hosted Verification workflow. An external-evidence item is complete in-repo when its harness, schema and validator are implemented and tested; release remains blocked until real evidence is supplied.

The branch may be merged only when:

- no temporary patch workflow remains;
- contract checks, vet, race tests, exact-r29 corpora, runtime build and macOS ARM64 CLI build all pass on the exact PR merge commit;
- remaining external evidence is represented as explicit release blockers rather than ambiguous TODO text;
- current support documentation matches code behavior;
- the merge uses the expected reviewed head SHA.
