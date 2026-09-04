# VMP Phase 13 — Exception Bridge Preflight and Fail-Closed Integration

## 1. Initial action plan

Goal: connect the existing Phase 7 C++ unwind/LSDA model to the production translation pipeline far enough to detect every selected function that requires a local C++ exception landing bridge, and reject packing before runtime construction until that bridge is actually implemented.

This phase deliberately does **not** claim full exception support. It converts the current isolated `internal/unwind` model into a production requirement and closes the unsafe gap where a structurally valid artifact could otherwise omit required landing semantics.

## 2. Plan audit

### Existing reusable implementation

The repository already contains:
- `.eh_frame` CIE/FDE parsing (`internal/unwind/frame.go`);
- LSDA call-site/action/type parsing (`internal/unwind/lsda.go`);
- original-PC → VM call-site mapping (`MapCallSites`);
- content-addressed invoke-thunk planning (`PlanExceptionBridge`);
- single-call bridge LSDA construction and deferred relocation (`BuildBridgeLSDA`).

The ARM64 translator also already records throw-capable `BL`/`BLR` sites, but it exposes only the call VM offset. Its internal ARM64-offset → VM-offset label map is currently used only to build the bytecode trailer.

### Production gap

`TranslationPreparation` currently carries only SVC, exclusive-region, and FP/SIMD runtime requirements. `runtime.BuildConfig`, `RewritePlan`, and `app.Run` consume only those requirements. No production code consumes `internal/unwind`.

Therefore local C++ landing-pad semantics are not yet a validated runtime capability.

### Correctness risk

A native call that is inside an LSDA range with a nonzero landing pad cannot safely use the ordinary native bridge if an exception may unwind through it. Until the runtime invoke/personality/landing bridge and final FDE/LSDA integration exist, the product must fail closed.

A call-site range with no local landing pad remains an ordinary unwind-through range and does not by itself require a local bridge.

## 3. Corrected implementation plan

### ARM64 translation source map

Add an always-available, sorted source map to `TranslateResult`:

```go
type SourceMapEntry struct {
    ARM64Offset int
    VMOffset    int
}
```

Populate it from the translator label map using the same sorted ARM64 offsets used by the trailer. It must include the function-end label and merged/skipped instruction offsets. This remains independent of debug logging.

### EH preflight model

Add `internal/elf/exception_preflight.go`.

Responsibilities:
1. parse the input `.eh_frame` once for translation preparation;
2. locate a unique FDE that fully covers each selected function that has native call sites;
3. resolve and parse the FDE LSDA by virtual address;
4. map LSDA call-site start/end/landing PCs through the translator source map;
5. convert translator native-call records into `unwind.NativeCallLocation` values;
6. detect whether any call falls in a call-site range with a nonzero local landing pad;
7. only then invoke `unwind.PlanExceptionBridge` and retain the validated plan.

Partial selections, ambiguous FDE coverage, unresolved LSDA addresses, missing source mappings, malformed EH metadata, or missing personality/action data fail closed when they are relevant to a local landing bridge.

### TranslationPreparation

Add a first-class prepared requirement:

```go
type PreparedExceptionBridge struct {
    Selection Selection
    Plan      *unwind.ExceptionBridgePlan
}
```

and `ExceptionBridges []PreparedExceptionBridge`.

Keep the list deterministic by selection address/name.

Add a method that reports a stable product error when any bridge remains pending, including function name and planned invoke-thunk count.

### Production fail-closed points

- `app.Run`: after translation preparation and before `runtime.Build`, reject pending exception bridges. This avoids constructing a runtime that cannot satisfy the requirement.
- `TranslationPreparation.ValidateRuntimeImage`: reject the same pending bridge requirements so callers cannot bypass the CLI path by injecting a runtime image and calling `ProcessAnalyzed` directly.

No runtime or writer change is made in this phase, because accepting a pending bridge before the invoke/personality/landing assembly and final unwind layout exist would be incorrect.

## 4. Tests

### Translator
- source map is sorted;
- contains ordinary instruction offsets, merged exclusive-region offsets, and function end;
- native-call VM offsets are represented by the same source map.

### Exception preflight unit tests
Using explicit CIE/FDE/LSDA models:
- native call in a local landing range produces one prepared bridge plan;
- unwind-through call-site with landing=0 produces no bridge requirement;
- native call outside EH call-site ranges produces no bridge requirement;
- missing landing/source-map mapping fails closed;
- ambiguous/partial FDE coverage fails closed when an EH bridge is needed;
- malformed/missing action/personality data fails through existing `PlanExceptionBridge` validation.

### Production guard
- pending bridge requirement is rejected before runtime capability validation;
- empty bridge set remains accepted.

## 5. Exit criteria

- No selected local C++ landing-pad native call can silently proceed through the ordinary runtime path.
- Existing unwind model is now an actual production requirement rather than dead planning code.
- No release-readiness claim is made.
- No runtime invoke/landing thunk is approximated or stubbed.
- Focused tests/vet pass, then full repository contract/test/race/exact-r29 runtime/vet/macOS ARM64 build passes before merge.

## 6. Next phase after this gate

Phase 14 should consume `PreparedExceptionBridge` to generate invoke/landing assembly, preserve the original Android C++ personality, emit per-thunk FDE/LSDA metadata, and integrate final unwind layout through `RewritePlan`. The physical-device unwinder gate remains mandatory after host integration.
