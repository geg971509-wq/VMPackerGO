# VMP Phase 11 — Exclusive Ordering Coverage

## 1. Action plan

Goal: cover the non-acquire/release A64 exclusive primitives that real relaxed atomic loops use, while preserving the existing closed native-thunk design.

Scope:
- LDXR / LDXRB / LDXRH
- STXR / STXRB / STXRH
- all four closed boundary combinations:
  - LDXR ... STXR
  - LDXR ... STLXR
  - LDAXR ... STXR
  - LDAXR ... STLXR
- 1/2/4/8-byte widths already represented by the decoder's shared `size` field
- no new VM wire opcode

## 2. Plan audit

Existing exclusive support has the correct architecture for this extension:
- `OpExclusive` identifies a content-addressed region, not a specific acquire/release flavor.
- the runtime thunk emits the exact rewritten A64 instruction words contiguously, so the PE exclusive monitor is not broken by interpreter activity.
- register remapping is generic once the load/store boundary instructions are recognized.
- unsafe bodies, SP address use, unclosed regions, and regions using too many guest registers already fail closed.

The current implementation unnecessarily hard-codes one ordering pair: LDAXR as the only start and STLXR as the only end. Relaxed and mixed C/C++/Rust atomic orderings can use LDXR/STXR variants, so this is a real compiler-facing gap.

## 3. Corrected fix plan

### Decoder
Add LDXR and STXR patterns next to LDAXR/STLXR, preserving the same decoded width/register shape.

### Policy
Classify LDXR/STXR as `dispositionNativeThunk`, exactly like LDAXR/STLXR. Standalone execution remains rejected; only a validated closed region may bypass the per-instruction policy.

### Translator / region validator
- Treat LDXR and LDAXR as legal exclusive-load starts.
- Treat STXR and STLXR as legal exclusive-store ends.
- Reject nested starts of either load flavor.
- Require matching address register and access width across the chosen start/end pair.
- Keep the body branch-free whitelist unchanged.
- Extend register-field remapping for both new instructions.

### Runtime
No runtime-format change. `generateExclusiveThunks` already emits patched raw words, so LDXR/STXR memory ordering remains architectural rather than re-encoded in the VM.

### Tests
- decoder recognition for LDXR/STXR across widths.
- all four boundary ordering combinations lower to one `OpExclusive`.
- raw thunk patching preserves the selected start/end opcodes.
- standalone STXR/STLXR, nested mixed loads, unsafe branch bodies, SP address, and width/address mismatch remain fail-closed.
- full PR verification after focused tests.

## 4. Explicit non-goals

- Branches inside the exclusive region. PC-relative branch relocation inside generated thunks needs a separate design and is not safe to smuggle into this change.
- LDXP/STXP pair-exclusive operations. They require pair-register validation/remapping and dedicated tests.
- CASP/LSE128/SVE/SME/MTE.
- Any claim of physical-device readiness without actual Android execution.

## 5. Exit criteria

- Existing LDAXR...STLXR behavior remains green.
- LDXR/STXR and mixed ordering pairs are validated and emitted through the same continuous native thunk.
- No new VM wire opcode or runtime handler is introduced.
- Full contracts/tests/race/exact-r29 runtime build/vet/macOS ARM64 CLI verification passes before merge.
