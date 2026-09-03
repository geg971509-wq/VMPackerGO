# VMPackerGO support matrix

This file describes **current implemented behavior**, not future acceptance criteria. `docs/product-contract.md` remains the product contract. The ARM64 tri-state policy in `internal/arch/arm64/policy.go` is the implementation authority for individual instructions; compiler corpora are verification evidence, not the definition of support.

## Status terms

- **host-verified** — implemented and covered by the normal macOS/exact-r29 Verification workflow.
- **device-required** — host implementation exists, but release acceptance requires physical Android evidence.
- **fail-closed** — intentionally rejected rather than approximated.
- **open** — required product behavior is not yet fully implemented.

## Product surface

| Capability | Status | Notes |
| --- | --- | --- |
| Host | host-verified | macOS ARM64 CLI only. |
| Android input | host-verified | AArch64 ELF64 shared objects, PIE and `ET_EXEC`, API 23+. |
| APK/AAB/GUI/Linux/Windows product | fail-closed / out of scope | Historical material is archived only. |
| Max input | host-verified | 1 GiB. |
| Max protected functions | host-verified | 4096. |
| Max final bytecode per function | host-verified | 256 KiB. |
| Aggregate rewrite expansion | host-verified | Appended rewrite data is bounded to 1 GiB and the final file endpoint to 2 GiB. |
| Protected entry ABI | intentional bounded support | Explicit ABI metadata is required; current release contract accepts at most eight integer/pointer parameters and `void` or one integer/pointer result. FP/vector/aggregate/variadic entry is rejected. |
| Calls inside protected code | host-verified, device-required | AAPCS64 bridge carries X0-X8, stack arguments, V0-V7 and FP state. |

## Runtime correctness

| Capability | Status | Notes |
| --- | --- | --- |
| Runtime faults | host-verified | Bytecode/control/resource/descriptor/eval-stack failures are separate from NZCV and terminate fail-closed after cleanup. |
| Architectural shadow stack | host-verified, device-required | Separate 8 MiB mapping with 16 KiB guard granules. |
| Protected-to-protected call frames | host-verified | Dynamically allocated and bounded; no fixed 16-frame silent truncation. |
| Packed direct tail | host-verified | Selected target switches VM code context without increasing call depth. |
| Native direct external tail | fail-closed | A generic native tail `B` is not approximated as call+return because that changes LR/backtrace/unwind observations. Only selected packed tails and explicitly validated compiler-outliner helpers are accepted. |
| Native indirect external `BR` | fail-closed | Internal/packed targets are handled; arbitrary native external indirect tail transfer is rejected. |
| Descriptor lookup | supported | Current runtime lookup remains bounded by the 4096-function product limit; an additional runtime index is a performance optimization, not a release-correctness requirement. |

## ARM64 instruction policy

| Family | Status | Notes |
| --- | --- | --- |
| Integer ALU / flags / conditions | host-verified | Explicit semantic handlers and width-aware NZCV. |
| ADR/ADRP/literal references | host-verified | Image-relative relocation model; device ASLR proof still required. |
| Transformed entry transfer reach | host-verified, device-required | Near entries use `B imm26`; out-of-range entries use a plan-time inline `ADRP X17 + ADD X17 + BR X17` veneer within ADRP range and with sufficient entry bytes. Farther/shorter cases reject deterministically. |
| FP/SIMD exact-r29 common corpus | host-verified, device-required | Whitelist plus generated native thunks. Unknown encodings reject. |
| LSE scalar and CASP atomics | host-verified, device-required | Native helpers; contention semantics require device evidence. |
| Exclusive regions | host-verified, device-required | Closed continuous native thunks; unsupported monitor topology rejects. |
| Barriers / raw SVC / selected MRS/MSR / PAC / BTI | host-verified, device-required | Physical CPU/kernel semantics remain a release gate. |
| SVE/SVE2/SME and unlisted architectural extensions | fail-closed | Not required for this release; no speculative emulation. |

Every decoder opcode is required by test to have an explicit `virtual`, `native thunk`, or `reject` disposition. Adding a decoder opcode without adding product policy is therefore a hosted CI failure.

## ELF and unwind

| Capability | Status | Notes |
| --- | --- | --- |
| Plan-first rewrite | host-verified | Complete plan is validated before writer mutation. |
| W^X runtime loads / 16 KiB alignment | host-verified, device-required | New loads use 0x4000 alignment. |
| Existing program-header relocation | host-verified, device-required | Android loader proof still required. |
| Runtime `.eh_frame` / GNU unwind index integration | host-verified structurally | Generated runtime FDEs are included where the target has a supported GNU unwind index. |
| C++ personality/invoke/landing/LSDA bridge | host-verified structurally, device-required | Runtime routes and generated CFI/LSDA exist; throw/catch/destructor/rethrow acceptance requires physical Android unwinder evidence. |
| Exception protection without discoverable `PT_GNU_EH_FRAME` | fail-closed | The packer rejects rather than claiming exception safety without a proven unwind discovery path. |

## Verification and release evidence

Hosted Verification validates the exact 85-demo case specification, device-evidence schema/negative cases, release-evidence schema/negative cases, a replayable fail-closed release rehearsal, active bounded mutation fuzzing of the current decoder/ELF/EH-frame/LSDA fuzz targets, exact Go toolchain, exact NDK r29 compiler corpora, runtime build, race tests, vet and the macOS ARM64 CLI.

The following are **not satisfied by host tests** and remain hard release gates:

- physical Android API/page-size/BTI/PAC/ASLR matrix;
- exact 85-demo baseline → pack → transformed differential execution on both 4 KiB and 16 KiB physical devices;
- multithreaded atomic contention;
- C++ throw/catch/destructor/rethrow through protected/native boundaries;
- malformed/adversarial runtime/device cases where applicable;
- Developer ID signing, hardened runtime and Apple notarization;
- source/checksum/provenance publication;
- independent release review.

The repository contains qualification, execution, merge, validation, signing/notarization and review-recording harnesses for those gates. Their real-world evidence is external and must never be fabricated. No absence of device or signing evidence may be converted into a passing release claim.
