# VMP Phase 17 — Unwind-Ready Exception Invoke Runtime Thunks

## 1. Action plan

Goal: generate and compile/link auditable AArch64 invoke wrappers for the C++ exception bridge model established by Phases 13–16, while keeping the product-level exception guard fail-closed.

This phase provides runtime artifacts only. It does **not** claim end-to-end C++ exception support and does not remove `ValidateRuntimeRequirements` / `ValidateRuntimeImage` rejection for prepared exception bridges.

## 2. Audit of the prior implementation branch

An earlier `fix/exception-invoke-runtime` branch already contained a useful audited design and repair scripts, but it was based before later Phase 12–16 changes. Its focused workflow failed during patch application because `runtime_test.go` had evolved from two to three exclusive-region fixtures. No runtime source from that failed branch was committed.

Reusable audited findings from that branch:

- use a fixed, explicitly tested invoke wrapper layout;
- preserve BTI/PAC/CFI and generate an FDE for every invoke function;
- preserve the original personality encoding, including `indirect|pcrel|sdata4`;
- use `unwind.BuildBridgeLSDA` rather than reimplement LSDA action/type semantics;
- keep LSDA object symbols distinct from the `vm_invoke_` function namespace so unwind FDE validation cannot confuse data with code;
- avoid non-comparable struct equality in tests;
- keep runtime generation deterministic and non-mutating.

## 3. Corrected implementation plan

### Runtime model

Add `ExceptionInvokeConfig` and `ExceptionInvokeImage` in `internal/runtime/invokegen.go`. Input is one selected function address plus its Phase 13/15 `ExceptionBridgePlan`. Output records personality encoding/anchor, thunk symbol, LSDA symbol and cloned `BridgeLSDA` metadata.

### Generated assembly

For each invoke thunk generate:

- hidden `vm_invoke_<id>` function in executable storage;
- `.cfi_startproc`, `.cfi_personality`, `.cfi_lsda`;
- BTI entry and PACIASP/AUTIASP-balanced frame;
- saved X29/X30/X19 with explicit CFI;
- call through the existing `vm_native_call` bridge;
- a normal return discriminator and a landing label that preserves unwinder X0/X1 into VM architectural state;
- generated LSDA bytes in a dedicated read-only exception-table section;
- a separate personality anchor object namespace.

The wrapper layout is fixed and must match the `InvokeThunkLayout` passed to `BuildBridgeLSDA`.

### Runtime build integration

Extend `runtime.BuildConfig` and `runtime.Image` with exception-invoke metadata. Generate `vm_invoke.S`, compile it with the same exact-NDK/PAC/BTI flags as other runtime assembly, and link it into `runtime.o`.

Extend runtime unwind validation so **function** symbols with the `vm_invoke_` prefix require FDE relocation coverage. LSDA objects must not be included in the function-FDE rule.

### Phase 16 compatibility

Phase 16 now provides stable `PreparedExceptionRoute` records for the later writer/runtime routing stage. This phase intentionally does not consume or expose those routes yet; it establishes a linkable unwind landing frame and LSDA first. A later integration phase will bind prepared final VM routes into the invoke/landing continuation path.

### Current-baseline correction

The exact-r29 runtime test now contains **three** exclusive-region fixtures (including pair-exclusive coverage), not two. Phase 17 patches and expected counts must preserve all three.

## 4. Verification

Focused gate:

- `go test -count=1 ./internal/unwind ./internal/runtime`
- `go vet ./internal/unwind ./internal/runtime`
- generator determinism and input non-mutation;
- LSDA bytes/relocations equal `BuildBridgeLSDA` output;
- direct and indirect personality encodings;
- duplicate thunk/call identity rejection;
- generated assembly contains BTI/PAC/CFI/personality/LSDA and does not use reserved host register patterns;
- host runtime parser sees invoke FDEs;
- exact-r29 build test compiles and links a real invoke wrapper and reports it in `runtime.Image`.

Then PR full gate: contracts, all tests, race, exact-r29 corpus, exact-r29 runtime build, vet and macOS ARM64 CLI.

## 5. Exit criteria

- invoke wrappers and LSDA are real linkable runtime artifacts;
- every generated invoke **function** has unwind FDE coverage;
- both supported personality encodings remain relocatable and auditable;
- current Phase 12 pair-exclusive exact-r29 fixtures remain intact;
- no bootstrap workflow/patch scripts remain in final diff;
- product C++ exception guard remains fail-closed.

## 6. Follow-up

The next phase should connect Phase 16 `PreparedExceptionRoute` to these invoke wrappers and landing continuations, relocate personality/LSDA metadata into final image layout, and only after actual Android unwinder/device validation consider removing the runtime exception guard.