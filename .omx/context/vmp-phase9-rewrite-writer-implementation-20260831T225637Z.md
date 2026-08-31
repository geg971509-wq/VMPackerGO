# Context Snapshot: VMP Phase 9 Rewrite Writer Implementation

## Task

Create the next-stage implementation-ready work plan, RALPLAN-DR consensus plan, repair plan, and execution plan for the Phase 9 ELF rewrite writer, then commit planning artifacts only.

## Desired outcome

Move the repository from the current `rewrite-plan-ready` fail-closed boundary to a complete in-memory ELF artifact producer without reopening Phase 8 layout decisions, mutating caller input, duplicating filesystem publication, or absorbing later runtime/decoder capability work.

## Current repository state

- Branch: `fix/call-vm-nested`.
- HEAD: `bf194bf` (`Advance the AArch64 runtime through validated rewrite planning.`).
- Worktree has no uncommitted project source changes; only `.claude/` and `.jspace/` are untracked local workflow files.
- Targeted baseline is green: `go test ./internal/elf ./internal/app ./internal/publish ./internal/report`.
- Existing design plan: `.omx/plans/vmp-phase9-rewrite-writer-plan.md`.

## Current implementation evidence

- `internal/elf/options.go:70-73` still carries hidden `rewritePlan` result state and the temporary `ErrRewriteWriterRequired` sentinel.
- `internal/elf/options.go:103-148` validates provenance/runtime/preparation, builds `RewritePlan`, sets `rewrite-plan-ready`, then returns the sentinel without an artifact.
- `internal/elf/rewrite_plan.go:28-110` defines immutable segment, runtime-section, symbol, function-entry-patch, and PHDR planning data.
- `internal/elf/rewrite_plan.go:125-189` fully resolves layout, runtime symbols, relocations, bytecode/token data, PHDR topology, and plan validation before returning.
- `internal/elf/rewrite_plan.go:695-837` decides PT_NULL reuse, in-place PHDR growth, fallback PHDR relocation into the trailing read-only runtime segment, final PT_LOAD entries, optional PT_PHDR update, and final serialized PHDR bytes.
- `internal/elf/rewrite_plan_test.go:412-422` locks the current writer boundary.
- `internal/elf/analysis_test.go:989-1005` independently verifies the same boundary and input immutability.
- `internal/app/run_test.go:170-195` expects the sentinel and verifies no output publication at the boundary.
- `internal/app/run.go:190-207` already routes a successful `Result.Artifact` to the existing publish layer.
- `internal/publish/publish.go:52-90` already owns temp-file preparation, publish ordering, artifact-last success semantics, cleanup, and rollback.
- Documentation still accurately describes the current pre-writer boundary and must change only with the implementation.

## Hard constraints

1. Phase 9 is only `validated RewritePlan -> complete []byte ELF artifact` plus minimal success-path integration.
2. `Request.Input` remains byte-for-byte unchanged on success and failure.
3. The writer may validate bounds/overflow/self-consistency, but must not recompute layout, relocations, token placement, entry trampolines, PHDR slots, or PHDR relocation.
4. Existing section headers remain byte-for-byte unchanged because `RewritePlan` contains no section-header mutation model.
5. `Result.Artifact` remains empty on writer or final-structure validation failure; no partial artifact escapes.
6. `internal/publish` remains the sole filesystem transaction layer.
7. No decoder, CFG, tail, system-instruction, atomic, FP/SIMD, runtime-ABI, or other capability expansion is in Phase 9.
8. `archive/**` is not part of the Phase 9 writer implementation.
9. Planning commit must not stage `.claude/`, `.jspace/`, source files, or unrelated artifacts.

## Planning deliverables

- One implementation-ready plan containing:
  - next-stage work plan;
  - RALPLAN-DR consensus with 3-5 principles, top 3 drivers, >=2 viable options, recommendation;
  - repair plan;
  - execution plan;
  - testable acceptance criteria;
  - risks/mitigations;
  - ADR;
  - reviewer improvement record;
  - available-agent roster;
  - Ralph/Team staffing and verification path.
- Phase 9 PRD.
- Phase 9 test specification.

## Stop condition

Planning is complete only when Planner -> Architect -> Critic reaches approval, every important file reference is current, acceptance criteria are runnable or explicitly manual, and the Git commit contains only the new planning artifacts.
