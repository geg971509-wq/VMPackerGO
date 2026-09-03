# VMPackerGO productization closure plan

Date: 2026-09-03

This file records the **first-pass** productization plan and the corrections made after the requested second-pass audit. The authoritative final necessity decisions and execution checkpoint are in [`remediation-audit-20260903.md`](remediation-audit-20260903.md). Where this file's original direction conflicts with that second-pass audit, the second-pass decision wins.

## 1. First-pass plan

The original sequence was:

1. establish `main` and work on an isolated productization branch;
2. close runtime silent-failure semantics before expanding instruction coverage;
3. replace fixed VM stack/call-depth assumptions with explicit bounded resources;
4. close external tail-control-flow gaps;
5. finish exception/unwind integration;
6. close transformed-entry branch reach;
7. consider broader protected-entry ABI coverage;
8. formalize ARM64 capabilities and differential verification;
9. make physical-device, 85-demo, signing/notarization, provenance and checksum requirements executable release gates;
10. normalize repository/module/toolchain/CI/documentation state.

The core ordering was accepted: correctness first, plan-first ELF rewriting remains authoritative, unsupported semantics fail closed, and external release facts must not be fabricated.

## 2. Second-pass necessity audit corrections

The second-pass audit rejected several first-pass assumptions that would have widened the product without improving correctness:

- **Generic native external tail transfer is not implemented by call+return.** That approximation changes observable LR/backtrace/unwind behavior and interacts incorrectly with the shadow-stack return path. The final product boundary accepts selected packed tails and explicitly validated compiler-outliner helpers; other native external direct/indirect tails fail closed.
- **Protected-entry FP/vector/aggregate/variadic expansion is not a current repair.** The approved binary contract intentionally requires explicit ABI metadata and accepts at most eight integer/pointer parameters plus `void` or one integer/pointer result. A stripped binary generally does not contain trustworthy function-type metadata, so automatic widening would require guessing.
- **Full SVE/SVE2/SME support is not required.** Every decoded opcode must have an explicit `virtual`, `native thunk`, or `reject` disposition; unsupported architectural profiles reject deterministically.
- **Far transformed entries use a bounded inline veneer rather than a speculative distant veneer island.** Near entries use `B imm26`; when that cannot reach, the immutable planner may emit `ADRP X17 + ADD X17 + BR X17` within ADRP range and only when enough entry bytes exist. Farther/shorter cases reject.
- **A decrypted-bytecode cache is deliberately deferred.** It is a performance feature, increases plaintext lifetime and runtime complexity, and is not needed for correctness/release closure.
- **The bounded O(n) packed-descriptor lookup is retained.** With the approved 4096-function limit, another runtime index is a performance optimization rather than a release correctness fix.

## 3. Final implementation plan

### Stage A — runtime semantic integrity

- typed faults separate from NZCV;
- fatal post-cleanup fault completion;
- explicit bytecode/control/descriptor/resource/eval-stack faults;
- SP-memory faults rather than skipped memory semantics;
- guarded separately mapped architectural shadow stack;
- dynamically bounded protected-call frame storage;
- transactional packed-callee loading and trailer/source-map validation.

### Stage B — control flow and entry reach

- selected protected direct tails switch VM context without growing call depth;
- exact-r29 compiler-outliner helpers remain a narrow validated optimization;
- arbitrary native external tails fail closed;
- transformed entries use direct near branch or bounded plan-time inline long veneer;
- BTI landing behavior remains valid for supported entry-transfer forms.

### Stage C — exception and unwind

- consume prepared final VM exception routes in runtime generation;
- generate personality/invoke/landing/LSDA/CFI artifacts;
- include runtime FDEs in a supported GNU unwind index;
- reject exception-bearing protection when no discoverable `PT_GNU_EH_FRAME` route is available;
- require physical Android throw/catch/destructor/rethrow evidence before release.

### Stage D — capability and robustness

- retain the explicit ARM64 tri-state policy as implementation truth;
- require every decoder opcode to have a product disposition;
- keep exact-r29 compiler/FP-SIMD corpora as evidence rather than the capability definition;
- add parser/decoder/unwind fuzz seeds;
- bound aggregate rewrite expansion and final output endpoint.

### Stage E — device and release evidence

- maintain an exact explicit device-case specification for all 85 manifest IDs;
- expose the Go demo through a real cgo `c-shared` AAPCS64 boundary instead of guessing Go ABIInternal;
- qualify physical devices and record only pseudonymous device identifiers;
- execute baseline → pack → transformed comparisons repeatedly on 4 KiB and 16 KiB physical devices;
- cover shared-object loading, PIE/ASLR/BTI/PAC, `ET_EXEC`, multithreaded atomics and C++ exception/unwind fixtures;
- merge and strictly validate evidence against exact commit and manifest hashes;
- require exact tagged source, `SHA256SUMS`, Developer ID/hardened-runtime/timestamp signing, Apple notarization/Gatekeeper validation and distinct independent review.

### Stage F — repository closure

- canonical module path and exact release Go toolchain;
- historical handoff archived rather than treated as current truth;
- one canonical Verification workflow;
- merge only an exact fully green PR head;
- verify the resulting `main` push before default-branch/obsolete-branch cleanup.

## 4. Completion rule

An in-repository repair is complete only after the exact candidate head passes contract/evidence checks, `go list`, full tests, race tests, exact-r29 FP/SIMD and whole-compiler corpora, exact-r29 runtime compilation, vet, and macOS ARM64 CLI build.

Physical-device executions, Developer ID credentials, Apple notarization acceptance and independent release approval are external evidence, not code TODOs. The repository can implement and validate their harnesses, but the product remains development-stage until real evidence satisfies those gates.
