# VMP Phase 21 — exact-r29 machine-outliner closure

## Goal

Remove the final exact-NDK-r29 compiler-derived intentional boundary, `machine-outliner`, without adding a generic external `B` or native tail-transfer ABI.

## Exact-r29 audit evidence

The macOS 15 / Android NDK `29.0.14206865` audit completed successfully before implementation.

At `-Oz -march=armv8-a`, one local `OUTLINED_FUNCTION_0` exists at `0x480`, size `0x14`. `vmp_atomic8`, `vmp_atomic16`, and `vmp_atomic32` end with unconditional `B` instructions to that exact start. The helper body is four unshifted 32-bit EOR(register) instructions followed by `RET X30`.

At `-Oz -march=armv8.1-a+lse`, one local `OUTLINED_FUNCTION_0` exists at `0x424`, size `0x8`. The same three atomic callers end with unconditional `B` instructions to that exact start. The helper body is one unshifted 32-bit EOR(register) followed by `RET X30`.

No caller executes an instruction after the outliner transfer. The helper bodies are contiguous, image-local, explicitly sized symbols, and RET-terminated.

## Architecture consensus

Use pack-time semantic inlining at the original tail-B source offset:

- selection analysis accepts an external `B` only when it is the selected function's final instruction;
- the target must have one exact `OUTLINED_FUNCTION_<n>` identity and no conflicting/branch-site relocation;
- the helper symbol must have nonzero aligned size, remain within one executable file-backed PT_LOAD, and not overlap the caller;
- raw helper bytes must pass the deliberately narrow exact-r29 semantic validator: one or more unshifted `EOR Wd, Wn, Wm`, then exact `RET X30`;
- the Translator emits those EOR semantics and a VM return at the original B's VM location;
- no synthetic ARM64 helper offsets are added to SourceMap/trailer/exception identities;
- generic external `B`, non-tail transfers, ambiguous helpers, shifted/64-bit EOR, other instructions, and future un-audited outliner shapes remain fail-closed.

No VM opcode, wire format, runtime native-tail bridge, or arbitrary helper importer is added.

## Compiler gate

The exact-r29 whole-compiler verifier resolves external tail-B targets against same-profile `OUTLINED_FUNCTION_*` corpus groups, validates the same helper raw semantics, configures the Translator inline, and requires zero compiler-derived intentional boundaries.

The old `machine-outliner` raw-word exemption and stale expectation are deleted. Any future external compiler transfer or helper shape becomes an unexpected exact-r29 gap.

## Verification / merge policy

Temporary audit/apply workflows and scripts must be absent from the final diff. Focused ARM64/ELF tests, full Go tests/race, exact-r29 FP/SIMD, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI must pass on the exact PR head. The squash-merged `main` must pass the same push Verification before all historical branches are synchronized to the final verified main SHA.
