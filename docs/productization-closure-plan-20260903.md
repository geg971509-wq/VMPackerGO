# VMPackerGO productization closure plan

Date: 2026-09-03

This document records the requested complete execution chain: initial repair plan, plan audit, corrected final plan, implementation order, verification gates, and merge policy.

## 1. Initial repair plan

1. Establish `main` from the only existing verified branch and perform all work on an isolated productization branch.
2. Close runtime semantic-integrity defects before expanding the ARM64 whitelist:
   - stack/evaluation-stack bounds;
   - malformed bytecode and dispatch failures;
   - nested packed-call depth and callee-load failures;
   - descriptor/trailer validation;
   - one explicit fail-closed fault path.
3. Remove fixed VM stack and call-depth assumptions that silently change legal program behavior.
4. Generalize external tail transfers instead of keeping a compiler-outliner-only product special case.
5. Finish exception/unwind runtime integration and final ELF unwind publication.
6. Finish far-branch veneer planning before mutation.
7. Expand entry ABI coverage only after the control-flow/runtime boundary is sound.
8. Add architecture capability and differential verification matrices.
9. Make physical-device evidence, 85-demo execution, 4 KiB/16 KiB loading, signing, notarization, provenance, and checksums executable release gates.
10. Normalize repository branch, module, CI, release, and living documentation state.

## 2. Audit of the initial plan

The initial plan was reviewed against correctness, closure, maintainability, over-design risk, and available evidence.

### Accepted

- Runtime semantic integrity must precede instruction-count growth.
- Plan-first ELF rewriting remains the architectural authority; the writer must not recompute layout.
- Unsupported semantics remain deterministic pack-time rejection.
- Physical-device and signing requirements remain release evidence, not host-test claims.
- The current tri-state ARM64 policy (`virtual`, `native thunk`, `reject`) is retained and strengthened rather than replaced.

### Corrected

- Increasing fixed stack/call-depth constants is rejected as a false fix. Resource growth must be bounded, explicit, and faulted.
- Returning integer zero on an internal runtime failure is rejected because zero may be a valid protected-function result.
- General external tail calls must become a first-class semantic path; exact-r29 outliner inlining may remain only as a validated optimization.
- A permanently failing release script is not a final gate. It must eventually consume machine-readable evidence and fail for specific missing evidence.
- Historical handoff text must not compete with the product contract as a second source of truth.

### Deferred by evidence, not by design

The following cannot be truthfully marked PASS without the required external evidence, but all code paths and gates must remain explicit:

- physical Android device execution and contention evidence;
- 4 KiB and 16 KiB device coverage;
- Developer ID signing and Apple notarization;
- independent release review.

Missing evidence keeps release closed; it does not justify weakening the product contract.

## 3. Corrected final repair plan

### Stage A — repository baseline and governance

- Create `main` at the current verified HEAD.
- Create an isolated repair branch.
- Keep `Verification` active for pull requests and `main` pushes.
- Merge only an exact reviewed head SHA.

### Stage B — runtime fail-closed integrity

- Define typed runtime fault bits and one fatal completion policy.
- Make evaluation-stack overflow/underflow set a fault.
- Make VM stack bounds failures set a fault rather than skip memory semantics.
- Make malformed instruction sizes, PC movement, missing handlers, descriptor/trailer failures, call-depth exhaustion, and callee-load failures set a fault.
- Add static translation stack-effect verification where bytecode construction can prove the invariant.
- Add regression tests that demonstrate faults cannot be observed as normal zero returns.

### Stage C — stack and packed-call lifecycle

- Replace fixed in-struct VM memory stack with separately allocated guarded/bounded stack storage.
- Replace fixed in-struct packed-call frames with bounded dynamic frame storage.
- Preserve one architectural SP across protected-to-protected calls.
- Ensure all root, nested, tail-switch, failure, and unwind paths release exactly the owned mappings.
- Add recursion, mutual-recursion, depth/resource, and cleanup tests.

### Stage D — control-flow completeness

- Introduce first-class native/packed external tail transfer handling.
- Keep arbitrary non-tail external `B` fail-closed.
- Retain exact-r29 outliner validation only as a bounded optimization.
- Complete B/BL reach analysis and immutable veneer-island planning before writing.
- Add near/far, packed/native, PIE/ET_EXEC, BTI/PAC, and ASLR tests.

### Stage E — exception and unwind closure

- Consume prepared exception bridge routes in runtime generation.
- Generate invoke/personality/landing-pad assembly with complete CFI.
- Merge runtime and generated FDE/LSDA data.
- rebuild `.eh_frame_hdr` and update `PT_GNU_EH_FRAME` in the immutable plan.
- Keep any unsupported exception topology fail-closed.
- Add host structural tests and require physical-device unwinder evidence before release.

### Stage F — ABI and instruction capability

- Add FP scalar, vector, HFA/HVA, aggregate, sret/X8, stack-argument, and return-class entry support in bounded increments.
- Keep variadic entry unsupported until a complete type/ABI contract exists.
- Make an architecture capability matrix the source of truth; compiler and real-world corpora prove the matrix instead of defining it.
- Add Android-relevant ARMv8 extension profiles and explicit deterministic rejection for unsupported profiles.

### Stage G — verification and release closure

- Execute the exact 85-demo manifest as build → baseline run → pack → transformed run → differential comparison.
- Add machine-readable device evidence for API, ABI, page size, BTI/PAC, CPU features, exit/signal/output, side effects, unwind, and contention.
- Add parser/planner/writer/unwind fuzz targets and adversarial ELF mutation fixtures.
- Add aggregate memory/output expansion budgets.
- Replace placeholder module identity with the canonical repository module path.
- Pin the Go toolchain used for releases.
- Add signed/notarized macOS ARM64 release automation, source archive, provenance, and `SHA256SUMS`.

## 4. Execution and merge policy

- Each stage must add focused regression coverage before broad verification.
- Required hosted verification is: contract checks, `go list`, unit tests, race tests, exact-r29 FP/SIMD corpus, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI build.
- No stage may convert a semantic failure into a warning or normal return.
- No release-ready claim is permitted while external evidence gates are absent.
- The final pull request is merged into `main` only with an exact expected head SHA and green required checks.
