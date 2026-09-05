# iOS dylib completion plan

The Android ELF lane remains unchanged. iOS is implemented as an independent
Mach-O backend so format-specific assumptions cannot leak into the existing
runtime.

1. **Contract and safety boundary (done).** The first target is a thin arm64
   device `MH_DYLIB`. arm64e, simulator, FAT, encrypted slices, and automatic
   signing are explicit fail-closed cases. Modified output is unsigned and
   requires the normal app/framework signing pipeline.
2. **Strict Mach-O model (done for the first lane).** Parse the 64-bit header,
   `LC_SEGMENT_64`, sections, `LC_SYMTAB`, code-signature metadata, bounds and
   16 KiB layout. Resolve explicit ranges or symbols and reject overlapping or
   relocation-sensitive functions.
3. **Plan-first relocation (done for the first lane).** Reserve an aligned
   `__VMPACK` executable segment, copy only validated functions, patch a direct
   AArch64 entry branch, preserve existing load-command offsets, and clear the
   old signature blob range.
4. **Native iOS VM runtime (next hard gate).** Build a Darwin `MH_OBJECT`
   runtime with Apple clang/ld64, platform memory APIs, thread-safe guarded
   storage, and an iOS relocation/import plan. The repository now has a
   validated Darwin `MH_OBJECT` builder/parser contract, but it deliberately
   does not generate a fake interpreter or claim VM semantics. The Android
   `svc` runtime cannot be reused.
5. **dyld metadata and unwind (next hard gate).** The first lane preserves
   exports, rebase/bind, `LC_FUNCTION_STARTS` and data-in-code records while
   original entry addresses remain stable. Chained fixups, compact unwind,
   exception, ObjC and Swift metadata remain fail-closed until the writer
   updates their address/segment tables. PC-relative and branch-heavy
   functions also remain fail-closed.
6. **Apple verification (external gate).** On macOS/Xcode, compile real dylib
   fixtures and validate with `otool`, `nm`, `codesign` and dyld. On devices,
   compare baseline and packed `dlopen`/`dlsym` behavior across page sizes,
   ASLR, PAC/BTI and exception/unwind cases.

The current implementation therefore provides a safe, structurally valid
relocation lane while keeping the unfinished VM/runtime and device gates
visible in reports and documentation. It does not claim Android runtime
semantics for an iOS binary.
