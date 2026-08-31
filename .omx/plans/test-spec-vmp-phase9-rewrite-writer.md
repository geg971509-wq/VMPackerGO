# Phase 9 Rewrite Writer Test Spec

## Goal

Prove that the Phase 9 writer turns a validated `RewritePlan` into a structurally valid ELF artifact without mutating input, without changing section headers, without replanning, and without introducing a second filesystem transaction path.

## Test layers

### 1) Unit tests

Target files:
- new `internal/elf/rewrite_writer_test.go`
- `internal/elf/rewrite_plan_test.go`
- `internal/elf/analysis_test.go`

Coverage:
- exact byte application from a finalized `RewritePlan`;
- PT_NULL reuse;
- in-place PHDR growth;
- relocated PHDR table / PT_PHDR;
- input immutability;
- section-header immutability;
- failure atomicity;
- no replanning in the writer path.

Suggested assertions:
- the output slice is distinct from the input slice;
- input bytes are unchanged after success and after failure;
- section-header table bytes are identical before and after apply;
- the writer copies `programHeaders.tableData` exactly to `phoffAfter`;
- the writer patches only the fields the plan already decided;
- a malformed or inconsistent plan returns an error and no partial artifact;
- direct-application preconditions reject mismatched `e_phoff/e_phnum`, malformed final table length, overflowed ranges, or relocated-PHDR bytes that disagree with the planned read-only segment.

### 2) Integration tests

Target files:
- `internal/elf/analysis_test.go`
- `internal/elf/rewrite_plan_test.go`

Coverage:
- `ProcessAnalyzed` / `Process` returns `rewrite-artifact-ready` on success;
- `Result.Artifact` is non-empty only after final writer + structural reparse success;
- writer/reparse failure after planning retains `DevelopmentStrategy == "rewrite-plan-ready"`, while success advances to `rewrite-artifact-ready`;
- `Request.Input` remains unchanged;
- the temporary boundary sentinel is gone from the success path;
- target kind and normalized request mode still agree after reparse.

### 3) CLI / publish tests

Target file:
- `internal/app/run_test.go`

Coverage:
- successful CLI run publishes artifact and report through the existing `internal/publish` layer;
- publish reuse still honors artifact-last success semantics;
- failure publishes no artifact;
- failure may still publish only the requested report when safe;
- no duplicate filesystem transaction path appears.

### 4) Reparse regression tests

Target files:
- `internal/elf/rewrite_writer_test.go`
- `internal/elf/analysis_test.go`

Coverage:
- the final artifact is reparsed with the current ELF parser after writer application;
- the parser sees a valid artifact under the normalized mode;
- PHDR updates still parse correctly after PT_NULL reuse, in-place growth, or relocation.

## PHDR case matrix

### PT_NULL reuse

Fixture:
- a plan with trailing `PT_NULL` slots available after the last load.

Assert:
- the writer uses the finalized `programHeaders.tableData` and does not expand the PHDR table when reuse is enough.

### In-place growth

Fixture:
- a plan where the PHDR table expands into the prevalidated gap.

Assert:
- the writer writes the expanded table in place;
- the input remains unchanged;
- the final artifact reparses.

### Relocated PHDR table / PT_PHDR

Fixture:
- a plan where growth cannot stay in place and the PHDR table is relocated into the read-only runtime segment.

Assert:
- the writer copies the relocated table bytes exactly;
- `PT_PHDR` points at the relocated table;
- the final artifact reparses;
- no other PHDR topology is recomputed.

### Input immutability

Assert:
- the original byte slice is identical before and after both success and failure paths.

### Section-header immutability

Assert:
- the section-header table bytes are unchanged in the output artifact.

### Failure atomicity and failure observability

Assert:
- any writer or final-reparse error leaves no partial artifact in the returned result;
- after planning succeeded, the failure result/report retains `development_strategy: rewrite-plan-ready`;
- CLI failure leaves no artifact file.

### No replanning

Assert:
- the writer consumes the supplied plan as-is;
- the output changes only when the plan changes;
- there is no second planning pass in the writer layer.

### Publication reuse

Assert:
- `internal/publish` remains the only filesystem transaction layer;
- successful CLI publication still uses the existing artifact-last path;
- a successful writer does not imply any new direct file write path in `internal/elf`.

## Required regression suite shape

Run, at minimum:
- `go test ./internal/elf`
- `go test ./internal/app ./internal/publish ./internal/report`
- `go test ./...`

## Pass criteria

The test spec is satisfied only if:
- all PHDR cases above are covered;
- the writer success path is proven by a fresh reparse;
- input immutability and section-header immutability are both covered;
- failure atomicity is covered;
- CLI publish reuse is covered;
- no test needs to reopen Phase 8 layout decisions;
- test/docs wording distinguishes the Phase-8 plan-first-writer umbrella from the Phase-9 final applicator while canonical product docs continue to call the complete plan-first writer Phase 8 and corpus/device differential work Phase 9.
