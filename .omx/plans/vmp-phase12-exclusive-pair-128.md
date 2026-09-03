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

### Safety constraints

- A single-register exclusive load must not pair with a pair store, and a pair load must not pair with a single store. ARM documents mismatched register-count pairs as constrained-unpredictable; the product must reject them before runtime.
- Pair load/store element width must match (32-bit elements or 64-bit elements).
- Address register must match and SP remains rejected by the existing thunk policy.
- Pair result/value registers and status register must all fit the existing X0-X15 remap bank together with body operands.
- Body remains branch-free. The retry CBNZ/B.cond must remain outside the thunk; PC-relative relocation is still a separate phase.

## 3. Corrected fix plan

### Shared IR
- Add `Rt2 int` to `vm.Instruction`.
- Initialize `Rt2: -1` in ARM64 decode.
- Add `Rt2` mapping to `applyCommonFields`.
- Keep non-pair code unchanged; no semantic overloading.

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
- Keep single-register behavior exactly as Phase 11.

### Branch-free compiler-body whitelist
Add only operations required by the audited compiler pattern:
- SUBS_REG / SUBS_IMM
- CSEL / CSINC / CSINV / CSNEG

These execute as original native words inside the thunk; the change only declares their GPR bit fields for safe remapping. No memory instruction or branch is added to the body whitelist.

### Tests
- decoder tests for 32-bit and 64-bit pair forms and all four boundary opcodes.
- all four pair ordering boundary combinations lower to one `OpExclusive`.
- reject single/pair mismatches, width mismatch, address mismatch, SP address, nested exclusive loads, and branch body.
- translate a real compiler-style 128-bit branch-free body:
  `LDAXP x9,x8,[x0]; CMP; CSET; CMP; CSET; CSEL; CMP #0; CSEL; CSEL; STLXP`.
- assert thunk remap preserves opcode/order bits and rewrites both Rt/Rt2/status fields.
- keep all existing Phase 11 tests green.

## 4. Exit criteria

- No new VM bytecode opcode or runtime dispatch entry.
- No field overloading in decoded IR.
- Single and pair exclusives cannot be mixed.
- Real compiler-style branch-free 128-bit body validates and plans a thunk.
- Focused tests/vet pass, then full PR contract/test/race/exact-NDK-r29 runtime/vet/macOS ARM64 build passes before merge.

## 5. Explicit non-goals

- CASP/LSE128 direct instruction support.
- Branch relocation inside exclusive regions.
- SVE/SME/MTE.
- Physical Android execution; remains a separate release gate.
