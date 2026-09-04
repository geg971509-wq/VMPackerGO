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
6. existing branch-free regions must retain their shortest raw region identity and product semantics.

## 2. Consensus audit

The initial phrase “relocate PC-relative branches” is too imprecise. AArch64 branch immediates are relative to the branch instruction, so a branch whose source and target are copied at unchanged relative positions does not need an immediate rewrite. The real Phase 19 problem is CFG closure:

- the current region scanner terminates at the first store-exclusive;
- real compare/exchange lowering can branch around one store, retry after a failed store status, or terminate an exclusive monitor through `CLREX`;
- therefore a correct region may contain multiple store-exclusive instructions and internal branch targets;
- a copied block is safe only when all control-flow paths from its load-exclusive entry terminate inside a validated closed block and then converge to one native-thunk return boundary, or when a separately designed continuation result is returned to the VM.

The exact-r29 corpus, not a hand-written synthetic example, is the architectural evidence source. Before product changes, Phase 19 captures the baseline atomic instruction streams emitted by NDK `29.0.14206865` at O0/O2/Oz and audits their branch targets and termination paths.

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

Keep `vm.ExclusiveRegion` content-addressed by exact original instruction words. Do not add mutable process-local labels or numbering.

When the exact-r29 CFG is fully internal and has a single post-region continuation, extend the region to include the shortest complete closed instruction block. Preserve original instruction order and spacing so internal PC-relative branch immediates remain valid after copying.

A new continuation ABI is not introduced because the exact-r29 evidence shows a single post-region continuation for the relevant micro-CFGs.

### 3.3 CFG validator

Replace the current first-store linear scan with a bounded CFG validator for the supported exclusive subset.

The validator must:

- start only at LDXR/LDAXR/LDXP/LDAXP;
- bound the region to the existing maximum instruction count;
- reject nested load-exclusive instructions; retry is represented as a branch back to the same region entry rather than a second copied load;
- allow only the existing arithmetic/select body plus `B`, `B.cond`, `CBZ`, and `CBNZ`, which are the branch forms proven by the exact-r29 micro-CFGs;
- allow `CLREX` as an in-region monitor termination operation;
- require branch targets to be aligned and either inside the candidate copied block or exactly at the one-past-block cleanup sentinel;
- reject any backward branch whose target is not the exclusive-load entry;
- reject calls, indirect branches, exception-generating instructions, unrelated memory operations, and PC-relative data accesses;
- require at least one matching store-exclusive in the candidate and validate every store path against the entry load;
- preserve scalar/pair arity, width, address-base, status/data overlap, and remap-capacity validation;
- require CBZ/CBNZ register operands to be remappable and ensure ordinary body instructions do not overwrite the address register.

The translator searches candidate lengths in ascending order after the first store appears. This preserves the historical shortest linear region for ordinary `LDXR/LDAXR → body → STXR/STLXR` loops and therefore avoids gratuitously changing existing region identities.

### 3.4 Thunk planning

Extend `PlanExclusiveThunk` only as needed for the validated CFG:

- remap the register field of CBZ/CBNZ;
- keep branch immediates unchanged because the validated raw block is copied position-isomorphically;
- branch targets equal to the one-past-block sentinel land on the generated cleanup sequence;
- preserve the NZCV bridge and complete guest-register writeback on the single thunk return path;
- issue an unconditional cleanup `CLREX` immediately after the copied raw block before architectural state is written back to `vm_ctx_t`.

The final cleanup `CLREX` is deliberate monitor hygiene. It is necessary for O0 compare-mismatch paths that branch out without executing a store-exclusive or source `CLREX`; it is harmless after STXR/STLXR or after an already executed source `CLREX`, and it does not change NZCV.

### 3.5 Tests

Add focused tests for:

- exact-r29 scalar compare/exchange branch shapes;
- exact-r29 byte/halfword `SUBS(ext)` + conditional branch shapes;
- exact-r29 pair-exclusive 128-bit dual-store paths;
- retry back-edge behavior;
- explicit CLREX failure paths;
- branch target outside the region rejected;
- backward branch to an interior instruction rejected;
- region with no store-exclusive rejected;
- register remap preserves CBZ/CBNZ operands;
- shortest existing linear region remains unchanged;
- deterministic content identity and thunk generation;
- generated one-past target lands on cleanup CLREX before NZCV/GPR writeback.

### 3.6 Phase 18 gate update

Remove only the `branchful-exclusive` intentional boundary classification after exact-r29 proves the affected baseline functions fully close through the real Translator. Keep `casp128` and `machine-outliner` unchanged.

The exact-r29 test must no longer accept a branchful-exclusive failure. Any remaining one is an unexpected gap.

## 4. Execution policy

- Implement only compiler-proven branch families needed for the closed CFG.
- Do not fold CASP128 or machine-outliner into this phase.
- Do not support arbitrary branch-bearing native snippets.
- Do not change VM wire opcodes or the ExclusiveRegion content-addressing contract.
- Temporary diagnostic/repair workflows and scripts must self-delete or be removed before the final diff.
- Every correction must pass focused Go tests/vet before the full PR Verification.

## 5. Exact-r29 diagnostic evidence and execution consensus

The exact NDK r29 baseline audit confirmed the architecture and removed the need for a new continuation ABI:

- O0 8/16/32/64-bit compare/exchange emits `LDAXR* → CMP/SUBS(ext) → B.ne(one-past) → STLXR* → CBNZ(entry)`. The compare-failure path has no source `CLREX`, so returning directly to the interpreter would leak a live exclusive monitor. The generated thunk therefore always performs cleanup `CLREX` before state writeback.
- O2/Oz 8/16/32/64-bit compare/exchange emits `LDAXR* → CMP → B.ne(CLREX) → STLXR* → CBNZ(entry) → B(one-past) → CLREX`. Copying all seven raw words preserves both internal branch distances; the final `B` targets the generated cleanup sentinel.
- O2/Oz 128-bit pair compare/exchange emits one `LDAXP`, comparisons/selects, a conditional branch to one of two `STLXP` paths, retry `CBNZ` edges to the entry, and a single one-past continuation. The entire 11-word block is position-isomorphic and therefore needs register remapping but no branch-immediate relocation.
- Ordinary fetch-add/exchange exclusive loops remain represented by their existing shortest load/body/store region; the following retry CBNZ remains ordinary VM control flow, preserving existing region identity and avoiding unnecessary native expansion.
- Oz machine-outliner tail branches occur after the exclusive sub-CFG has already closed and therefore remain a separate Phase 18 intentional fail-closed class.

Focused ARM64/runtime tests, full Go tests, and vet pass after implementation when exact-r29-only runtime tests are correctly left to the macOS release gate. The first temporary Ubuntu run failed only because its preinstalled NDK 27 environment triggered `TestBuildInstalledExactR29Object`; no product commit was made from that run. The corrected focused runner clears NDK environment variables, and the authoritative PR gate reruns the exact-r29 compiler corpus and exact-r29 runtime build on macOS.

PR Verification #46 passed on the pre-comment-cleanup product head, including exact-r29 compiler coverage with the `branchful-exclusive` expectation removed, exact-r29 runtime build, race, vet, and macOS ARM64 CLI. The final maintenance cleanup only corrects the stale `trExclusiveRegion` contract comment; the temporary cleanup workflow/script self-delete. A fresh full Verification is required on the final human-triggered head before merge.

## 6. Exit criteria

- exact-r29 baseline atomic branch CFGs are structurally understood and encoded as tests;
- supported branchful exclusive regions execute entirely in one native thunk without interpreter interruption;
- all internal branch targets are proven and position-correct;
- no stale `branchful-exclusive` intentional expectation remains;
- `casp128` and `machine-outliner` remain explicit fail-closed boundaries;
- full Go tests, race, exact-r29 FP/SIMD corpus, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI all pass on the current PR head/current `main` base;
- only the verified head is squash-merged to `main`;
- after merge, `main` push Verification passes and the repository's current default integration ref is fast-forwarded to the same commit so it remains content-identical to `main`.
