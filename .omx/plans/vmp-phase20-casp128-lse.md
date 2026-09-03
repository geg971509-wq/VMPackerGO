# VMP Phase 20 — CASP Pair Atomic Closure

## 1. Next-stage goal

Remove the exact-NDK-r29 `casp128` intentional fail-closed class by implementing architectural FEAT_LSE compare-and-swap-pair semantics without widening unrelated instruction support or destabilizing the existing scalar atomic transport.

Phase 19 leaves two compiler-derived intentional boundaries: `casp128` and `machine-outliner`. CASP is the higher-priority next target because exact NDK r29 emits CASP/CASPA/CASPL/CASPAL for ordinary `__int128` C11 atomics under the LSE profile.

## 2. Architecture consensus

### 2.1 Encoding and register representation

CASP shares the CAS encoding class but uses NP=0 instead of the scalar-CAS NP=1 form. The relevant fields are:

- size bits [31:30]; CASP supports a pair of 32-bit words (`00`) or a pair of 64-bit doublewords (`01`);
- acquire bit 22;
- Rs bits [20:16], the low compare/result register;
- release bit 15;
- Rn bits [9:5], the address register/SP;
- Rt bits [4:0], the low new-value register.

The second compare/result and new-value registers are implicit contiguous partners `Rs+1` and `Rt+1`. Both pair bases must be even. Therefore CASP does not require adding a fourth explicit data-register field to `vm.Instruction`.

For VMP's existing decoded representation:

- `Rm = Rs` (expected value before execution; old memory value after execution);
- `Rd = Rt` (new value pair source);
- `Rn = address`;
- `Shift = per-register width` (4 or 8 bytes).

### 2.2 Wire-format consensus

Keep `vm.OpAtomic` at its existing fixed 7-byte wire size:

`op | kind | width | order | rd | rn | rm`

Add atomic kind 12 for CASP. For kind 12:

- width is 4 or 8 bytes per pair member;
- `rm/rm+1` is the expected/result pair;
- `rd/rd+1` is the replacement pair;
- `rn` is the address register.

No VM opcode number, opcode size, source-map accounting, or scalar atomic encoding changes are needed.

### 2.3 Native ABI consensus

Do not force pair results through the existing scalar `vm_atomic_native`, whose ABI returns only X0.

Add a dedicated native helper with an AAPCS64 two-register return value, conceptually:

`pair vm_atomic_pair_native(order, width, address, expected_lo, expected_hi, new_lo, new_hi)`

The helper executes only CASP-family instructions and returns the observed old pair in X0/X1. It uses fixed even temporary register pairs for architectural CASP operands and keeps LSE isolated in the native helper exactly as the scalar helper does.

The C VM handler derives the implicit high registers, calls the pair helper, and writes the returned old pair back to `rm/rm+1`.

## 3. Corrected repair plan

### 3.1 Decoder

- add a distinct `CASP` Op;
- add a CASP decoder pattern before/beside scalar CAS, with NP=0 fixed and only architectural CASP size forms accepted;
- decode Rs to `Rm`, Rt to `Rd`, Rn normally;
- set `Shift` to 4 or 8 bytes per pair member;
- preserve memory-order bits for `atomicMemoryOrder`;
- do not decode reserved size forms as CASP.

### 3.2 Product policy

Add a CASP-specific validator rather than weakening generic scalar validation:

- width must be 4 or 8;
- Rn must be a valid X0-X30/SP address register;
- Rs/Rt pair bases must be even and must name real contiguous GPR pairs (no register-31 pair member);
- fail closed on odd or out-of-range pair bases;
- preserve the architecture-defined overlap cases unless exact ISA constraints require rejection; do not invent restrictions merely for implementation convenience;
- exact raw decoding and memory-order bits must remain self-consistent.

### 3.3 Translator

- map CASP to atomic kind 12;
- keep existing 7-byte `OpAtomic` emission;
- encode pair-low registers directly in `rm` and `rd`;
- reuse existing Rn mapping/SP behavior;
- use the CAS memory-order bit extraction (`acquire=bit22`, `release=bit15`);
- make no change to scalar CAS/LDADD/SWP/min/max kinds 0–11.

### 3.4 Runtime handler

For atomic kind 12:

- require width 4 or 8;
- validate even pair bases and ensure both implicit high members are real GPRs;
- read expected pair from `rm/rm+1`;
- read replacement pair from `rd/rd+1`;
- call `vm_atomic_pair_native`;
- write observed old pair back to `rm/rm+1`;
- for 32-bit CASP, zero-extend each returned W result through the existing VM GPR model;
- leave replacement registers unchanged;
- preserve the fixed handler length of 7 bytes.

### 3.5 Native helper

- isolate CASP instructions under `.arch_extension lse`;
- dispatch relaxed/acquire/release/acq_rel to CASP/CASPA/CASPL/CASPAL;
- support both architectural pair widths (W-pair and X-pair), even though exact-r29 `__int128` currently exercises X-pair;
- copy expected and replacement values into fixed even/contiguous scratch pairs before executing CASP;
- return the updated expected pair in X0/X1;
- return deterministic zeroes/fail-safe behavior for invalid helper arguments only after the C handler has already faulted invalid bytecode.

### 3.6 Tests

Add focused tests for:

- all four exact-r29 CASP order variants;
- O0 and O2/Oz exact raw words from the compiler corpus;
- decoder field extraction for Rs/Rt/Rn and implicit pair semantics;
- W-pair and X-pair widths;
- odd Rs/Rt pair bases rejected;
- pair base that would consume register 31 rejected;
- scalar CAS decoding remains distinct;
- atomic order extraction for all four CASP variants;
- emitted `OpAtomic` remains 7 bytes and uses kind 12;
- runtime handler writes old pair only to the expected/result pair;
- native assembly contains CASP/CASPA/CASPL/CASPAL W and X forms with fixed legal register pairs;
- exact-r29 runtime object assembles/links with NDK 29;
- malformed/reserved CASP encodings remain fail-closed.

## 4. Phase 18/19 compiler gate update

Delete only the exact-r29 `casp128` intentional boundary and its raw-word exemption after the real Decoder + whole-function Translator close the LSE `vmp_atomic128` corpus.

Keep `machine-outliner` unchanged.

The exact-r29 compiler gate must fail if any CASP record is still unsupported after Phase 20; it must not retain a fallback CASP whitelist.

## 5. Execution and verification policy

- implement from current verified `main`, never from the old Phase 18 plan branch;
- temporary audit/repair workflows/scripts must be absent from the final diff;
- focused Go tests/vet come first;
- the authoritative merge gate is the current-head/current-`main` macOS Verification using exact NDK `29.0.14206865`;
- required gates: contract checks, Go list, full tests, race, exact-r29 FP/SIMD corpus, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, macOS ARM64 CLI;
- only the exact verified PR head may be squash-merged;
- the resulting `main` push Verification must also pass;
- after that, fast-forward the repository's current default integration ref `fix/call-vm-nested` to the resulting `main` SHA and verify 0 ahead / 0 behind.

## 6. Exit criteria

Phase 20 is complete only when:

- CASP/CASPA/CASPL/CASPAL decode and execute with architectural pair semantics;
- no general `Instruction` expansion or `OpAtomic` wire-size change was introduced;
- scalar atomic behavior and kinds 0–11 are unchanged;
- exact-r29 `casp128` intentional failures are zero and the exemption is removed;
- `machine-outliner` remains the only compiler-derived intentional boundary from Phase 18;
- all release gates pass on the exact PR head and again on merged `main`;
- `main` and `fix/call-vm-nested` end content-identical.