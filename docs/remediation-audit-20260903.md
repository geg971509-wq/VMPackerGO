# Remaining-gap remediation audit

Date: 2026-09-03

This document is the authoritative execution record for the second-pass audit requested after the first productization closure branch. Each previously open item was re-evaluated before implementation; an item was not implemented merely because it appeared in an earlier brainstorm.

## 1. Necessity audit

| Item | Decision | Reason |
| --- | --- | --- |
| Runtime silent faults / stack / nested-call resource handling | MUST FIX | Correctness defect: failures could previously become valid-looking results. |
| Temporary self-modifying workflows | MUST FIX | They are development scaffolding and can break the canonical verification contract. |
| Canonical public Go module path | MUST FIX | Explicit release blocker in the product contract and required for maintainable public imports. |
| Exact release Go toolchain | MUST FIX | Release reproducibility requires a concrete compiler, separate from the source-language baseline. |
| Historical handoff in repository root | MUST FIX | It contains stale limits and old repository identity; it must be retained only as an archive snapshot. |
| Far transformed-entry transfer / veneer planning | MUST FIX | Direct imm26 failure alone does not satisfy the approved transformed-entry reach contract. |
| C++ exception / unwind bridge | MUST FIX | The contract requires explicit throw/catch/destructor semantics; metadata parsing alone is insufficient. |
| Protected-entry FP/vector/aggregate/variadic ABI expansion | NOT A CURRENT REPAIR | The active contract intentionally limits protected entry ABI. A stripped ELF generally has no trustworthy function type information, so automatic expansion would require guessing. A future explicit ABI manifest is a product feature, not a correctness repair. |
| Architecture capability matrix | MUST FIX AS DOCUMENTED POLICY | The existing tri-state policy remains implementation truth; compiler corpora prove rather than define support. Full SVE/SVE2/SME implementation is not required. |
| Physical Android device automation and evidence schema | MUST FIX AUTOMATION; EXTERNAL EVIDENCE REQUIRED | The code path and validator can be completed in-repo. Real device results cannot be fabricated. |
| Exact 85-demo build/pack/run/compare runner | MUST FIX AUTOMATION; EXTERNAL EVIDENCE REQUIRED | Inventory-only validation does not prove execution semantics. |
| Differential AArch64 runtime testing | MUST FIX HARNESS; EXTERNAL EVIDENCE REQUIRED | High-risk flags/FP/atomics/unwind behavior needs native comparison. |
| Parser/planner/unwind fuzz seeds | MUST FIX | Cheap robustness coverage for hostile/malformed ELF inputs. |
| Aggregate output/memory budget | MUST FIX | Per-function limits alone do not bound whole-pack resource expansion. |
| Evidence-driven release gate | MUST FIX | A permanently failing gate is a placeholder, not a release contract. Missing evidence must fail for explicit reasons. |
| Apple signing/notarization | MUST FIX PIPELINE; EXTERNAL CREDENTIALS REQUIRED | Automation can be complete, but acceptance cannot be asserted without real credentials and Apple response. |
| Default branch / merge / branch cleanup | MUST FIX LAST | Governance is finalized only after the exact candidate is green and `main` passes its post-merge Verification. |
| Packed descriptor O(n) lookup | DO NOT CHANGE FOR THIS CLOSURE | The lookup is bounded by the approved 4096-function limit. An extra runtime index is a performance optimization, not a correctness/release fix. |
| Decrypted bytecode cache | DO NOT FIX NOW | Performance optimization, not correctness or release closure; caching decrypted protected code increases lifetime/attack surface and complexity. |

## 2. Initial execution plan

1. Restore a clean canonical PR verification baseline and remove one-shot development workflows.
2. Canonicalize module identity, release toolchain and historical documentation placement.
3. Add living support/release/device evidence documentation and machine-readable validation.
4. Add 85-demo/differential device harness entry points and fuzz/resource-budget gates.
5. Implement immutable transformed-entry far-transfer planning before writer mutation.
6. Complete the host exception/unwind bridge and keep physical unwinder proof as a hard release requirement.
7. Run full hosted verification on the exact candidate.
8. Synchronize current-truth documentation and re-run the same Verification on the exact documentation head.
9. Merge by expected head SHA only; verify the resulting `main` push, make `main` canonical, then remove obsolete branches.

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
- A generic external native AArch64 tail `B` must **not** be approximated as native call plus protected return. That changes LR/backtrace/unwind observations and interacts incorrectly with the shadow-stack return path. Only selected packed tails and explicitly validated compiler-outliner helpers remain supported; other native direct or indirect external tails fail closed.
- The product contract's bounded protected-entry ABI remains a deliberate release boundary, not an unfinished implementation task. Without explicit type metadata, expanding it would require guessing.
- A distant runtime segment is not automatically a valid veneer island. The final far-entry design uses a bounded inline `ADRP+ADD+BR` sequence at the selected entry and rejects outside ADRP range or when patch space is insufficient.

## 4. Final repair plan and completion criteria

A code item is complete only when the exact branch head passes the normal hosted Verification workflow. An external-evidence item is complete in-repo when its harness, schema and validator are implemented and tested; release remains blocked until real evidence is supplied.

The branch may be merged only when:

- no temporary patch workflow remains;
- contract checks, evidence/demo validators, exact toolchain/NDK checks, `go list`, unit tests, race tests, exact-r29 corpora, exact-r29 runtime build, vet and macOS ARM64 CLI build all pass on the exact candidate;
- remaining external evidence is represented as explicit release blockers rather than ambiguous TODO text;
- current support documentation matches code behavior;
- the merge uses the exact verified head SHA.

## 5. Executed remediation

Implemented in the candidate branch:

- runtime fault completion is fail-closed after owned mappings are released;
- SP memory and eval-stack faults no longer silently change results;
- the architectural shadow stack is a separate guarded 8 MiB mapping;
- protected-call frames grow dynamically to a bounded resource limit;
- packed callee loading and trailer/source-map validation are transactional and strict;
- selected protected-to-protected direct tails switch VM context without adding call depth;
- generic native external tails were re-audited and intentionally restored to fail-closed instead of keeping a non-equivalent call/return approximation;
- near entry transfers retain `B imm26`; out-of-range entries use a plan-time inline `ADRP X17 + ADD X17 + BR X17` veneer when within ADRP range and when the selected function has 20 bytes (24 with preserved BTI); farther/shorter cases reject deterministically;
- `vm_entry_token` uses a BTI landing compatible with supported near and long entry forms;
- generated runtime unwind FDEs are incorporated into a supported GNU unwind index; exception-bearing protection rejects when a discoverable `PT_GNU_EH_FRAME` path is unavailable;
- canonical Go module identity is `github.com/geg971509-wq/VMPackerGO`; release compiler is pinned by `.go-version`/`toolchain go1.26.0`;
- the stale historical handoff is archived rather than serving as a second current contract;
- a living support matrix and an exhaustive decoder-opcode→tri-state-policy test define the instruction support boundary;
- all 85 demo IDs have an explicit device-case specification; the Go fixture uses a `c-shared` exported AAPCS64 entry rather than guessing Go ABIInternal;
- physical-device qualification, 85-demo differential execution, semantic coverage, evidence merge and strict evidence validation harnesses are present;
- device evidence is tied to exact commit and manifest hashes and requires 4 KiB/16 KiB physical coverage, repeated baseline/packed equality, atomics contention and C++ exception/destructor/rethrow cases;
- parser/decoder/unwind fuzz seed tests are present;
- aggregate rewrite expansion is bounded to 1 GiB and the final file endpoint to 2 GiB in addition to existing per-function limits;
- the release gate is evidence-driven instead of permanently failing; signed/notarized standalone macOS ARM64 packaging, exact source/checksums and explicit independent-review recording are automated, while real device/Apple/reviewer evidence remains external.

Intentionally not implemented because the necessity audit rejected them as current repairs:

- automatic FP/vector/aggregate/variadic protected-entry ABI inference;
- full SVE/SVE2/SME emulation;
- a decrypted-bytecode cache;
- an O(log n) packed-descriptor index.

## 6. Verification checkpoint

The code-complete merge candidate `da3170d88986c2947d862e82ab8b80b6e8cc87fb` passed hosted Verification run `33770578681` end-to-end, including evidence/contract self-tests, Go 1.26, exact NDK r29, `go list`, full tests, race tests, both exact-r29 corpora, exact-r29 runtime build, vet, and macOS ARM64 CLI build.

Current-truth documentation was synchronized after that green code checkpoint. **The exact final documentation head must pass the same Verification workflow before merge.** After that hosted pass, the only remaining release blockers are external evidence gates: physical Android execution evidence, Apple Developer ID/notarization evidence, and distinct independent release approval. Those are deliberately enforced by the release contract and are not considered unfixed source-code points.
