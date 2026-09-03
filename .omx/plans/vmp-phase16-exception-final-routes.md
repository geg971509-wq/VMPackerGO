# VMP Phase 16 — Final VM Routes for C++ Exception Bridges

## 1. Initial action plan

Goal: turn each Phase 13/15 `InvokeThunk` from translation-phase VM offsets into deterministic offsets in the final reversed bytecode representation, without enabling the still-incomplete runtime exception bridge.

The runtime/personality/landing implementation must never consume `VMCallOffset` or `VMLandingPad` directly after bytecode reversal.

## 2. Plan audit

### Existing facts

- `InvokeThunk.VMCallOffset` and `VMLandingPad` are produced before bytecode finalization.
- `finalizePreparedBytecode` always reverses instructions with `reverseInstructions` and uses the returned `offsetMap` to rewrite the trailer ARM64→VM map.
- image relocations, opcode encryption and whole-bytecode XOR do not change instruction boundaries or the reversal mapping.
- Phase 15 added `OriginalLandingPad` specifically as the stable identity that survives final bytecode remapping.
- Phase 13 currently fails closed before runtime build whenever `ExceptionBridges` is non-empty. This guard must remain in force in Phase 16.

### Main risk

A future invoke/landing bridge that consumes translation-phase `VMCallOffset` / `VMLandingPad` would resume at the wrong VM boundary after reversal. Reimplementing reversal math separately would create a second source of truth.

### Corrected design

Reuse the exact `reverseInstructions` offset map that final bytecode generation already uses. Materialize a writer-facing route model during translation preparation:

```go
type PreparedExceptionRoute struct {
    ThunkID              uint32
    OriginalCallPC       uint64
    OriginalLandingPad   uint64
    FinalVMCallOffset    uint32
    FinalVMLandingOffset uint32
}
```

Each `PreparedExceptionBridge` carries a deterministic `Routes` slice in the same thunk order.

This phase does not alter `unwind.InvokeThunk`; its translation-time offsets remain useful provenance and validation inputs.

## 3. Fix plan

### Route resolver

Add `internal/elf/exception_routes.go` with a pure resolver that:

1. validates selection/translation/plan/thunk provenance;
2. calls the same `reverseInstructions(translation.Bytecode, translation.CodeLen, opcodes)` used by finalization;
3. resolves every thunk `VMCallOffset` and `VMLandingPad` through the returned offset map;
4. cross-checks `OriginalPC` and `OriginalLandingPad` against `TranslateResult.SourceMap`;
5. rejects missing/duplicate thunk IDs, offsets outside uint32, source-map mismatches, or offsets not on VM instruction boundaries;
6. returns routes in deterministic thunk order without mutating the unwind plan.

### Preparation integration

After `prepareExceptionBridges`, populate `PreparedExceptionBridge.Routes` using the matching `PreparedFunction.Translation` and current opcode map.

Do not relax `ValidateRuntimeRequirements` or `ValidateRuntimeImage`; pending exception bridges must still fail closed.

### Final-bytecode cross-check helper

Add an internal test helper/parser that XOR-decodes a finalized bytecode trailer and proves its ARM64→VM entries match the route resolver for both original call PC and original landing PC. Production route resolution must not depend on decrypting final bytecode.

## 4. Tests

- ordinary Phase 13 local-landing fixture gains one deterministic final route;
- `OriginalCallPC` and `OriginalLandingPad` map to source-map offsets and then to final reversed offsets;
- resolved final offsets differ from translation offsets for a multi-instruction function where reversal moves them;
- final route values equal the actual remapped trailer entries produced by `finalizePreparedBytecode` after undoing test XOR;
- duplicate thunk IDs, missing source-map identities, mismatched translation offsets, and invalid VM boundaries fail closed;
- route resolution does not mutate `ExceptionBridgePlan` or `TranslateResult`;
- Phase 13 product guard still rejects pending bridges.

## 5. Exit criteria

- every prepared invoke thunk has an explicit final reversed-bytecode call/landing route;
- route calculation has exactly one reversal source of truth;
- final trailer and prepared routes agree in tests;
- no runtime exception support is claimed or enabled;
- focused elf/unwind tests and vet pass, followed by the full repository gate before merge.

## 6. Follow-up

Phase 17 can consume `PreparedExceptionRoute` to build linkable invoke/landing wrappers and their FDE/LSDA metadata. Only after runtime personality semantics, final landing routing, GNU unwind discovery and Android device unwinder validation are closed should the Phase 13 runtime guard be removed.