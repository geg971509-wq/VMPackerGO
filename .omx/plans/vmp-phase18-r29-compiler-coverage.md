# VMP Phase 18 — Exact NDK r29 Compiler Coverage Gate

## 1. Next-stage plan

Goal: replace incremental opcode guessing with a reproducible compiler-derived ARM64 product coverage gate using the exact Android NDK r29 toolchain already required by release verification.

The gate compiles representative freestanding C/C11 at `-O0`, `-O2`, and `-Oz`, under both baseline Armv8-A and LSE-enabled profiles, then feeds every emitted instruction through the real product decoder, policy, and whole-function translator.

The gate distinguishes three states:

1. **supported/closed** — the complete function translates with no unsupported instruction and all generated side metadata validates;
2. **intentional fail-closed** — compiler output is deliberately outside current product scope and is covered by an explicit, reviewed exact expectation;
3. **unexpected gap** — compiler output is unknown, rejected unexpectedly, or cannot close through the current translator/runtime model.

The first implementation intentionally starts with no broad expectation whitelist. Real exact-r29 evidence must be collected before anything is classified as intentionally unsupported.

## 2. Consensus audit

The older `fix/compiler-r29-coverage` branch contains only a useful planning document and predates later Phase 13–17 work. It must not be merged or used as an implementation base.

The corrected implementation starts from current `main` and is renumbered Phase 18 because Phase 13 is already the production exception-preflight phase.

Architecture findings:

- `internal/corpus` owns the existing 85-demo manifest and should remain architecture-neutral;
- the exact compiler verifier belongs in `internal/arch/arm64` test code, where it can use the unexported product policy without exporting a new API;
- `Decoder.Decode` and `Translator.Translate` are the authoritative behavior; no second opcode table is permitted;
- exclusive instructions are native-thunk-only individually, so only whole-function translation can prove a closed load-exclusive…store-exclusive region;
- FP/SIMD must continue through `ValidateFPSIMDInstruction` and the exact-r29 whitelist path;
- generated TSV/disassembly is temporary CI evidence and must not be committed.

## 3. Corrected fix plan

### Compiler corpus

Add `internal/corpus/testdata/compiler_r29.c` with `noinline, used` freestanding functions covering:

- signed/unsigned 32/64-bit arithmetic, bitwise operations, shifts and rotates;
- comparisons, ternary select, min/max idioms;
- if/else, loops, switch, direct and indirect calls;
- byte/half/word/dword and signed loads/stores, indexed traffic, pair/struct traffic and stack locals;
- widening/truncation, multiply-add and high-multiply idioms using `__int128`;
- ABI pressure with more than eight arguments and multiple live locals;
- C11/Clang atomics for 8/16/32/64-bit load/store/fetch-add/exchange/compare-exchange under relaxed/acquire/release/acq_rel/seq_cst orders;
- 128-bit atomics as evidence of the exact compiler strategy, without assuming CASP or any other ISA family must automatically be supported.

No libc dependency is allowed.

### Exact derivation

Add `scripts/derive-compiler-corpus.sh`:

- require NDK `29.0.14206865` exactly;
- resolve absolute `aarch64-linux-android23-clang` and `llvm-objdump` from that NDK only;
- compile `O0`, `O2`, `Oz`;
- compile profile `base` with `-march=armv8-a -mno-outline-atomics`;
- compile profile `lse` with `-march=armv8.1-a+lse -mno-outline-atomics`;
- use freestanding/PIC/no-builtin/no-stack-protector settings;
- preserve instruction order and emit deterministic TSV fields:
  `optimization profile function address raw mnemonic operands`.

### Product verifier

Add `internal/arch/arm64/compiler_corpus_test.go`:

- parse and validate the TSV contract;
- group instructions by optimization/profile/function and require contiguous 4-byte instruction addresses;
- decode every raw word with the real `Decoder`;
- evaluate whole functions with a real `Translator` and identity VM opcode map;
- validate every emitted exclusive-region content hash and every FP/SIMD side instruction;
- report every unexpected gap with optimization/profile/function/address/raw/mnemonic/operands;
- keep reports deterministic;
- require all six optimization/profile combinations and representative corpus function families;
- provide ordinary non-NDK unit tests for parser/grouping/fail-closed classification behavior.

Any future intentional-fail-closed expectation must be exact and reviewed; mnemonic-wide or family-wide suppression is forbidden.

### CI

Extend `Verification` without removing any existing gate:

1. derive the existing exact-r29 FP/SIMD corpus;
2. derive the Phase 18 compiler corpus into `$RUNNER_TEMP`;
3. run `TestExactR29CompilerCorpusCoverage` with `VMPACKER_COMPILER_CORPUS`;
4. retain exact-r29 runtime build, full tests/race, vet, contract checks and macOS ARM64 CLI build.

## 4. Execution policy

The first exact-r29 PR run is diagnostic by design. If it exposes gaps:

- repair ordinary compiler-emitted instructions when semantics fit the current VM/runtime cleanly;
- otherwise record a narrow, evidence-backed intentional fail-closed expectation only after source-level audit;
- do not promote CASP/SVE/SME/MTE/crypto/system extensions merely because the compiler can emit them;
- rerun the complete current-head/current-base Verification after every correction.

## 5. First exact-r29 diagnostic findings and corrections

The first full PR run proved that the gate catches real product defects rather than merely measuring an abstract instruction count. The following ordinary compiler outputs were incorrectly rejected and are repaired in Phase 18:

- `LDARB` / `LDARH` / `LDAR` were swallowed by an over-broad integer pair-load mask;
- ordinary signed-offset `LDP` / `STP` addressing (`WB=2` in the decoded IR) was incorrectly treated as an invalid writeback mode;
- valid 64-bit logical immediates with bit 63 set were incorrectly rejected because their unsigned bit pattern is stored in the signed `Instruction.Imm` field;
- the pair mask is tightened through bit 30 so integer `LDP` does not consume `LDPSW`;
- the `LDPSW` match value is corrected to the actual pair-signed-word encoding;
- mode `WB=2` is accepted only for pair signed-offset addressing and is explicitly excluded from writeback-overlap constraints.

Focused ARM64 tests/vet pass for these repairs. Temporary repair workflows/scripts self-delete and are not part of the product diff.

The same diagnostic run also exposed larger architecture boundaries that are **not** being hidden by the baseline fixes:

- baseline 128-bit atomics use branch-bearing pair-exclusive loops (`CBZ`/`CBNZ`/`B.cond` inside the exclusive sequence), which require PC-relative control-flow relocation rather than a whitelist relaxation;
- the LSE profile emits `CASP` / `CASPA` / `CASPL` / `CASPAL`, which need a first-class 128-bit pair atomic transport design;
- exact Clang uses GPR↔D/Q lane moves for `__int128`; single-GPR-role forms are candidates for the existing FP/SIMD native-thunk architecture and must be validated separately;
- a register-offset `LDR Q` appears in ordinary pair/struct traffic and requires two GPR address roles, beyond the existing one-scratch FP/SIMD thunk contract;
- `-Oz` can use LLVM machine-outliner tail branches even with outline atomics disabled; external-function tail transfer must stay explicit rather than being misclassified as an in-function branch.

The second exact-r29 run is used to measure these remaining boundaries after removing the confirmed baseline decoder/policy defects.

## 6. Exit criteria

- compiler corpus and derivation are deterministic and exact-r29-bound;
- real Decoder + whole-function Translator are the authority;
- no generated corpus artifact is committed;
- supported, intentional fail-closed, and unexpected-gap states cannot be conflated;
- all existing Verification gates plus the new compiler-coverage gate are green;
- only verified product changes are squash-merged into `main` with the exact head SHA;
- the repository's current default integration ref is fast-forwarded to the resulting `main` SHA so the two remain content-identical until repository metadata can be changed separately.
