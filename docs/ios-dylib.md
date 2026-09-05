# iOS Mach-O dylib backend

`-mode ios` accepts a thin little-endian `MH_DYLIB` arm64 device slice. It
validates load-command and section bounds, selects a function from `LC_SYMTAB`
or an explicit VM address/range, and emits a plan-first rewrite with a 16 KiB
aligned `__VMPACK` executable segment. The original entry is replaced with an
AArch64 branch to a relocated copy of the function.

This is a structural relocation lane, not the completed VMP pipeline. It does
not generate encrypted VM bytecode, inject a Darwin interpreter, or provide an
iOS native-call/runtime ABI. The instruction migration boundary is recorded in
[the iOS instruction matrix](ios-instruction-matrix.md).

The backend fails closed for arm64e, FAT files, simulator slices, PC-relative
instructions, direct/indirect branches, and images whose load-command headerpad
cannot hold the new segment command. Those cases require relocation-aware dyld
fixup handling and the native iOS VM runtime; copying them would risk a binary
that loads but behaves incorrectly.

The output is unsigned. Any existing `LC_CODE_SIGNATURE` blob is invalidated
and must be replaced during the containing app/framework signing step. The old
signature cannot be reused after code or load commands change. Host structural
tests do not substitute for `codesign`, dyld loading, Objective-C/Swift
metadata, compact unwind, chained fixups, PAC/BTI, or physical-device
baseline-versus-packed execution gates.

On macOS with Xcode installed, `make ios-dylib-validation` compiles a real
arm64 iPhoneOS `MH_DYLIB` fixture with Apple clang/ld64, packs it through the
CLI, checks the resulting Mach-O header, `__VMPACK` segment and exported symbol,
and performs an ad-hoc `codesign --verify` pass. The fixture deliberately uses
one position-independent arithmetic function, so this gate validates the
implemented relocation-only lane without pretending to cover unsupported
PC-relative code or iOS device execution. On non-macOS hosts the target exits
successfully with an explicit skip because Apple's linker and signing tools are
unavailable. The fixture passes `-no_compact_unwind` so it exercises the currently supported dyld
metadata-preservation lane and requires a successful packed artifact. Unsupported
compact-unwind, Objective-C and Swift metadata remain fail-closed and require
dedicated relocation-aware fixtures before being accepted.
