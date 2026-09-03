# Productization closure execution log

Date: 2026-09-03

This log records the executed host-side closure. The first-pass plan is retained in `productization-closure-plan-20260903.md`; second-pass necessity decisions and intentionally deferred/non-required work are in `remediation-audit-20260903.md`.

## Repository baseline

- Created `main` and isolated `codex/productization-closure-20260903`.
- Opened pull request 21 against `main`.
- Reconciled later `main` CI drift into the repair branch with a non-forced two-parent merge while preserving the already-audited candidate tree.
- Temporary self-modifying/one-shot workflows used during implementation were removed; the candidate contains one permanent hosted workflow: `.github/workflows/build.yml`.

## Runtime semantic integrity — host verified

Implemented:

- typed, NZCV-independent runtime fault classes;
- fatal post-cleanup fault completion instead of normal integer-zero returns;
- explicit bytecode/control/descriptor/resource/evaluation-stack faults;
- SP-memory faults rather than silent memory-operation skipping;
- 8 MiB separately mapped architectural stack with 16 KiB guard granules;
- dynamically growing, bounded protected-call frame storage;
- transactional nested bytecode decrypt/validate/install;
- strict forward/reverse trailer and source-map validation;
- invalid branch-target rejection rather than safe-looking fall-through;
- bounded vector transfers;
- AArch64 signed-division overflow semantics without C undefined behavior;
- corrected arithmetic-sign CLS semantics.

Earlier exact implementation checkpoints passed hosted Verification runs `33751638537` and related follow-up runs.

## Control flow and transformed-entry reach — host verified

Implemented:

- selected protected-to-protected terminal `B` becomes an explicit VM-to-VM tail switch without growing protected-call depth;
- exact-r29 machine-outliner inlining remains a narrow validated optimization;
- generic native external direct/indirect tails fail closed rather than using a non-equivalent call+return approximation;
- image-relative materialization rejects signed-address overflow;
- near transformed entries use `B imm26`;
- an out-of-range transformed entry can use a plan-time inline `ADRP X17 + ADD X17 + BR X17` veneer when within ADRP range and when the selected function has enough entry bytes (20 bytes, or 24 while preserving entry BTI);
- farther/shorter cases reject deterministically;
- runtime entry BTI accepts the supported branch/jump landing forms.

Focused far-entry and native-tail tests passed before full hosted verification.

## Exception and unwind — structural host closure

Implemented:

- final VM call/landing routes are derived from the translated/reversed bytecode boundaries;
- runtime generation emits personality bridges, invoke wrappers, landing routes, CFI and rebuilt LSDA data;
- runtime FDEs are integrated into a supported GNU unwind index;
- the immutable rewrite plan updates the supported `PT_GNU_EH_FRAME` route;
- exception-bearing protection fails closed when a discoverable `PT_GNU_EH_FRAME` path is unavailable.

Host structural verification does not substitute for physical Android unwinder behavior. Throw/catch/destructor/rethrow device evidence remains a release gate.

## ABI and instruction capability decision

- The protected-entry ABI remains intentionally bounded to explicit integer/pointer metadata; automatic FP/vector/aggregate/variadic inference was rejected as an unsafe binary-type guess, not left as an accidental TODO.
- The ARM64 tri-state product policy remains the implementation authority.
- A hosted test requires every decoder opcode to have an explicit `virtual`, `native thunk`, or `reject` disposition.
- SVE/SVE2/SME are not speculatively emulated; unsupported profiles fail closed.

## 85-demo, device and release automation — in-repository closure

Implemented:

- exact device-case specification for all 85 approved manifest IDs, with zero unresolved cases;
- Go fixture changed to an explicit cgo `c-shared` AAPCS64 export rather than treating Go ABIInternal as AAPCS64;
- physical-device qualification with API/ABI/page-size/emulator checks, BTI/PAC/CPU-feature inventory and hashed device identity;
- per-device 85-demo build → baseline run → pack → transformed run → repeated differential evidence generation;
- semantic coverage runner for shared-object/dynamic loading, PIE/ASLR/BTI/PAC, static `ET_EXEC`, multithreaded LSE contention, C++ exception/destructor/rethrow, and deterministic malformed-input rejection;
- deterministic evidence merge and strict device-evidence validation tied to the exact commit and manifest hash;
- evidence-driven release validation, exact Go/NDK checks, Developer ID/hardened-runtime/timestamp signing, Apple notarization/Gatekeeper validation, exact tagged source, `SHA256SUMS`, and explicit independent-review recording.

No physical-device, signing/notarization or independent-review result is fabricated by these tools. Their real evidence remains external and keeps the product in development status until supplied.

## Robustness and resource closure

- Added decoder, ELF parser and unwind parser fuzz seed tests.
- Added aggregate rewrite budgeting: at most 1 GiB appended rewrite data and a 2 GiB final file endpoint, in addition to the existing 1 GiB input / 4096-function / 256 KiB-per-function limits.
- Canonicalized the public Go module to `github.com/geg971509-wq/VMPackerGO`.
- Pinned the release compiler with `.go-version` and `toolchain go1.26.0` while retaining the source-language baseline in the `go` directive.
- Archived the stale handoff snapshot under `archive/handoffs/`.

## Hosted verification evidence

The code-complete merge candidate `da3170d88986c2947d862e82ab8b80b6e8cc87fb` passed Verification run `33770578681` end-to-end:

- contract and evidence-schema validator self-tests;
- exact Go 1.26 toolchain;
- exact Android NDK `29.0.14206865`;
- `go list ./...`;
- full `go test -count=1 ./...`;
- full race-enabled tests;
- exact-r29 FP/SIMD corpus;
- exact-r29 whole-compiler corpus;
- exact-r29 runtime build/validation;
- `go vet ./...`;
- macOS ARM64 CLI build.

Documentation was synchronized after that code-complete run. The exact final documentation head must pass the same Verification workflow before PR 21 is merged.

## Final boundary

After a final exact-head hosted green run, no known in-repository correctness/productization item from the second-pass audit remains untreated. The remaining release blockers are **external evidence gates**: physical Android device evidence, Apple Developer ID/notarization evidence, and a distinct independent release review. The release contract deliberately rejects missing or stale evidence.
