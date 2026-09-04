# VMP Phase 15 — Relocatable C++ Personality References

## 1. Initial action plan

Goal: make the existing exception preflight/modeling path consume Android/Clang CIE personality references that use `DW_EH_PE_indirect`, without pretending the host can or should dereference the personality function during packing.

Also retain the original landing-pad PC in each `InvokeThunk`, because the final runtime bridge must route through the final bytecode trailer map after reverse/encryption instead of relying on pre-finalization VM offsets.

## 2. Plan audit

### Real inconsistency found

`internal/unwind/lsda.go` already handles indirect type-info correctly for a relocatable packer: it clears `PEIndirect` while decoding the encoded reference address, and separately records `TypeInfo.Indirect=true`.

`internal/unwind/frame.go` does not follow that model for CIE personality references. It passes the full personality encoding into `DecodePointer`, whose explicit contract rejects `PEIndirect` without target-memory access.

For Android/Clang C++ this is the wrong production boundary. A common CIE personality encoding is `PEIndirect | PEPcrel | PESdata4`: the encoded value names a target-local pointer slot (for example a DW.ref/GOT-style personality reference). The rewritten CIE can preserve the same indirect semantics by pointing at the same slot; the packer does not need the external runtime personality function address.

### Correct semantic model

For `CIE.Personality`:
- if `PersonalityEncoding` has `PEIndirect`, `Personality` stores the resolved address of the indirection slot;
- the encoding retains the `PEIndirect` bit;
- callers rebuilding an EH reference emit the same encoding and target the stored slot address.

This matches the existing `TypeInfo{Address, Indirect}` approach and avoids unsafe/dynamic-memory dereference assumptions.

### Stable landing identity

`InvokeThunk.VMLandingPad` is a translation-phase VM offset. Final bytecode reversal remaps it later, so it is unsuitable as a runtime-stable routing identity.

Add `OriginalLandingPad uint64` from the original LSDA call-site. The future landing trampoline can compute `arm64_off = OriginalLandingPad - vm->func_addr` and look up the final continuation boundary in the already-remapped trailer `addr_map`.

## 3. Corrected fix plan

### `internal/unwind/frame.go`
- decode CIE personality using `PersonalityEncoding &^ PEIndirect`;
- keep the original encoding unchanged;
- add an explicit comment documenting `Personality` as the encoded reference target/slot when indirect;
- continue to fail closed for unsupported format/application encodings.

Do not change the public `DecodePointer` contract: direct callers that ask it to resolve an indirect pointer without memory access must still fail. This prevents accidental semantic broadening in `.eh_frame_hdr`, LPStart, or other callers.

### `internal/unwind/bridge.go`
Add:

```go
type InvokeThunk struct {
    ...
    OriginalLandingPad uint64
}
```

Populate it from the matched original `CallSite.LandingPad` and include it in the content-addressed thunk ID input.

Validation:
- nonzero local landing is already required for bridge creation;
- `OriginalLandingPad` must therefore be nonzero on every generated invoke thunk.

### Tests
- parse a CIE with `zPR` / personality `0x9b` and prove the stored personality is the pcrel indirection-slot address while encoding remains `0x9b`;
- existing `DecodePointer(... PEIndirect ...)` test continues to reject direct dereference without memory access;
- exception bridge plan retains original landing PC and changing landing PC changes thunk ID;
- `BuildBridgeLSDA` behavior remains unchanged because it consumes action/type metadata, not the runtime landing identity.

## 4. Production consequence

Phase 13 preflight can now plan common indirect-personality C++ metadata instead of rejecting it at CIE parse time. It still fails closed later because runtime invoke/landing integration is not complete.

No exception support is enabled in this phase; this only repairs the metadata model needed by the next runtime/writer slice.

## 5. Exit criteria

- common indirect CIE personality references are modeled relocatably;
- no target-memory dereference or external personality-address guess is introduced;
- invoke plans carry a runtime-stable original landing identity;
- focused unwind/elf tests and vet pass;
- full exact-r29 repository gates pass before merge.
