# VMP Phase 20B — bounded per-function bytecode capacity

## Problem

A real protected function (`pics_memo_upload_module_impl`) produced 72,836 bytes of final VM bytecode after translation, reverse-layout markers, and the address-map trailer. The current product rejects any final function bytecode above 65,536 bytes even though the transport and runtime length fields are already 32-bit.

## Audit conclusion

The 64 KiB value is not a wire-format limit:

- rewrite-plan `bytecodeLen` is `uint32`;
- token descriptor `bc_len` is `u32`;
- VM `pc`, `bc_len`, branch targets, source-map offsets, and map counts are 32-bit;
- bytecode storage is mmap-backed, not a fixed 64 KiB stack buffer;
- selected native functions remain bounded to at most 4096 AArch64 instructions.

The real synchronized limits are Go `validateFinalBytecodeSize` and runtime `VM_BYTECODE_MAX`.

## Consensus

Raise the bounded per-function final bytecode limit to 256 KiB (262,144 bytes), not unlimited and not merely 128 KiB.

Rationale:

- directly covers the observed 72,836-byte OLLVM-expanded function;
- leaves substantial headroom for VM expansion plus the 8-byte-per-source-map-entry trailer;
- remains far below 32-bit transport limits;
- with `VM_CALL_DEPTH_MAX=16`, the worst-case simultaneous packed bytecode buffers remain approximately 4 MiB, excluding the root buffer and VM contexts;
- keeps malformed/tampered input bounded.

## Correctness repair

Runtime must reject an oversized descriptor instead of silently truncating `bc_len` to the maximum. Silent truncation can execute an incomplete instruction stream/trailer and violates fail-closed policy.

## Changes

1. Define one Go-side final bytecode maximum of 256 KiB and use it in `validateFinalBytecodeSize`.
2. Set runtime `VM_BYTECODE_MAX` to the same 256 KiB bound.
3. Change root and nested packed-function loading from clamp-to-limit to fail-closed rejection.
4. Add Go regression proving 72,836 bytes is accepted and 262,145 bytes is rejected.
5. Add runtime template regression proving the 256 KiB constant and reject-not-clamp behavior remain synchronized.
6. Update product/development contract text from 64 KiB to 256 KiB; also correct the now-stale workflow runner note to macOS 15.

## Merge policy

Run the unchanged full release Verification on the exact PR head: contracts, full Go tests, race, exact-r29 FP/SIMD corpus, exact-r29 whole-compiler corpus, exact-r29 runtime build, vet, and macOS ARM64 CLI. Squash only that verified head to `main`, run the `main` push Verification again, then fast-forward `fix/call-vm-nested` to exact `main`.
