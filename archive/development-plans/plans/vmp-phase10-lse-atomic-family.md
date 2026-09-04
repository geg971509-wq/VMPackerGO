# VMP Phase 10 — LSE Atomic Family Completion

## 1. Initial action plan

Goal: extend the existing ARM64 VM path with high-value LSE atomics without widening the VM ISA or changing publication/ELF ownership.

1. Inventory the existing ARM64 decoder, product whitelist, translator, `OpAtomic` wire format, Android runtime helper, and semantic tests.
2. Rank uncovered instructions by compiler frequency, implementation reuse, device availability, and testability.
3. Implement one closed tranche only: SWP, LDCLR, LDEOR, LDSET, including byte/halfword/word/doubleword and relaxed/acquire/release/acq_rel encodings.
4. Preserve the existing 7-byte `OpAtomic` representation and extend only its internal `kind` field.
5. Add decoder + translation + runtime tests; run full Go tests, race/vet/build/contracts, and Android cross-build gates available in CI.
6. Merge only after the implementation branch is green. Keep device-only validation explicitly open if no physical Android device is attached.

## 2. Plan audit

### Existing strengths

- `OpAtomic` already carries `kind`, width, order, destination, address, and source registers.
- `trAtomic` already normalizes XZR and memory-order bits.
- `vm_atomic_native` already isolates FEAT_LSE instructions in one native helper, so adding additional LSE operations does not expand the VM bytecode surface.
- The decoder already accepts all four widths for LDADD/CAS, and semantic tests already inspect exact `OpAtomic` operands.

### Findings that change the initial plan

1. Do **not** add a new VM opcode per ARM instruction. Extend the existing atomic kind space instead; this avoids needless bytecode churn.
2. Fix an existing ordering accuracy issue while touching this path: for load-return LSE operations (LDADD/SWP/LDCLR/LDEOR/LDSET), architectural acquire semantics are suppressed when `Rt == XZR`. The translator must not encode acquire in that case. CAS keeps its existing order handling.
3. Do **not** include CASP/LSE128 in this phase. It needs pair-register/128-bit value transport and materially widens the runtime ABI.
4. Do **not** include SVE/SME/MTE here. Those require new architectural state or memory-tag semantics and are not incremental atomic work.
5. Hardware execution remains gated by FEAT_LSE exactly as the already-supported LDADD/CAS path is today; this phase does not introduce a new baseline hardware assumption.

## 3. Corrected fix plan

### Decoder
- Add semantic ops `SWP`, `LDCLR`, `LDEOR`, `LDSET`.
- Add LSE patterns using the existing LDADD encoding class and preserve `A/R`, width, `Rs`, `Rn`, `Rt` fields.
- Reuse one common post-processor for atomic RMW operations.

### Product policy
- Whitelist the four new ops through `validateAtomicNative`.
- Preserve width and register validation; no silent fallback.

### Translator
- Route the four ops to `trAtomic`.
- Expand atomic kinds: 0 LDAR, 1 STLR, 2 LDADD, 3 CAS, 4 SWP, 5 LDCLR, 6 LDEOR, 7 LDSET.
- Encode acquire suppression for load-return LSE ops when destination is XZR.

### Runtime
- Extend `h_atomic` kind validation to 0..7.
- Reuse the current native helper ABI.
- Add width/order variants for `swp`, `ldclr`, `ldeor`, and `ldset` using the same order-selection macro as LDADD.

### Tests
- Decoder raw-word tests for all four new families.
- Exact bytecode operand tests for operation kind, width, A/R order and XZR handling.
- Regression test that `LDADDA ... , XZR` does not encode acquire.
- Runtime source/generation test or Android cross-build to ensure all instruction mnemonics assemble.

## 4. Exit criteria

- No new VM wire opcode is introduced.
- Existing LDAR/STLR/LDADD/CAS tests remain green.
- New SWP/LDCLR/LDEOR/LDSET tests are green.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, project build/contracts and CI gates pass where supported.
- Any unavailable physical-device execution is reported as an open release gate, not treated as success.
