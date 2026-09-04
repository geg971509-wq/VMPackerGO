# VMP Phase 17 — Unwind-Ready Exception Invoke Runtime Thunks

## 1. Action plan

Goal: generate and compile/link auditable AArch64 invoke wrappers for the C++ exception bridge model established by Phases 13–16, while keeping the product-level exception guard fail-closed.

This phase provides runtime artifacts only. It does **not** claim end-to-end C++ exception support and does not remove `ValidateRuntimeRequirements` / `ValidateRuntimeImage` rejection for prepared exception bridges.

## 2. Audit of the prior implementation branch

An earlier `fix/exception-invoke-runtime` branch contained a useful audited design and repair scripts, but it was based before later Phase 12–16 changes. Its focused workflow failed during patch application because `runtime_test.go` had evolved from two to three exclusive-region fixtures. No runtime source from that failed branch was committed.

Reusable audited findings from that branch:

- use a fixed, explicitly tested invoke wrapper layout;
- preserve BTI/PAC/CFI and generate an FDE for every invoke function;
- retain the original personality address and source encoding as metadata;
- use `unwind.BuildBridgeLSDA` rather than reimplement LSDA action/type semantics;
- keep LSDA object symbols distinct from the `vm_invoke_` function namespace so unwind FDE validation cannot confuse data with code;
- avoid non-comparable struct equality in tests;
- keep runtime generation deterministic and non-mutating.

## 3. Corrected implementation plan

### Runtime model

Add `ExceptionInvokeConfig` and `ExceptionInvokeImage` in `internal/runtime/invokegen.go`. Input is one selected function address plus its Phase 13/15 `ExceptionBridgePlan`. Output records the original personality address/encoding, the emitted CFI encoding, personality slot symbol, thunk symbol, LSDA symbol and cloned `BridgeLSDA` metadata.

### Personality relocation model

The first Phase 17 implementation incorrectly used a local one-byte anchor as the target of `.cfi_personality`. That is not a valid direct personality target, and one byte cannot hold an AArch64 personality pointer for indirect encoding.

The corrected model is deliberately uniform:

- accept the two source encodings already modeled by the preflight: `pcrel|sdata4` and `indirect|pcrel|sdata4`;
- retain that source encoding in `ExceptionInvokeImage.PersonalityEncoding` for auditability;
- always emit the generated CIE personality reference as `indirect|pcrel|sdata4`;
- emit one aligned **8-byte** `vm_personality_anchor_<function>` pointer slot initialized to zero;
- retain the real original personality VA in `ExceptionInvokeImage.Personality`;
- keep the product exception guard fail-closed until the final writer patches that slot to the real personality VA.

Normalizing both source encodings to one generated indirect slot avoids pretending a local object address is the original direct personality and gives the later finalizer one explicit relocation contract.

Duplicate exception plans for one selected function are rejected so generated personality-slot symbols cannot collide.

### Generated assembly

For each invoke thunk generate:

- hidden `vm_invoke_<id>` function in executable storage;
- `.cfi_startproc`, normalized `.cfi_personality`, `.cfi_lsda`;
- BTI entry and PACIASP/AUTIASP-balanced frame;
- saved X29/X30/X19 with explicit CFI;
- call through the existing `vm_native_call` bridge;
- a normal return discriminator and a landing label that preserves unwinder X0/X1 into VM architectural state;
- generated LSDA bytes in a dedicated read-only exception-table section;
- a separate aligned 8-byte personality pointer-slot namespace.

The wrapper layout is fixed and must match the `InvokeThunkLayout` passed to `BuildBridgeLSDA`.

### Runtime build integration

Extend `runtime.BuildConfig` and `runtime.Image` with exception-invoke metadata. Generate `vm_invoke.S`, compile it with the same exact-NDK/PAC/BTI flags as other runtime assembly, and link it into `runtime.o`.

Extend runtime unwind validation so **function** symbols with the `vm_invoke_` prefix require FDE relocation coverage. LSDA objects must not be included in the function-FDE rule. Generated invoke/personality/LSDA symbols must have the expected ELF types, sizes and executable/non-executable section properties.

### Phase 16 compatibility

Phase 16 now provides stable `PreparedExceptionRoute` records for the later writer/runtime routing stage. This phase intentionally does not consume or expose those routes yet; it establishes a linkable unwind landing frame and LSDA first. A later integration phase will bind prepared final VM routes into the invoke/landing continuation path and patch personality/LSDA relocation metadata.

### Current-baseline correction

The exact-r29 runtime test now contains **three** exclusive-region fixtures (including pair-exclusive coverage), not two. Phase 17 patches and expected counts must preserve all three.

## 4. Verification

Focused gate:

- `go test -count=1 ./internal/unwind ./internal/runtime`
- `go vet ./internal/unwind ./internal/runtime`
- generator determinism and input non-mutation;
- LSDA bytes/relocations equal `BuildBridgeLSDA` output;
- both accepted source personality encodings normalize to emitted `0x9b` CFI through an 8-byte pointer slot;
- original source encoding and personality VA remain in runtime metadata;
- duplicate function/thunk/call identity rejection;
- generated assembly contains BTI/PAC/CFI/personality/LSDA and does not use reserved host register patterns;
- host runtime parser sees invoke FDEs and validates generated symbol classes;
- exact-r29 build test compiles and links a real invoke wrapper and reports it in `runtime.Image`.

Then PR full gate: contracts, all tests, race, exact-r29 corpus, exact-r29 runtime build, vet and macOS ARM64 CLI.

## 5. Exit criteria

- invoke wrappers and LSDA are real linkable runtime artifacts;
- every generated invoke **function** has unwind FDE coverage;
- accepted source personality encodings are retained for audit but emitted through a correct, uniform indirect pointer slot;
- every personality slot is exactly 8 bytes and remains fail-closed/unpatched until final rewrite integration;
- current Phase 12 pair-exclusive exact-r29 fixtures remain intact;
- no bootstrap workflow/patch scripts remain in final diff;
- product C++ exception guard remains fail-closed.

## 6. Follow-up

The next phase should connect Phase 16 `PreparedExceptionRoute` to these invoke wrappers and landing continuations, patch each personality slot and LSDA type relocation in the final image layout, and only after actual Android unwinder/device validation consider removing the runtime exception guard.
