# VMP Phase 16 — Exception Invoke Runtime Thunks

## 1. Initial action plan

Goal: make the runtime builder generate real per-call invoke wrappers with AArch64 PAC/BTI/CFI, original C++ personality metadata, and bridge LSDA payloads, while keeping the Phase 13 production guard in place until the final writer/handler routing is integrated.

This isolates exact-NDK-r29 assembler/ELF/CFI risk from the later behavior-enabling patch.

## 2. Plan audit

### Reusable architecture

- `vm_native_call(vm, target)` already owns the full AAPCS64/native-stack bridge and has unwind-safe CFI whose CFA remains on the saved host frame while SP temporarily points at the VM guest stack.
- A per-call wrapper therefore only needs to preserve one callee-saved register for `vm*`, call `vm_native_call`, expose a unique personality+LSDA call site, and on landing store AArch64 exception state X0/X1 into VM R0/R1.
- `BuildBridgeLSDA` already emits the action/type/filter payload with deferred relocations.
- Phase 15 now preserves relocatable personality references and the original landing PC.
- Phase 14 indexes runtime FDEs in targets with an existing GNU EH index.

### Fixed wrapper layout

Use a deliberately fixed AArch64 instruction layout (all 4-byte instructions):

- 0: `bti c`
- 4: `paciasp`
- 8: `stp x29,x30,[sp,#-32]!`
- 12: `str x19,[sp,#16]`
- 16: `mov x29,sp`
- 20: `mov x19,x0`
- 24: `bl vm_native_call` ← LSDA call site (length 4)
- 28: `mov w0,#0` normal completion result
- 32: branch to common return
- 36: landing: `stp x0,x1,[x19,#VM_CTX_R]`
- 40: `mov w0,#1` landing result
- 44: restore x19
- 48: restore x29/x30 and SP
- 52: `autiasp`
- 56: `ret`
- total range: 60 bytes

`InvokeThunkLayout{CallOffset:24, CallLength:4, LandingOffset:36, RangeLength:60}` is therefore authoritative and unit-tested against generated assembly tokens; exact-r29 build is the assembler truth gate.

### Personality strategy

Do not reference the final target personality directly from the runtime ET_REL. Instead each bridge plan gets a defined local/global hidden placeholder symbol `vm_personality_anchor_<id>`. `.cfi_personality` uses the original encoding but initially targets that anchor. The final writer will later patch the CIE personality field from the anchor VA to `ExceptionBridgePlan.Personality`, preserving the original direct/indirect encoding.

Phase 16 only supports personality encodings the final writer can patch safely in the next phase:
- `pcrel|sdata4` (`0x1b`)
- `indirect|pcrel|sdata4` (`0x9b`)

Other encodings remain fail-closed.

### LSDA strategy

For every `InvokeThunk`:
- call `BuildBridgeLSDA` using the fixed wrapper layout;
- emit its raw placeholder bytes under a symbol `vm_invoke_lsda_<id>` in an allocatable read-only section;
- `.cfi_lsda 0x1b, vm_invoke_lsda_<id>` lets the assembler/linker generate the FDE LSDA reference;
- retain the `BridgeLSDA` relocation model in runtime `Image` metadata for final writer materialization.

## 3. Corrected implementation plan

### Runtime types

Add:

```go
type ExceptionInvokeConfig struct {
    FunctionAddress uint64
    Plan            *unwind.ExceptionBridgePlan
}

type ExceptionInvokeImage struct {
    FunctionAddress    uint64
    Personality        uint64
    PersonalityEncoding byte
    PersonalityAnchor string
    Thunk              unwind.InvokeThunk
    ThunkSymbol        string
    LSDASymbol         string
    LSDA               *unwind.BridgeLSDA
}
```

`BuildConfig.ExceptionInvokes []ExceptionInvokeConfig` and `Image.ExceptionInvokes []ExceptionInvokeImage`.

The generator clones/normalizes metadata and never mutates the preparation plan.

### Runtime generator

Add `internal/runtime/invokegen.go` returning generated assembly plus normalized image metadata.

Validation:
- plan/personality present;
- supported personality encoding only;
- nonzero unique thunk IDs / original PCs / original landing PCs;
- deterministic sort by FunctionAddress, OriginalPC, ID;
- duplicate IDs or call identities reject;
- `BuildBridgeLSDA` must succeed for every thunk.

Generated assembly includes:
- one personality anchor per source plan;
- one exact LSDA blob per thunk;
- one `vm_invoke_<id>` wrapper per thunk;
- `.cfi_personality`, `.cfi_lsda`, PAC/BTI and unwind directives;
- AArch64 GNU property note.

### Runtime build

- write/compile `vm_invoke.S` alongside existing SVC/exclusive/FP-SIMD generated objects;
- link into `runtime.o`;
- store normalized invoke metadata on `Image` after `ParseImage`;
- `validateRuntimeUnwind` requires FDE coverage for `vm_invoke_` symbols.

No handler uses these wrappers in Phase 16. Production pending-bridge guard remains unchanged.

## 4. Tests

- generator deterministic ordering and no mutation;
- assembly contains expected wrapper labels, PAC/BTI/CFI, personality/LSDA directives, `bl vm_native_call`, X0/X1→VM R0/R1 landing store, and exact fixed layout semantics;
- duplicate IDs/calls and unsupported personality encodings reject;
- generated LSDA metadata equals `BuildBridgeLSDA` output and retains type-info relocations;
- runtime parser/unwind validation recognizes invoke FDE symbols;
- exact-r29 runtime integration compiles and links at least one `0x9b` wrapper and returns a runtime image containing invoke metadata + FDE relocation.

## 5. Exit criteria

- exact-r29 assembler/linker proves the wrapper/CFI/personality/LSDA shape is valid;
- runtime image carries enough normalized metadata for the final writer to patch personality and type-info relocations;
- no production exception behavior is enabled yet;
- existing runtime features remain unchanged;
- full repository gates pass before merge.

## 6. Next phase

Phase 17 will classify direct external BL exception sites, feed eligible plans into runtime build, materialize personality/LSDA references after final layout, route the call handler through the wrapper, map `OriginalLandingPad` through final `addr_map`, and relax the Phase 13 guard only for that validated subset. BLR and packed-to-packed exception propagation remain fail-closed until separately implemented.
