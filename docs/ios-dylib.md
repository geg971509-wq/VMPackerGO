# iOS Mach-O dylib backend

`-mode ios` accepts a thin little-endian `MH_DYLIB` arm64 device slice. It
validates load-command and section bounds, selects a function from `LC_SYMTAB`
or an explicit VM address/range, and emits a plan-first rewrite with a 16 KiB
aligned `__VMPACK` executable segment. The original entry is replaced with an
AArch64 branch to a relocated copy of the function.

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
