# Context Snapshot: VMP Phase 9 Rewrite Writer

Captured: `2026-08-31T22:49:43Z` (`2026-09-01 07:49:43 +09:00`)
Branch: `fix/call-vm-nested`

## Task

Freeze the next-stage plan for Phase 9, including the work plan, RALPLAN-DR consensus, repair plan, execution plan, acceptance gates, risks, ADR, staffing, and verification path, then commit only these planning artifacts.

## Desired outcome

Close the current Phase 8/G8 boundary by turning an already validated, immutable `RewritePlan` into one complete in-memory ELF artifact. Phase 9 ends when a valid request returns a fully materialized `Result.Artifact` and the existing app/publish layer can publish it without any new filesystem transaction logic.

## Current evidence

- `internal/elf/options.go:73` defines the temporary `ErrRewriteWriterRequired` sentinel.
- `internal/elf/options.go:103-148` shows `ProcessAnalyzed` validating input/runtime/preparation, building a rewrite plan, setting `DevelopmentStrategy = "rewrite-plan-ready"`, storing `result.rewritePlan`, and stopping at the writer boundary.
- `internal/elf/rewrite_plan.go:28-110` defines planned segments, runtime placement facts, function entry patches, and the final program-header plan.
- `internal/elf/rewrite_plan.go:125-189` proves `buildRewritePlan` completes runtime layout, relocation/finalization, PHDR planning, and plan validation before returning.
- `internal/elf/rewrite_plan.go:290-339` fixes final runtime segment file offsets/VAs before the writer exists.
- `internal/elf/rewrite_plan.go:425-490` finalizes bytecode, tokens, entry targets, entry file offsets, and exact entry patch bytes inside the plan.
- `internal/elf/rewrite_plan.go:544-567` validates plan segment alignment, W^X, size consistency, non-overlap, and selection/function count consistency.
- `internal/elf/rewrite_plan.go:695-837` already decides all PHDR behavior: post-last-load `PT_NULL` reuse, in-place PHDR growth, fallback PHDR-table relocation into the trailing read-only runtime segment, final `PT_LOAD` entries, and `PT_PHDR` update.
- `internal/elf/rewrite_plan.go:831-835` makes `programHeaders.tableData` the final serialized PHDR image; when relocated, those exact bytes are already embedded in the planned read-only segment.
- `internal/elf/rewrite_plan.go:840-849` permits in-place PHDR growth only when the range is file-backed, zero-filled, and does not overlap protected data.
- `internal/elf/rewrite_plan.go:852-865` is the canonical PHDR serializer.
- `internal/elf/rewrite_plan_test.go:412-422` locks the current writer boundary: `ErrRewriteWriterRequired` plus empty artifact.
- `internal/elf/analysis_test.go:989-1005` separately locks the same boundary and verifies input immutability.
- `internal/app/run.go:190-207` treats successful transform output as `result.Artifact` and routes it through the existing publish layer.
- `internal/publish/publish.go:52-90` already provides temp-file preparation, fsync/publish sequencing, artifact-last success-marker semantics, rollback, and cleanup. Phase 9 must not duplicate it.
- `docs/android-arm64-test-plan.md:21-28` and `docs/report-schema-v1.md:21-24,48-54,66-73` still describe the pre-writer boundary and must be synchronized only when implementation lands.

## Hard constraints

1. Preserve the existing dirty worktree. Do not reset, revert, overwrite, stage, or commit unrelated edits.
2. `archive/**` is read-only for Phase 9 implementation.
3. Phase 9 is strictly `validated RewritePlan -> complete []byte artifact`.
4. Do not expand decoder/runtime instruction coverage, CFG recovery, tails, system instructions, atomics, FP/SIMD, or other Phase 10 capability work.
5. Do not recompute layout, relocations, branch targets, segment placement, token placement, PHDR slots, or PHDR relocation in the writer.
6. Do not mutate `Request.Input`.
7. On any writer or final-structure validation error, `Result.Artifact` stays empty and no partial artifact escapes.
8. Do not add a second publish/rollback transaction layer; continue to use `internal/publish.All` unchanged.
9. The current `RewritePlan` contains no section-header mutation plan. Phase 9 therefore preserves the existing ELF section-header table byte-for-byte; new runtime content is represented by the planned `PT_LOAD` segments.

## RALPLAN-DR consensus result

Planner recommendation: one unexported pure in-memory writer, e.g. `applyRewritePlan(input []byte, plan *RewritePlan) ([]byte, error)`.

Architect challenge: a streaming/random-access writer can reduce memory use, but it adds plumbing and weakens the direct all-or-nothing mapping from immutable plan to artifact. The strongest alternative does not justify the extra surface at the current <=1 GiB analysis boundary.

Architect synthesis: keep the pure in-memory writer. Mutation order is plan/bounds validation -> final size -> allocate/copy input -> segment blobs -> final PHDR table -> ELF `e_phoff/e_phnum` -> entry patches -> structural reparse -> assign artifact.

Critic verdict: `APPROVE`, no blocking corrections.

Reviewer refinements adopted:
- name the writer entrypoint/file explicitly;
- test the final ELF header bytes independently from full structural reparse;
- test section-header byte-for-byte preservation independently from reparse;
- make post-writer report wording exact;
- keep “no layout recomputation” as a hard implementation invariant;
- state explicitly that `phnumAfter` is the final PHDR count and `tableData` is the exact final PHDR image.

## Settled design

Recommended implementation surface:

- New `internal/elf/rewrite_writer.go` with the single writer entrypoint `applyRewritePlan`.
- Minimal `internal/elf/options.go` integration: build plan -> apply writer -> reparse/validate final artifact -> set success strategy -> assign `Result.Artifact` -> return nil.
- Replace the temporary boundary-only `ErrRewriteWriterRequired` and `Result.rewritePlan` state once no tests/callers need them.
- Preserve section headers exactly; do not invent runtime section-table entries.
- Keep `internal/app/run.go` and `internal/publish/publish.go` behavior unchanged except tests that now observe the transform continuing through successful publication.

Post-writer development strategy chosen for the plan: `rewrite-artifact-ready`.

## Validation baseline for implementation

Targeted first:

```bash
go test ./internal/elf
go test ./internal/app ./internal/publish ./internal/report
```

Then repository-level gates where environment permits:

```bash
go test ./...
go vet ./...
make packer
bash -n scripts/*.sh
bash scripts/check-contract.sh
```

## Planning-commit guard

This planning commit may contain only:

- `.omx/context/vmp-phase9-rewrite-writer-20260831T224943Z.md`
- `.omx/plans/vmp-phase9-rewrite-writer-plan.md`

Because `.omx/` is ignored by `.git/info/exclude`, stage only these exact paths with `git add -f`. Never use `git add .` for this commit.
