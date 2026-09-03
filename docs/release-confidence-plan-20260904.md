# Release-confidence hardening plan

Date: 2026-09-04

## 1. Necessity audit

The previous productization closure completed the known in-repository correctness and release-tooling gaps. The next useful stage is confidence, not feature expansion.

Required now:

- Actively fuzz the existing decoder, ELF metadata, EH-frame and LSDA fuzz targets in CI. Normal `go test` executes seed cases but does not perform mutation fuzzing.
- Add a replayable release rehearsal that reruns all in-repository evidence/contract self-tests and proves the final release contract still fails closed when no external evidence is supplied.
- Keep these gates short and deterministic enough for the canonical macOS Verification workflow.

Not part of this repair:

- FP/vector/aggregate/variadic protected-entry inference without explicit trusted ABI metadata.
- SVE/SVE2/SME implementation solely for breadth claims.
- Generic native external tail emulation that would change LR/PAC/unwind behavior.
- Decrypted-bytecode caching or token lookup indexing without profiling evidence.
- Fabricating physical Android, Apple notarization, or independent-review evidence.

## 2. Initial plan

1. Add one host fuzz-smoke runner covering all current fuzz targets with a bounded per-target duration.
2. Add a release-rehearsal runner that executes demo/evidence/contract validation and asserts that `--release` rejects a missing evidence document for the expected reason.
3. Expose both via Make targets.
4. Add both to the canonical Verification workflow.
5. Run exact-head Verification.
6. Merge only the verified head to `main`, then require the `main` push Verification to pass.

## 3. Plan audit / consensus

Accepted:

- Fuzzing is a robustness gate, not an instruction-support definition.
- Each fuzz target runs separately so a failure identifies one parser/decoder boundary.
- Fuzz duration is intentionally short in PR CI; longer fuzz campaigns remain optional/manual.
- Release rehearsal must prove fail-closed behavior without constructing fake external evidence.
- Existing product scope and fail-closed unsupported semantics are preserved.

Rejected alternatives:

- Do not turn PR CI into an unbounded or hour-long fuzz campaign.
- Do not weaken release validation to make rehearsal pass.
- Do not add mock signing/notarization success records to the repository.
- Do not mix performance refactors into this confidence-only stage.

## 4. Corrected final plan

Completion requires:

- all four current Go fuzz targets receive active mutation fuzzing in canonical Verification;
- fuzz duration is configurable but bounded by a small default;
- release rehearsal reruns all local evidence/contract self-tests and confirms missing external release evidence is rejected;
- Make help exposes both new gates;
- exact PR head passes full Verification, including existing Go/race/r29/runtime/vet/macOS-build gates;
- the exact verified head is merged to `main` and the resulting main Verification passes;
- physical-device, Apple and independent-review evidence remain external blockers rather than source-code TODOs.
