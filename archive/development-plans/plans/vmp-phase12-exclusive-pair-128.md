# VMP Phase 12 — Pair-Exclusive 128-bit Atomic Coverage

## 1. Initial action plan

Goal: extend the already-validated content-addressed exclusive thunk to pair-exclusive A64 operations used by real 128-bit atomic compiler fallbacks, without adding a new VM wire opcode.

Target instructions:
- LDXP / LDAXP (32-bit pair and 64-bit pair forms)
- STXP / STLXP (32-bit pair and 64-bit pair forms)
- all relaxed/acquire × relaxed/release boundary combinations

Target branch-free compiler body:
- SUBS register/immediate (CMP aliases)
- CSEL / CSINC / CSINV / CSNEG (including CSET aliases)

Reference compiler shape to cover:
`LDAXP; CMP/CSET/CSEL...; STLXP`, as emitted for 128-bit atomics when a pair-exclusive fallback is selected.

## 2. Plan audit

### Reusable architecture

- `OpExclusive` identifies exact raw instruction regions and is already independent of the specific exclusive opcode.
- Runtime generation executes remapped raw A64 instructions contiguously, so pair load/store and ordering bits do not require a new handler or bytecode format.
- Register remapping already supports arbitrary bit positions once those positions are declared.

### Blocking model gap found during audit

`vm.Instruction` has only `Rd`, `Rn`, and `Rm`. Pair-exclusive needs a fourth decoded GPR field (`Rt2`) and store-pair additionally uses the existing `Rm` slot for status `Rs`. Encoding Rt2 into `Imm`, `ShiftType`, or another unrelated field would be brittle and misleading.

Corrective design: add an explicit `Rt2 int` field to the shared decoded instruction model, initialize it to -1, teach common field extraction to populate it, and compare/remap it only where architecturally relevant.

### Additional semantic findings from the plan audit

1. ARM documents single-vs-pair exclusive mismatch as CONSTRAINED UNPREDICTABLE. The validator must reject LDXR/LDAXR paired with STXP/STLXP and LDXP/LDAXP paired with STXR/STLXR.
2. Pair-exclusive load with `Rt == Rt2` is CONSTRAINED UNPREDICTABLE. Reject it before thunk generation.
3. Store-exclusive status `Rs` overlapping the store value register(s), or overlapping the base register, is CONSTRAINED UNPREDICTABLE. The active single-register validator currently misses this too, so Phase 12 will correct both single and pair forms.
4. The audited compiler 128-bit body uses `CMP/CSET/CSEL`, which consumes and produces NZCV inside the native thunk. The current exclusive thunk only restores/writes GPR state. Merely adding these body instructions would be unsafe for incoming flags and flags live-out.
5. `vm_ctx_t` already has architectural `FL` with a fixed assembly ABI offset. Therefore the correct repair is to restore `vm->FL` into hardware NZCV before executing the raw exclusive region and write hardware NZCV back to `vm->FL` afterwards. X16 remains the context pointer and X17 is reserved as bridge scratch; guest registers are remapped only into X0-X15.

### Safety constraints

- Pair load/store element width must match (32-bit elements or 64-bit elements).
- Address register must match and SP remains rejected by the existing thunk policy.
- Pair result/value registers and status register must all fit the existing X0-X15 remap bank together with body operands.
- Body remains branch-free. The retry CBNZ/B.cond must remain outside the thunk; PC-relative relocation is still a separate phase.
- No CONSTRAINED UNPREDICTABLE overlap is accepted as a product behavior.

## 3. Corrected fix plan

### Shared IR
- Add `Rt2 int` to `vm.Instruction`.
- Initialize `Rt2: -1` in ARM64 decode.
- Add `Rt2` mapping to `applyCommonFields`.
- Keep non-pair code unchanged; no semantic overloading.
- Correct the stale `ExclusiveRegion` comment so it describes generic load-exclusive/store-exclusive regions rather than the old LDAXR...STLXR-only form.

### Decoder
Add explicit patterns:
- LDXP: mask `0xBFFF8000`, value `0x887F0000`
- LDAXP: mask `0xBFFF8000`, value `0x887F8000`
- STXP: mask `0xBFE08000`, value `0x88200000`
- STLXP: mask `0xBFE08000`, value `0x88208000`

Bit30 selects 32-bit vs 64-bit elements; `Rt2` is bits 14:10.

### Policy
Classify all four pair-exclusive instructions as `dispositionNativeThunk`, never as standalone virtual instructions.

### Exclusive-region validator
- Generalize load/store helpers to include single and pair forms.
- Add pair-arity check before accepting a region.
- Compare raw-decoded Rt2 on boundary instructions.
- Validate/remap Rt2 for pair loads/stores.
- Reject duplicate pair-load destinations.
- Reject store-exclusive status/data and status/base overlap for both old single and new pair forms.
- Keep the existing address/width/SP/remap-capacity checks fail-closed.

### Branch-free compiler-body whitelist
Add only operations required by the audited compiler pattern:
- SUBS_REG / SUBS_IMM
- CSEL / CSINC / CSINV / CSNEG

These execute as original native words inside the thunk; no memory instruction or branch is added to the body whitelist.

### NZCV bridge
- Generated exclusive assembly includes `vm_abi.h`.
- Before raw `.inst` words: load `VM_CTX_FL`, mask to NZCV nibble, shift to architectural bits [31:28], and `msr nzcv`.
- After raw `.inst` words: `mrs nzcv`, shift to the VM nibble, and store to `VM_CTX_FL`.
- This preserves both incoming condition state and flags live-out instead of relying on compiler-specific dead-flag assumptions.

### Tests
- decoder tests for 32-bit and 64-bit pair forms and all four boundary opcodes.
- all four pair ordering boundary combinations lower to one `OpExclusive`.
- reject single/pair mismatches, width mismatch, address mismatch, SP address, duplicate pair-load destinations, store status overlap, nested exclusive loads, and branch body.
- translate a real compiler-style 128-bit branch-free body:
  `LDAXP x9,x8,[x0]; CMP; CSET; CMP; CSET; CSEL; CMP #0; CSEL; CSEL; STLXP`.
- assert thunk remap preserves opcode/order bits and rewrites both Rt/Rt2/status fields.
- assert generated exclusive assembly restores and writes back NZCV through `VM_CTX_FL`.
- extend exact-r29 runtime build coverage with a pair-exclusive region.
- keep all existing Phase 11 tests green.

## 4. Exit criteria

- No new VM bytecode opcode or runtime dispatch entry.
- No field overloading in decoded IR.
- Single and pair exclusives cannot be mixed.
- CONSTRAINED UNPREDICTABLE register overlaps are rejected.
- Real compiler-style branch-free 128-bit body validates and plans a thunk.
- VM FL and hardware NZCV are bridged across exclusive thunks.
- Focused tests/vet pass, then full PR contract/test/race/exact-NDK-r29 runtime/vet/macOS ARM64 build passes before merge.

## 5. Explicit non-goals

- CASP/LSE128 direct instruction support.
- Branch relocation inside exclusive regions.
- SVE/SME/MTE.
- Physical Android execution; remains a separate release gate.
