# VMP Phase 19 — Branchful Exclusive CFG Closure

## 1. Next-stage plan

Goal: remove the exact-NDK-r29 `branchful-exclusive` intentional fail-closed class without weakening the product whitelist or breaking the architectural exclusive monitor.

Phase 18 proved that baseline Clang emits real C11 atomic compare/exchange sequences containing `B.cond`, `CBZ`, or `CBNZ` between load-exclusive and store-exclusive operations. The current translator stops at the first store-exclusive and only permits a linear branch-free body, so those compiler-generated functions fail closed even though their ordinary arithmetic and exclusive instructions are otherwise supported.

The implementation must preserve these invariants:

1. no interpreter memory access may occur between the load-exclusive and the architecturally corresponding store-exclusive / CLREX termination;
2. every copied PC-relative branch must have a proven target in the copied native-thunk CFG, unless an explicit continuation protocol is designed and validated;
3. guest GPRs remain remapped into the bounded X0-X15 thunk bank and no reserved host register is exposed;
4. VM NZCV is restored before native execution and written back after native execution;
5. no broad branch or atomic whitelist is introduced merely to make exact-r29 CI pass;
6. current branch-free exclusive regions remain byte-for-byte behavior compatible.

## 2. Consensus audit

The initial phrase “relocate PC-relative branches” is too imprecise. AArch64 branch immediates are relative to the branch instruction, so a branch whose source and target are copied at unchanged relative positions does not need an immediate rewrite. The real Phase 19 problem is CFG closure:

- the current region scanner terminates at the first store-exclusive;
- real compare/exchange lowering can branch around one store, retry after a failed store status, or terminate an exclusive monitor through `CLREX`;
- therefore a correct region may contain multiple store-exclusive instructions and internal branch targets;
- a copied block is safe only when all control-flow paths from its load-exclusive entry terminate inside a validated closed block and then converge to one native-thunk return boundary, or when a separately designed continuation result is returned to the VM.

The exact-r29 corpus, not a hand-written synthetic example, is the architectural evidence source. Before product changes, Phase 19 will capture the baseline atomic instruction streams emitted by NDK `29.0.14206865` at O0/O2/Oz and audit their branch targets and termination paths.

## 3. Corrected repair plan

### 3.1 Diagnostic proof

Use the existing exact-r29 compiler corpus derivation and collect the complete `base` profile instruction streams for `vmp_atomic8`, `vmp_atomic16`, `vmp_atomic32`, `vmp_atomic64`, and `vmp_atomic128`.

For every exclusive load encountered:

- identify all reachable instructions before monitor termination;
- classify `B.cond`, `CBZ`, `CBNZ` targets as internal or external;
- identify every reachable store-exclusive and `CLREX` termination;
- identify retry back-edges to the load-exclusive entry;
- determine whether all paths rejoin at one post-exclusive continuation.

No product behavior changes are allowed until this evidence is reviewed.

### 3.2 Region representation

Prefer keeping `vm.ExclusiveRegion` content-addressed by exact original instruction words. Do not add mutable process-local labels or numbering.

If the exact-r29 CFG is fully internal and has a single post-region continuation, extend the region to include the entire closed instruction block. Preserve original instruction order and spacing so internal PC-relative branch immediates remain valid after copying.

If any exact-r29 path requires more than one post-region continuation, add an explicit, deterministic exit code / continuation-offset field to the thunk contract rather than guessing. Any such ABI change must be versioned through the content identity or otherwise proven collision-safe.

### 3.3 CFG validator

Replace the current first-store linear scan with a bounded CFG validator for the supported exclusive subset.

The validator must:

- start only at LDXR/LDAXR/LDXP/LDAXP;
- bound the region size and number of visited CFG states;
- reject nested load-exclusive instructions except a proven retry back-edge to the same entry;
- allow only the existing arithmetic/select body plus exact branch operations proven by the compiler corpus;
- require conditional branch targets to be aligned and inside the candidate region;
- reject calls, indirect branches, exception-generating instructions, memory operations other than the exclusive boundaries/CLREX, and PC-relative data accesses;
- require every path to terminate the monitor through a matching STXR/STLXR/STXP/STLXP or CLREX before leaving the native thunk;
- preserve scalar/pair arity, width, address-base, status/data overlap, and remap-capacity validation;
- require all branch register operands to be remappable and ensure the address register is not overwritten.

### 3.4 Thunk planning

Extend `PlanExclusiveThunk` only as needed for the validated CFG:

- remap register fields for CBZ/CBNZ (and TBZ/TBNZ only if exact-r29 evidence requires them);
- do not rewrite a branch immediate when both source and target remain at the same relative positions;
- if the complete copied CFG is not position-isomorphic, re-encode the immediate from validated source/target indices with range checks;
- preserve NZCV bridge and complete guest-register writeback on every thunk return path;
- never return directly from inside the copied raw instruction stream unless all architectural state has first been committed back to `vm_ctx_t`.

### 3.5 Tests

Add focused tests for:

- exact-r29 scalar compare/exchange branch shapes;
- exact-r29 byte/halfword `SUBS(ext)` + conditional branch shapes;
- exact-r29 pair-exclusive 128-bit baseline shapes where support is architecturally closed;
- retry back-edge behavior;
- CLREX failure path if emitted;
- branch target outside the region rejected;
- branch into the middle of an instruction rejected;
- branch to unsupported instruction rejected;
- nested unrelated exclusive load rejected;
- scalar/pair mismatch rejected;
- register remap preserves branch register operands;
- malformed/raw-field mismatch remains fail-closed;
- deterministic content identity and thunk generation.

### 3.6 Phase 18 gate update

Remove only the `branchful-exclusive` intentional boundary classification after exact-r29 proves the affected baseline functions fully close through the real Translator. Keep `casp128` and `machine-outliner` unchanged.

The exact-r29 test must explicitly fail if any `branchful-exclusive` intentional record remains after Phase 19.

## 4. Execution policy

- Implement only compiler-proven branch families needed for the closed CFG.
- Do not fold CASP128 or machine-outliner into this phase.
- Do not support arbitrary branch-bearing native snippets.
- Do not change VM wire opcodes unless the diagnostic proves a multi-continuation exit protocol is unavoidable.
- Temporary diagnostic/repair workflows and scripts must self-delete or be removed before the final diff.
- Every correction must pass focused Go tests/vet before the full PR Verification.

## 5. Exit criteria

- exact-r29 baseline atomic branch CFGs are structurally understood and encoded as tests;
- supported branchful exclusive regions execute entirely in one native thunk without interpreter interruption;
- all internal branch targets are proven and position-correct;
- no stale `branchful-exclusive` intentional expectation remains;
- `casp128` and `machine-outliner` remain explicit fail-closed boundaries;
- full Go tests, race, exact-r29 FP/SIMD corpus, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI all pass on the current PR head/current `main` base;
- only the verified head is squash-merged to `main`;
- after merge, `main` push Verification passes and the repository's current default integration ref is fast-forwarded to the same commit so it remains content-identical to `main`.