# Android AArch64 test plan

This plan covers independent AArch64 ELF64 shared objects and native executables. It does not certify release readiness; each release gate needs reproducible evidence.

## Tier 0: repository checks

```sh
make android-stub ANDROID_NDK=/path/to/android-ndk-r29
make packer ANDROID_NDK=/path/to/android-ndk-r29
go list ./...
go test ./...
go vet ./cmd/vmpacker ./internal/...
bash -n scripts/*.sh
bash scripts/check-contract.sh
```

Phase 3 adds generated ELF fixtures for malformed tables/ranges, symtab/dynsym merge, explicit ranges, CFG inference/rejection, selection limits, bytecode limits, and no-panic fuzz seeds. `bash scripts/check-contract.sh --release` is expected to fail while the fixed embedded interpreter blob remains a deferred runtime-template blocker.

## Tier 1: host transformation

1. Build the Android API 23 interpreter blob and macOS ARM64 CLI.
2. Build repository fixtures with `make android-fixtures`.
3. Run `make mac-so-pack-smoke`.
4. Confirm the output remains an AArch64 shared object and the report records `target_kind: android-so`, the current `development_strategy`, and `status: ok`.
5. Repeat with the no-note shared-object fixture and confirm `development_strategy: add-segment`.

## Tier 2: native executable device smoke

1. Connect an authorized `arm64-v8a` test device with API 23 or newer.
2. Run `make android-native-smoke`.
3. Run `make android-addsegment-native-smoke`.
4. Compare baseline and transformed stdout and exit status.
5. Capture crashes, linker failures, signal failures, and relevant SELinux diagnostics.

Root access is optional diagnostic tooling and must not become a runtime requirement.

## Required release matrix

- Android API 23 and representative later API levels.
- 4 KiB and 16 KiB page-size devices.
- Shared object, PIE, and `ET_EXEC` inputs.
- Note-hijack and conservative add-segment layouts.
- BTI-enabled and PAC-enabled binaries and devices.
- Every supported translated instruction plus explicit rejection for unsupported instructions.
- Repeatable loading and execution on physical devices.

## Pass criteria

A release candidate passes only when every required matrix entry has recorded, reproducible evidence and transformed behavior matches its baseline. The current repository must be described as development-stage until that evidence exists.
