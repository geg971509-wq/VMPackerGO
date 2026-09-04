# PRD: VMP Phase 9 Rewrite Writer

## Purpose

This task keeps the historical “Phase 9 rewrite writer” workflow label for continuity, while canonical product numbering keeps the plan-first writer in Phase 8 and Phase 9 for corpus/device differential work. The final applicator turns the current fail-closed `rewrite-plan-ready` boundary into a complete in-memory ELF artifact writer. The user-visible outcome is a packed artifact in `Result.Artifact`, with the existing publish layer still handling filesystem writes.

## Scope

In scope:
- apply the already validated `RewritePlan` to produce a complete ELF byte slice;
- keep `Request.Input` unchanged;
- keep section headers byte-for-byte unchanged;
- validate the written artifact structurally before success;
- keep `internal/publish` as the only filesystem transaction layer;
- update the success boundary value to `rewrite-artifact-ready`;
- preserve `rewrite-plan-ready` on writer/reparse failure so failure reports identify the last successfully completed boundary.

Current basis:
- `internal/elf/options.go:59-73, 103-148` still returns `ErrRewriteWriterRequired` and reports `rewrite-plan-ready`.
- `internal/elf/rewrite_plan.go:125-189, 695-865` already finalizes layout, runtime symbols, PHDR decisions, and PHDR bytes before any writer exists.
- `internal/app/run.go:190-207` already publishes `Result.Artifact`.
- `internal/publish/publish.go:52-90` already owns prepare / publish / cleanup / rollback.
- `docs/report-schema-v1.md:21-34, 48-54` still treats `rewrite-plan-ready` as the current boundary string.
- `docs/android-arm64-test-plan.md:21-40` still names the pre-writer host gate and the later device gate.
- `PRODUCTIZATION_HANDOFF.md:206-228` calls the overall plan-first writer Phase 8 while the current sentinel calls the final applicator Phase 9; docs must preserve the canonical product numbering and remove the obsolete sentinel wording rather than renumbering history.

## Non-goals

- no Phase 8 replanning changes;
- no decoder, CFG, tail, system-instruction, atomic, FP/SIMD, or runtime-ABI expansion;
- no `archive/**` work;
- no new filesystem transaction layer;
- no new publication behavior;
- no release engineering, signing, or Phase 10 work.

## User-visible / result contract

On success:
- `Result.Artifact` contains the rewritten ELF bytes;
- `Result.DevelopmentStrategy == "rewrite-artifact-ready"`;
- `Result.RuntimeStrategy` remains the validated runtime strategy already in use;
- the input buffer is unchanged;
- the artifact reparses successfully;
- the CLI can publish the artifact through the existing `internal/publish` layer;
- section headers are unchanged byte-for-byte.

## Failure contract

On any writer or validation failure:
- return an error;
- keep `Result.Artifact` empty;
- keep `Result.DevelopmentStrategy == "rewrite-plan-ready"` once planning succeeded;
- preserve `Request.Input` exactly;
- do not publish an artifact;
- keep failure reporting available through the existing report path when safe;
- do not fall back to a second writer or a layout replanner.

## Exact success state

Phase 9 is successful only when all of the following are true:
1. A valid request returns `nil` error.
2. `Result.Artifact` is non-empty.
3. The artifact is structurally valid after a fresh ELF reparse.
4. `Request.Input` is byte-for-byte unchanged.
5. Section headers are byte-for-byte unchanged.
6. `Result.DevelopmentStrategy` is `rewrite-artifact-ready`.
7. The existing publish path remains the only filesystem transaction surface.
8. PT_NULL reuse / in-place growth / relocated PHDR handling all work from the finalized plan, without replanning.

## Done criteria

Phase 9 is done when the implementation package proves the following with tests and CLI evidence:
- unit tests cover the writer’s exact mutation rules and failure atomicity;
- integration tests prove the success boundary change from sentinel to artifact success;
- CLI tests prove publish reuse and failure behavior;
- the final artifact reparses successfully;
- docs and report text no longer describe Phase 9 as an open writer boundary;
- no unrelated publication path was added.

## Contract notes

- The exact success boundary value should be `rewrite-artifact-ready`, not `rewrite-plan-ready`, because the latter describes the pre-writer boundary that Phase 9 is replacing.
- `Result.Artifact` must only be assigned after writer success and structural validation success.
- Writer-level validation may prove direct-application consistency and bounds, including PHDR-before fields and relocated-PHDR byte agreement, but may not recompute planner decisions.
- The plan-only `rewritePlan` state and `ErrRewriteWriterRequired` are internal scaffolding that should disappear once the success path is fully wired.
