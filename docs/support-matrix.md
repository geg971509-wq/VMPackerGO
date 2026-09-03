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
| Protected entry ABI | intentional bounded support | Explicit ABI metadata is required; current release contract accepts at most eight integer/pointer parameters and `void` or one integer/pointer result. FP/vector/aggregate/variadic entry is rejected. |
| Calls inside protected code | host-verified, device-required | AAPCS64 bridge carries X0-X8, stack arguments, V0-V7 and FP state. |

## Runtime correctness

| Capability | Status | Notes |
| --- | --- | --- |
| Runtime faults | host-verified | Bytecode/control/resource/descriptor/eval-stack failures are separate from NZCV and terminate fail-closed after cleanup. |
| Architectural shadow stack | host-verified, device-required | Separate 8 MiB mapping with 16 KiB guard granules. |
| Protected-to-protected call frames | host-verified | Dynamically allocated and bounded; no fixed 16-frame silent truncation. |
| Packed direct tail | host-verified | Selected target switches VM code context without increasing call depth. |
| Native direct tail | bounded host implementation | Currently lowered through the validated native call bridge plus protected return; exact tail-transfer equivalence remains under final control-flow audit. |
| Native indirect external `BR` | fail-closed | Internal/packed targets are handled; arbitrary native external indirect tail transfer is not claimed. |
| Descriptor lookup | supported | Current runtime lookup remains bounded by the 4096-function product limit; performance optimization is not release-critical. |

## ARM64 instruction policy

| Family | Status | Notes |
| --- | --- | --- |
| Integer ALU / flags / conditions | host-verified | Explicit semantic handlers and width-aware NZCV. |
| ADR/ADRP/literal references | host-verified | Image-relative relocation model; device ASLR proof still required. |
| B/BL immediate reach | open | Near transfers are planned; long out-of-range entry transfer closure is still required. |
| FP/SIMD exact-r29 common corpus | host-verified, device-required | Whitelist plus generated native thunks. Unknown encodings reject. |
| LSE scalar and CASP atomics | host-verified, device-required | Native helpers; contention semantics require device evidence. |
| Exclusive regions | host-verified, device-required | Closed continuous native thunks; unsupported monitor topology rejects. |
| Barriers / raw SVC / selected MRS/MSR / PAC / BTI | host-verified, device-required | Physical CPU/kernel semantics remain a release gate. |
| SVE/SVE2/SME and unlisted architectural extensions | fail-closed | Not required for this release; no speculative emulation. |

## ELF and unwind

| Capability | Status | Notes |
| --- | --- | --- |
| Plan-first rewrite | host-verified | Complete plan is validated before writer mutation. |
| W^X runtime loads / 16 KiB alignment | host-verified, device-required | New loads use 0x4000 alignment. |
| Existing program-header relocation | host-verified, device-required | Android loader proof still required. |
| Runtime `.eh_frame` / GNU unwind index integration | host-verified structurally | Generated runtime FDEs are included where the target has a supported GNU unwind index. |
| C++ personality/invoke/landing/LSDA bridge | host-verified structurally, device-required | Runtime routes and generated CFI/LSDA exist; throw/catch/destructor/rethrow acceptance requires physical Android unwinder evidence. |
| Targets without a usable unwind discovery mechanism | fail-closed/open audit | Must not be described as exception-safe until the final discovery strategy is proven. |

## Release evidence

The following are **not satisfied by host tests** and remain hard release gates:

- physical Android API/page-size/BTI/PAC/ASLR matrix;
- exact 85-demo baseline → pack → transformed differential execution;
- multithreaded atomic contention;
- C++ throw/catch/destructor/rethrow through protected/native boundaries;
- malformed/adversarial device cases where applicable;
- Developer ID signing, hardened runtime and Apple notarization;
- source/checksum/provenance publication;
- independent release review.

No absence of device or signing evidence may be converted into a passing release claim.
