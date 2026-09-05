# iOS instruction and VM migration matrix

The Android backend has a shared ARM64 decoder, translator, bytecode format
and interpreter. The current iOS backend does **not** invoke that pipeline. It
only relocates a narrow, non-PC-relative native function, so the rows below
describe the migration gate rather than claiming iOS runtime support.

| Instruction family | Android VM disposition | Current iOS relocation lane | Full iOS VMP gate |
| --- | --- | --- | --- |
| Integer ALU, logical, shifts, flags, multiply/divide | Virtual, tested | Native bytes may be copied when no relocation-sensitive instruction is present | Darwin interpreter handler and differential execution |
| MOV wide, bitfield, extract, conditional select, compare | Virtual, tested | Same narrow copy rule | Darwin bytecode translation and NZCV tests |
| Scalar loads/stores, pair loads/stores, literal loads | Virtual or image relocation | Literal/relocation-sensitive functions fail closed | Mach-O image relocation and bounds model |
| B/BL/conditional branches, CBZ/TBZ, BR/BLR/RET | Virtual/native bridge | All branch/control-flow relocation cases fail closed | VM control stack, native-call ABI and target relocation |
| SIMD/FPSIMD | Native thunk | Not a VM runtime capability on iOS | Darwin SIMD thunk with device feature policy |
| Exclusive/LSE/CAS/CASP atomics | Native thunk | Not a VM runtime capability on iOS | Darwin LL/SC or feature-gated native thunks |
| SVC | Linux-specific native thunk | Must not be interpreted as an Android syscall | iOS API bridge or deterministic reject |
| MRS/MSR, PAC, BTI, barriers | Host/native semantics | No arm64e/PAC claim; metadata-sensitive cases fail closed | Darwin ABI-preserving implementation and device matrix |
| WFE/WFI, HLT/BRK, unknown encodings | Reject or fail closed | Reject | Remain reject unless a separately verified design exists |
| C++ unwind, ObjC/Swift metadata, chained fixups | Android-specific or external metadata | Fail closed where address tables can become stale | Mach-O metadata rewrite plus exception/device evidence |

The single source of truth for decoder policy is
`internal/arch/arm64/policy.go`. `internal/runtime/vm_decode.h` is checked
against the Go opcode sizes so a handler cannot silently disagree with the
bytecode stream. In particular, `VLD16` and `VST16` are four-byte VM
instructions.

Until the Darwin runtime, Mach-O bytecode/token writer, import/fixup handling,
ABI bridge and device gates are implemented, `-mode ios` must be described as
structural relocation only. It is not a completed iOS VMP packer.
