# VMP Phase 9 Rewrite Writer Plan

Scope: close the Phase 8/G8 `rewrite-plan-ready` boundary by materializing the already validated `RewritePlan` into a complete in-memory ELF artifact. `archive/**` stays read-only. Decoder/runtime capability expansion is explicitly Phase 10 and out of scope.

## 1) Next-stage work plan

Phase 9 has one product outcome: a valid `Process` / `ProcessAnalyzed` request produces a structurally valid rewritten ELF in `Result.Artifact`, while caller input stays byte-for-byte unchanged and the existing publish layer remains the sole filesystem transaction surface.

The writer is intentionally narrow:

```text
validated Request + Analysis + TranslationPreparation + RuntimeImage
    -> buildRewritePlan (already implemented)
    -> immutable RewritePlan
    -> applyRewritePlan (Phase 9)
    -> structural artifact validation
    -> Result.Artifact
    -> existing app/report/publish path
```

Hard non-goals:

- no instruction/decoder whitelist expansion;
- no CFG/tail/system/atomics/FP-SIMD capability work;
- no runtime ABI redesign;
- no layout, relocation, token, branch-target, or PHDR replanning in the writer;
- no direct output-file writes from `internal/elf`;
- no second temp/rename/rollback implementation;
- no new ELF section headers for appended runtime content;
- no `archive/**` edits.

Current implementation anchors:

- writer sentinel and Phase 8 handoff: `internal/elf/options.go:73,103-148`;
- rewrite-plan model: `internal/elf/rewrite_plan.go:28-110`;
- complete plan construction: `internal/elf/rewrite_plan.go:125-189`;
- final segment placement: `internal/elf/rewrite_plan.go:290-339`;
- finalized bytecode/token/entry patches: `internal/elf/rewrite_plan.go:425-490`;
- plan validation: `internal/elf/rewrite_plan.go:544-567`;
- token trampoline bytes: `internal/elf/rewrite_plan.go:652-692`;
- PHDR planning and relocation: `internal/elf/rewrite_plan.go:695-837`;
- in-place PHDR growth guard: `internal/elf/rewrite_plan.go:840-849`;
- final PHDR serializer: `internal/elf/rewrite_plan.go:852-865`;
- current writer-boundary test: `internal/elf/rewrite_plan_test.go:412-422`;
- current end-to-end boundary test: `internal/elf/analysis_test.go:989-1005`;
- app success handoff: `internal/app/run.go:190-207`;
- existing publication transaction: `internal/publish/publish.go:52-90`.

## 2) RALPLAN-DR consensus plan

### Principles

1. **Plan authority is final.** Writer applies exact planned bytes/offsets; it does not derive a second layout.
2. **Input immutability and failure atomicity.** All mutation happens on a fresh artifact buffer and `Result.Artifact` is assigned only after complete validation.
3. **Smallest responsibility boundary.** ELF materialization belongs in `internal/elf`; filesystem publication stays in `internal/publish`.
4. **Preserve existing ELF metadata unless the plan explicitly says otherwise.** Phase 9 changes only planned PHDR/header/entry ranges plus appended segment bytes; section headers remain byte-for-byte unchanged.
5. **Verification must distinguish writer correctness from parser acceptance.** Exact byte-shape assertions and final structural reparse are separate gates.

### Top 3 decision drivers

1. **Correctness and recoverability:** one bad offset must fail before any success artifact or filesystem publication is visible.
2. **Architectural closure:** Phase 8 already decided runtime layout, entry trampolines, and PHDR topology; Phase 9 must not reopen those decisions.
3. **Minimal blast radius:** reuse current parsers and publish code, add one writer file, and keep implementation/test changes attributable despite the dirty worktree.

### Options

#### Option A — pure in-memory plan applicator

Add `internal/elf/rewrite_writer.go` with one unexported entrypoint:

```go
func applyRewritePlan(input []byte, plan *RewritePlan) ([]byte, error)
```

It validates writer-level bounds/consistency, sizes one artifact buffer, copies input, applies exact plan bytes, and returns the completed artifact only on success.

Pros:
- strongest all-or-nothing semantics;
- easiest mapping from plan fields to output bytes;
- no new I/O abstraction;
- simplest unit testing of exact mutations;
- compatible with the repository's current 1 GiB input-analysis limit.

Cons:
- one full input copy plus appended runtime data is resident during rewrite;
- writer must perform careful overflow/range checks before every copy.

#### Option B — streaming/random-access scratch writer

Build into a temp buffer or random-access sink, then re-read/reparse before returning bytes.

Pros:
- can lower peak memory for very large artifacts;
- natural fit if Phase 9 later needs externalized storage.

Cons:
- more I/O/plumbing and cleanup states;
- still needs random writes for PHDR/header/entry patches;
- duplicates concerns already handled by `internal/publish`;
- no current evidence that memory pressure justifies the extra surface.

#### Option C — assemble the artifact in `internal/app`

Keep ELF planner output abstract and have the app layer write the final image.

Pros:
- fewer changes in `internal/elf` at first glance.

Cons:
- leaks ELF byte-layout details into orchestration code;
- weakens the `ProcessAnalyzed` contract;
- risks duplicating publish responsibility.

**Recommended: Option A.** It is the smallest design that fully closes the current boundary.

### Strongest antithesis, tradeoff tension, synthesis

Strongest antithesis: Option B can reduce memory use and may make very-large-file writing more scalable.

Tradeoff tension: **single-buffer deterministic atomicity vs lower-memory streaming complexity**.

Synthesis: current analysis is already capped at 1 GiB (`internal/elf/parse.go:73-75`), the writer needs random-access mutation, and publication already has a robust temp-file transaction. The extra streaming abstraction is not justified in Phase 9. Use Option A now; revisit only with measured memory/throughput evidence.

### ADR

- **Decision:** implement a single pure in-memory `applyRewritePlan` in `internal/elf/rewrite_writer.go`.
- **Drivers:** exact plan authority, failure atomicity, input immutability, smallest diff, reuse of existing publish semantics.
- **Alternatives considered:** streaming/random-access writer; app-layer artifact assembly; folding materialization back into `buildRewritePlan`.
- **Rejected:**
  - streaming adds I/O state without a demonstrated need;
  - app-layer assembly violates the ELF/package boundary;
  - planner-integrated materialization couples planning and mutation and weakens the immutable-plan checkpoint.
- **Consequences:** the writer owns only byte materialization; plan calculation stays in `rewrite_plan.go`; filesystem durability stays in `publish`.
- **Follow-up:** any future streaming writer requires measured evidence and must preserve the same `RewritePlan -> complete artifact` contract.

## 3) Repair plan

### P0 — lock Phase 9 invariants before implementation

Goal: turn the current boundary into precise regression expectations before adding the writer.

Required invariants:

- `Request.Input` is never modified.
- `RewritePlan` is treated as immutable input.
- `programHeaders.phnumAfter` is the final PHDR count.
- `programHeaders.tableData` is the exact final serialized PHDR table image to place at `phoffAfter`.
- Existing section-header table bytes are immutable in Phase 9.
- Writer never calls layout/planning routines such as `placeSegments`, `planProgramHeaders`, runtime relocation planning, or trampoline construction.
- Failure returns no artifact bytes.

Touchpoints:
- `internal/elf/rewrite_plan_test.go`
- `internal/elf/analysis_test.go`

Regression lock before risky edits:

```bash
go test ./internal/elf
```

### P1 — implement the pure writer core

Goal: apply an already validated `RewritePlan` exactly once.

Primary file:
- **new:** `internal/elf/rewrite_writer.go`

Writer contract:

```go
func applyRewritePlan(input []byte, plan *RewritePlan) ([]byte, error)
```

Required mutation/validation order:

1. Reject nil/internally impossible writer input and validate writer-level copy ranges/overflow only. Do not recompute any plan decision.
2. Compute final artifact size as the maximum of:
   - `len(input)`;
   - every `segment.fileOffset + segment.fileSize`;
   - `programHeaders.phoffAfter + len(programHeaders.tableData)`.
3. Allocate one output slice and copy the full original input into it.
4. Materialize every planned `segment.data` at its exact `segment.fileOffset`.
5. Copy the exact final `programHeaders.tableData` to `programHeaders.phoffAfter`.
   - `PT_NULL` reuse: table size/count can remain unchanged.
   - in-place growth: writes the expanded table into the prevalidated zero/file-backed gap.
   - relocated table: writes the same final bytes into the planned read-only runtime segment location; this must agree with the bytes already embedded in that segment.
6. Update only the ELF64 header fields required by the PHDR plan:
   - `e_phoff` bytes `[32:40] = phoffAfter`;
   - `e_phnum` bytes `[56:58] = phnumAfter`;
   - leave `e_phentsize` and all section-table fields unchanged.
7. Apply every exact `function.entryPatch` at `function.entryFileOffset`.
8. Return the completed scratch artifact; never return a partial slice on error.

Writer-level checks are allowed only to prevent unsafe copies or prove that fields needed for direct application are self-consistent. They must not select alternate offsets, rebuild PHDRs, regenerate entry trampolines, or otherwise become a second planner.

### P2 — close `ProcessAnalyzed` and success-state handoff

Goal: change the validated path from a sentinel failure into a real artifact success.

Touchpoints:
- `internal/elf/options.go`
- `internal/elf/analysis_test.go`
- `internal/elf/rewrite_plan_test.go`
- `internal/elf/preparation_test.go` only where sentinel-specific assertions need deletion/adjustment

Flow:

1. `buildRewritePlan` succeeds as today.
2. Call `applyRewritePlan(req.Input, plan)`.
3. Structurally validate the produced artifact with the existing ELF parser under the normalized request mode and confirm target kind still matches the analyzed target.
4. Only after that validation succeeds:
   - set `DevelopmentStrategy = "rewrite-artifact-ready"`;
   - set `Result.Artifact = artifact`;
   - return `nil` error.
5. On writer or final-reparse error, return the partially populated `Result` with an empty `Artifact`.
6. Remove the temporary boundary-only `ErrRewriteWriterRequired` and `Result.rewritePlan` state once all callers/tests are migrated; both are internal boundary scaffolding, not post-Phase-9 product contracts.

Do not move publication into this layer.

### P3 — integration tests and documentation/report synchronization

Goal: make the new boundary visible end-to-end without changing publication architecture.

Touchpoints:
- `internal/app/run_test.go`
- `internal/report/report_test.go` if exact success fields change
- `docs/android-arm64-test-plan.md`
- `docs/report-schema-v1.md`
- `docs/product-contract.md` only if it still names the writer as an open gate
- `PRODUCTIZATION_HANDOFF.md` only if its current handoff still marks Phase 9 as pending

Exact report/test-plan wording decisions:

- replace `development_strategy: rewrite-plan-ready` with `development_strategy: rewrite-artifact-ready` on successful Phase 9 transforms;
- remove the expected `Phase 9 rewrite writer required` failure from Tier 1 host validation;
- Tier 1 must instead require `status: ok`, `output_sha256`, a structurally valid artifact, and byte-for-byte unchanged input;
- update the Tier 2 prerequisite wording from the stale `Phase 8 writer` wording to the completed `Phase 9 rewrite writer` host-validation gate;
- keep failure-report and artifact-last publication semantics unchanged.

## 4) Execution plan

### Stage 0 — preserve and verify the current baseline

Before Phase 9 implementation:

```bash
git status --short
go test ./internal/elf
go test ./internal/app ./internal/publish ./internal/report
```

Record the dirty-file set. Do not reset/revert it. `archive/**` remains read-only even though it is already dirty from prior work.

Stop condition: current failures, if any, are classified as pre-existing before writer edits begin.

### Stage 1 — add writer-focused regression tests

Add or replace tests to lock these outcomes before integration:

1. exact segment bytes at every planned file offset;
2. exact entry patch bytes at every entry file offset;
3. exact ELF header `e_phoff` and `e_phnum` bytes;
4. exact final PHDR `tableData` at `phoffAfter`;
5. existing section-header table bytes remain byte-for-byte equal to input;
6. input slice remains byte-for-byte equal to its before snapshot;
7. invalid writer range/overflow returns error plus nil/empty artifact.

PHDR scenario matrix:

- **PT_NULL reuse:** verify reused slot becomes the planned `PT_LOAD`; final count semantics match `phnumAfter`.
- **In-place growth:** verify `e_phoff` stays the original location, `e_phnum` grows, and the exact final table occupies the validated gap.
- **Relocation:** verify `e_phoff == phoffAfter`, the final table is in the trailing read-only segment, final `PT_LOAD` size includes it, and `PT_PHDR` describes the relocated table.

Stop condition: tests fail for the current writer-boundary implementation for the expected missing-writer reason, while unrelated baseline tests remain unchanged.

### Stage 2 — implement `internal/elf/rewrite_writer.go`

Implement the smallest direct plan applicator. No interfaces, factories, sink abstractions, or new dependencies.

Acceptance before moving on:

```bash
go test ./internal/elf
```

Stop condition: all writer-core and PHDR scenario tests pass, including input/section-header immutability.

### Stage 3 — integrate `ProcessAnalyzed`

Replace the sentinel boundary with writer + final structural validation.

Required result behavior:

- valid request: `err == nil`, non-empty `Artifact`, `DevelopmentStrategy == "rewrite-artifact-ready"`;
- invalid writer/reparse path: error, empty `Artifact`;
- `RuntimeStrategy` and `OpcodeMapDigest` remain unchanged from the validated runtime path;
- no input mutation.

Delete `ErrRewriteWriterRequired` / hidden result plan state if no migrated test/caller still uses them.

Acceptance:

```bash
go test ./internal/elf
go test ./internal/app
```

### Stage 4 — prove app/report/publish integration without changing publish code

Update the current app boundary test so the real transform continues through success. Assert the produced output is an ELF artifact, the runtime builder is still called with the same aggregated requirements, and publication is still performed by `publish.All`.

Do not modify `internal/publish/publish.go` unless a new failing test proves an actual publication defect caused by Phase 9. Existing publication transaction tests are the baseline contract.

Acceptance:

```bash
go test ./internal/app ./internal/publish ./internal/report
```

### Stage 5 — synchronize product docs and handoff text

Apply only current-fact wording:

- successful development strategy: `rewrite-artifact-ready`;
- no expected Phase 9 sentinel failure;
- Tier 1 host validation expects a real artifact;
- Tier 2 prerequisite names the Phase 9 writer correctly;
- release readiness remains false until the existing device/release matrix is proven.

Acceptance:

```bash
bash scripts/check-contract.sh
```

### Stage 6 — repository verification and implementation commit boundary

Run, in order:

```bash
go test ./internal/elf ./internal/app ./internal/publish ./internal/report
go test ./...
go vet ./...
make packer
bash -n scripts/*.sh
bash scripts/check-contract.sh
```

If an environment-dependent gate cannot run, record the exact blocker; do not silently substitute a weaker claim.

Before the implementation commit:

```bash
git status --short
git diff --check
```

Stage only Phase 9 files. Never sweep the dirty worktree with `git add .`.

## 5) Testable acceptance criteria

Phase 9 is implementation-complete only when all applicable criteria are proven:

1. **Success artifact:** a valid rewrite-ready request returns `err == nil` and a non-empty ELF `Result.Artifact`.
2. **Input immutability:** input bytes are identical before/after both success and failure.
3. **Failure atomicity:** every writer/reparse failure returns an empty artifact; no partial scratch buffer escapes through `Result`.
4. **Plan fidelity:** all runtime segment bytes appear exactly at planned file offsets.
5. **Entry fidelity:** every function's planned `entryPatch` appears exactly at `entryFileOffset`.
6. **Header fidelity:** final ELF header bytes encode exactly `e_phoff = phoffAfter` and `e_phnum = phnumAfter`.
7. **PHDR fidelity:** bytes at `phoffAfter` equal `programHeaders.tableData` exactly.
8. **PT_NULL path:** reusable post-load PT_NULL slots are consumed exactly as planned without an unnecessary PHDR relocation.
9. **In-place-growth path:** expanded PHDR tables stay in place only in the already validated file-backed zero gap.
10. **Relocation path:** non-growable PHDR tables move to the planned read-only segment and any existing `PT_PHDR` points to the final table with the final size.
11. **Section-header immutability:** the original section-header table bytes, `e_shoff`, `e_shentsize`, `e_shnum`, and `e_shstrndx` are unchanged.
12. **Structural validation:** final artifact reparses successfully under existing strict ELF metadata validation and preserves the analyzed target kind.
13. **No replanning:** writer code does not invoke planner/layout/relocation/trampoline-generation routines.
14. **Publication reuse:** app success still reaches `internal/publish.All`; no duplicate filesystem transaction layer is added.
15. **Accurate reporting:** success reports use `development_strategy: rewrite-artifact-ready`, include output hash/status success, and do not mention the writer-required sentinel.
16. **Scope discipline:** no new Phase 10 capability work and no `archive/**` edits are introduced by the Phase 9 implementation.

## 6) Risks and mitigations

### Risk: PHDR table and segment bytes disagree after relocation
Mitigation: require exact `tableData` equality at `phoffAfter`, verify the relocated table is covered by the planned read-only segment, and reparse the final artifact.

### Risk: accidental mutation aliases the caller input
Mitigation: allocate the full scratch artifact before any write; tests snapshot and compare input on success and failure.

### Risk: writer becomes a second planner
Mitigation: prohibit calls into layout/PHDR/trampoline generation from the writer; only bounds/overflow checks and direct copies are allowed.

### Risk: section-table scope creep
Mitigation: section headers are an explicit Phase 9 invariant: preserve them byte-for-byte because the plan contains no section-header mutation model.

### Risk: full-buffer memory overhead
Mitigation: accept one input copy under the current <=1 GiB parser boundary. Revisit streaming only with measured evidence; do not pre-build that abstraction.

### Risk: dirty worktree contaminates the Phase 9 commit
Mitigation: snapshot `git status --short`, stage exact paths, inspect `git diff --cached`, and never reset/revert unrelated edits.

### Risk: stale report/docs still claim writer failure
Mitigation: make the exact `rewrite-artifact-ready` wording update part of Phase 9 acceptance, not optional cleanup.

## 7) File touchpoints

Expected implementation files:

- **new:** `internal/elf/rewrite_writer.go`
- `internal/elf/options.go`
- `internal/elf/rewrite_plan_test.go`
- `internal/elf/analysis_test.go`
- `internal/elf/preparation_test.go` only for obsolete sentinel assertions
- `internal/app/run_test.go`
- `internal/report/report_test.go` only if exact strategy expectations require it
- `docs/android-arm64-test-plan.md`
- `docs/report-schema-v1.md`
- `docs/product-contract.md` only if stale
- `PRODUCTIZATION_HANDOFF.md` only if stale

Expected unchanged behavior files:

- `internal/app/run.go` publication ordering, unless only minimal success-path glue is mechanically required;
- `internal/publish/publish.go` and its transaction model;
- `internal/arch/arm64/**`;
- `internal/runtime/**` behavior/capabilities;
- `archive/**`.

## 8) Consensus reviewer improvement record

### Planner -> Architect

Architect accepted the pure in-memory design but sharpened these points:

- treat streaming as the strongest antithesis rather than dismissing it;
- make single-buffer atomicity vs memory overhead the central tradeoff;
- preserve section headers byte-for-byte instead of inventing “required section-header metadata” work;
- define exact writer mutation order;
- make `phnumAfter` final-count semantics and final `tableData` semantics explicit.

### Architect -> Critic

Critic verdict: **APPROVE**.

No blocking corrections. Required final-plan improvements adopted:

1. exact writer entrypoint/file: `internal/elf/rewrite_writer.go`, `applyRewritePlan`;
2. exact final ELF header byte-shape assertions;
3. structural reparse and section-header immutability are separate tests;
4. post-writer report wording uses exact replacement `rewrite-artifact-ready`;
5. “no layout recomputation” remains a hard implementation invariant.

## 9) Available-agent-types roster

Available useful roles for the implementation phase:

- `executor` — implement `rewrite_writer.go` and minimal `ProcessAnalyzed` integration;
- `test-engineer` — writer/PHDR/failure-atomicity regression matrix;
- `verifier` — run acceptance commands and inspect exact artifact/commit evidence;
- `architect` — only for a discovered ELF structural contradiction, not routine implementation;
- `critic` — adversarial review if implementation diverges from the approved plan;
- `explore` — repo lookup only, especially to prove a caller/touchpoint set before deleting boundary scaffolding;
- `git-master` — isolate/stage Phase 9 commits without contaminating the existing dirty worktree.

## 10) Ralph / Team staffing guidance

### Preferred default: solo or Ralph single-owner execution

The writer and integration are tightly coupled and touch a small number of files. One executor owning writer + integration is the lowest-conflict path. Use Ralph only if Phase 9 becomes a long verification/fix loop.

Suggested ownership:

- owner A (`executor`): `internal/elf/rewrite_writer.go`, `internal/elf/options.go`;
- owner B (`test-engineer`, only if parallelism is useful): writer tests in `internal/elf/*_test.go`;
- owner C (`verifier`): read-only acceptance verification after integration.

### Team mode

Use Team only when parallelism outweighs coordination overhead. If used:

1. executor owns writer/integration files;
2. test-engineer owns test-only files;
3. writer/docs owner updates docs only after behavior is green;
4. verifier owns no source files and validates the integrated commit.

Do not split PHDR writer logic across multiple implementation owners.

If Ralph runtime is activated later, satisfy its PRD + test-spec gate before implementation; this plan itself does not start a runtime workflow.

## 11) Team verification path

Verification is sequential at integration boundaries even if implementation lanes run in parallel:

1. **Writer lane:** targeted `internal/elf` tests prove exact byte materialization and failure atomicity.
2. **Integration lane:** `ProcessAnalyzed` tests prove success artifact, strategy state, target-kind reparse, and unchanged input.
3. **App lane:** `internal/app` test proves the real transform now reaches the unchanged publish layer.
4. **Publish/report lane:** existing publish rollback tests remain green; report tests prove accurate post-writer wording.
5. **Repository gate:** `go test ./...`, `go vet ./...`, build/script/contract checks where environment permits.
6. **Git evidence:** staged diff contains only the intended Phase 9 implementation set; `archive/**` remains untouched by this phase.

A verifier must reject completion if any claim is supported only by source inspection when a runnable test can decide it.

## 12) Current planning commit acceptance

This turn commits planning only, not Phase 9 implementation.

Only these two ignored `.omx/` files may be force-staged:

- `.omx/context/vmp-phase9-rewrite-writer-20260831T224943Z.md`
- `.omx/plans/vmp-phase9-rewrite-writer-plan.md`

Required checks before commit:

```bash
git diff --check -- .omx/context/vmp-phase9-rewrite-writer-20260831T224943Z.md .omx/plans/vmp-phase9-rewrite-writer-plan.md
git add -f .omx/context/vmp-phase9-rewrite-writer-20260831T224943Z.md .omx/plans/vmp-phase9-rewrite-writer-plan.md
git diff --cached --stat
git diff --cached -- .omx/context/vmp-phase9-rewrite-writer-20260831T224943Z.md .omx/plans/vmp-phase9-rewrite-writer-plan.md
```

Stop if anything else is staged.

Lore commit intent:

```text
Freeze the Phase 9 writer execution contract before implementation.

Constraint: Preserve the dirty Phase 8 worktree and keep archive sources read-only.
Rejected: Recompute layout or publish files inside the writer | both violate the validated-plan boundary and duplicate existing responsibilities.
Confidence: high
Scope-risk: narrow
Directive: Phase 9 applies only an immutable validated RewritePlan and must not absorb Phase 10 capability work.
Tested: Planner, Architect, and Critic consensus plus plan-reference and staged-diff validation.
Not-tested: Phase 9 implementation has not started.
```
