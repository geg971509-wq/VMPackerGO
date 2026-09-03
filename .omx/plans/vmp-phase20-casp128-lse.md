# VMP Phase 20 — CASP Pair Atomic Closure

## 1. Goal

Remove the exact-NDK-r29 `casp128` intentional fail-closed class by implementing FEAT_LSE compare-and-swap-pair semantics without widening unrelated instruction support or changing the seven-byte `OpAtomic` wire contract.

Phase 19 left `casp128` and `machine-outliner` as the two compiler-derived intentional boundaries. CASP is addressed first because exact NDK r29 emits CASP/CASPA/CASPL/CASPAL for ordinary `__int128` atomics under the LSE profile.

## 2. Final architecture consensus

### 2.1 Decoder representation

CASP shares the compare-and-swap semantic family with scalar CAS. The decoder therefore keeps `Op == CAS` and distinguishes pair CAS with the architectural raw encoding:

- CASP pattern mask/value: `0xBFA07C00 / 0x08207C00`;
- size `00` = W-register pair, 4-byte members;
- size `01` = X-register pair, 8-byte members;
- acquire = bit 22;
- release = bit 15;
- `Rm = Rs`, the expected/result pair low register;
- `Rd = Rt`, the replacement pair low register;
- `Rn` = address/SP;
- high members are implicit `Rm+1` and `Rd+1`.

This avoids adding a synthetic fourth data register or a new generic instruction enum solely for an encoding variant. A dedicated `isCASPPair` predicate keeps the distinction explicit wherever pair-specific policy or transport is required.

### 2.2 Product policy

CASP overrides only CAS validation through a small pair-specific validator:

- width must be 4 or 8 bytes per pair member;
- address register may be X0-X30/SP;
- expected/result and replacement pair lows must be even;
- supported pair lows are conservatively bounded to X0-X28 so the implicit high member never becomes encoding 31;
- scalar CAS continues through the existing generic atomic validator unchanged.

The X30/X31 boundary remains fail-closed until independent architecture/assembler evidence proves the intended register-31 semantics. Exact-r29 compiler evidence does not require that relaxation.

### 2.3 Wire transport

`OpAtomic` remains exactly seven bytes:

`op | kind | width | order | rd | rn | rm`

Kinds 0-11 remain unchanged. Pair CAS uses kind 12:

- `rm/rm+1` = expected/result pair;
- `rd/rd+1` = replacement pair;
- `rn` = address;
- width = 4 or 8 bytes per member;
- order = relaxed/acquire/release/acq_rel.

No opcode-number, opcode-size, source-map or trailer change is introduced.

### 2.4 Runtime ABI

A dedicated AAPCS64 helper returns the observed pair in X0/X1:

`vm_atomic_pair_native(order, width, address, expected_lo, expected_hi, new_lo, new_hi)`

The helper uses fixed legal even/odd scratch pairs X8/X9 and X10/X11 and executes only CASP-family instructions under `.arch_extension lse`. The VM handler validates kind/width/register pairs/alignment before the native call and writes only the observed old pair back to `rm/rm+1`; replacement registers remain unchanged.

## 3. Repair plan executed

1. Add explicit CASP raw pattern before the scalar LSE patterns while retaining CAS semantic Op.
2. Add `isCASPPair` and pair width decoding.
3. Add isolated `casp_policy.go` so scalar atomic policy is not weakened.
4. Select `OpAtomic` kind 12 from `trAtomic` only for CASP raw encodings.
5. Add pair return ABI and runtime handler branch for kind 12.
6. Add W/X CASP/CASPA/CASPL/CASPAL native helper forms.
7. Remove the `casp128` exact-r29 exemption and stale expectation; retain only `machine-outliner`.
8. Add exact-r29/O0/O2/Oz CASP tests plus W-pair, malformed pair and wire-size tests.
9. Add regression coverage for real STP/LDP signed-offset raw words that must keep `WB=2` as non-writeback addressing.
10. Remove all temporary Phase 20 workflows and patch scripts before PR review.

## 4. Verification policy

The authoritative gate is the repository's existing PR `Verification` workflow on the exact candidate head and current `main`. It must pass:

- contract checks;
- exact host/Android NDK `29.0.14206865` checks;
- `go list ./...`;
- full Go tests;
- race tests;
- exact-r29 FP/SIMD corpus;
- exact-r29 whole-compiler corpus with no `casp128` allowance;
- exact-r29 runtime build/validation;
- `go vet ./...`;
- macOS ARM64 CLI build.

Only the exact verified head may be squash-merged. The resulting `main` push must pass the same Verification again. After that, `fix/call-vm-nested` is fast-forwarded to the exact `main` SHA and compared for zero ahead/behind.

## 5. Exit criteria

Phase 20 is complete only when:

- compiler-emitted CASP/CASPA/CASPL/CASPAL close through Decoder -> policy -> Translator -> runtime;
- `OpAtomic` remains seven bytes and scalar kinds 0-11 are unchanged;
- the `casp128` intentional class is gone rather than broadened or renamed;
- `machine-outliner` remains the only exact-r29 compiler-derived intentional boundary;
- real signed-offset STP/LDP encodings remain accepted as `WB=2` non-writeback pairs;
- temporary implementation/audit files are absent from the final diff;
- exact-head PR Verification and post-merge `main` Verification both pass;
- `main` and `fix/call-vm-nested` finish content-identical.
