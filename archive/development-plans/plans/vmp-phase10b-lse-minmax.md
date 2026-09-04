# VMP Phase 10b — Complete Scalar LSE Min/Max RMW Family

## 1. Action plan

Goal: correct the remaining gap in the scalar FEAT_LSE single-register read-modify-write matrix after Phase 10.

Scope:
- LDSMAX / LDSMAXA / LDSMAXL / LDSMAXAL
- LDSMIN / LDSMINA / LDSMINL / LDSMINAL
- LDUMAX / LDUMAXA / LDUMAXL / LDUMAXAL
- LDUMIN / LDUMINA / LDUMINL / LDUMINAL
- byte, halfword, word, and doubleword widths
- no new VM wire opcode

## 2. Plan audit

Phase 10 correctly established the reusable architecture:
- ARM64 decoder patterns are isolated in `decode_lse_atomic.go`.
- `OpAtomic` already transports kind, width, memory order, Rt, Rn, and Rs.
- `h_atomic` already treats kinds >= 4 as load-return RMW operations.
- `vm_atomic_native` already isolates FEAT_LSE assembly and selects relaxed/acquire/release/acq_rel variants.
- acquire suppression for load-return RMW with `Rt == XZR` is already centralized.

The earlier wording "complete scalar LSE atomic RMW coverage" was too broad. The signed/unsigned min/max four families are still absent. This phase explicitly corrects that gap rather than treating the previous green CI as architectural completeness.

## 3. Corrected implementation plan

### Decoder
Add semantic ops and decoder patterns using the same LSE encoding class:
- LDSMAX: opcode field 0x4
- LDSMIN: opcode field 0x5
- LDUMAX: opcode field 0x6
- LDUMIN: opcode field 0x7

Preserve size, A/R, Rs, Rn, and Rt extraction through the existing common post-processing.

### Policy and translator
- Whitelist all four through the existing atomic validator.
- Extend `isLoadReturnLSE` so XZR acquire suppression applies uniformly.
- Extend `OpAtomic.kind` internally: 8 LDSMAX, 9 LDSMIN, 10 LDUMAX, 11 LDUMIN.
- Do not change `internal/vm/opcodes.go` or the wire-size contract.

### Runtime
- Accept atomic kinds through 11.
- Keep the existing seven-byte handler format.
- Extend `vm_atomic_native` with the four min/max operations for 1/2/4/8 byte widths and all four memory-order variants.

### Verification
- Decoder tests across all four widths for each new family.
- Exact bytecode kind/width/order/register tests.
- Extend XZR acquire-suppression regression to all load-return LSE RMW families.
- Extend invalid-width fail-closed tests.
- Run focused Go tests/vet, then the repository PR verification: contracts, full tests, race, exact NDK r29 corpus/runtime assembly build, vet, and macOS ARM64 CLI build.

## 4. Explicit non-goals

- CASP / FEAT_LSE128 and other pair/128-bit atomics: require pair-value transport and a separate ABI design.
- SVE/SME/MTE: require architectural state or memory-tag semantics beyond scalar LSE.
- Newer unprivileged LSE/LRCPC families: audit separately against actual compiler/runtime corpus before adding them.
- Physical Android execution: remains a device gate and must not be inferred from host or cross-build success.

## 5. Exit criteria

- The core scalar single-register FEAT_LSE RMW set represented by the existing `OpAtomic` shape is closed for ADD/CLR/EOR/SET/SMAX/SMIN/UMAX/UMIN/SWP plus CAS.
- No new VM wire opcode or bytecode width is introduced.
- Exact NDK r29 runtime assembly accepts every added mnemonic.
- Full PR verification is green before merge.
