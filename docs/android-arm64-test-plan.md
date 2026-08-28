# Android AArch64 test plan

This plan covers independent AArch64 ELF64 shared objects and native executables. It does not certify release readiness; each release gate needs reproducible evidence.

## Tier 0: repository checks

```sh
make packer
make runtime-integration ANDROID_NDK=/path/to/android-ndk-r29
go list ./...
go test ./...
go vet ./...
bash -n scripts/*.sh
bash scripts/check-contract.sh
```

Phase 3 adds generated ELF fixtures for malformed tables/ranges, symtab/dynsym merge, explicit ranges, CFG inference/rejection, selection limits, bytecode limits, and no-panic fuzz seeds. Phase 4 removes the fixed blob and validates an exact-r29 relocatable runtime before the explicit Phase 8 planner boundary.

## Tier 1: host pipeline validation

1. Build the macOS ARM64 CLI and validate the Android API 23 relocatable runtime with exact NDK r29.
2. Build repository fixtures with `make android-fixtures`.
3. Run the development CLI with a requested report and confirm it records the target, `opcode_map_digest`, `runtime_strategy: ndk-r29-et-rel-validated`, and the explicit `Phase 8 rewrite planner required` failure.
4. Confirm that no artifact or debug map is published and the input remains byte-for-byte unchanged.

## Tier 2: native executable device smoke

This tier resumes only after the Phase 8 writer is implemented and independently verified. Then connect authorized physical `arm64-v8a` devices, compare baseline and transformed behavior, and capture linker, signal, unwind, and SELinux diagnostics.

Root access is optional diagnostic tooling and must not become a runtime requirement.

## Required release matrix

- Android API 23 and representative later API levels.
- 4 KiB and 16 KiB page-size devices.
- Shared object, PIE, and `ET_EXEC` inputs.
- Plan-first layouts with 4 KiB/16 KiB-compatible LOAD alignment and far-branch veneers; note hijacking is forbidden.
- BTI-enabled and PAC-enabled binaries and devices.
- Every supported translated instruction plus explicit rejection for unsupported instructions.
- Repeatable loading and execution on physical devices.

## Pass criteria

A release candidate passes only when every required matrix entry has recorded, reproducible evidence and transformed behavior matches its baseline. The current repository must be described as development-stage until that evidence exists.
