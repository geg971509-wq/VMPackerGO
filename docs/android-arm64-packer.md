# Android AArch64 ELF development lane

## Scope

The active packer accepts independent Android AArch64 ELF64 shared objects (`.so`) and PIE or `ET_EXEC` native executables. The official host product is a macOS ARM64 CLI, and the minimum target runtime is Android API 23. Container formats and desktop frontends are outside the active product; historical snapshots are isolated under `archive/`.

This repository is under development. The behavior below describes current interfaces, not completed release certification.

## Input requirements

- ELF64 with `EM_AARCH64`.
- Shared objects use `ET_DYN` and `PT_DYNAMIC`.
- Native executables may be PIE (`ET_DYN` with `PT_INTERP`) or `ET_EXEC`.
- At least one executable `PT_LOAD`.
- Final segment layout is deferred to the Phase 8 plan-first writer and must not reuse notes or assume program-header padding.

## Development CLI

```sh
make mac-cli

./dist/vmpacker \
  -ndk /path/to/android-ndk-r29 \
  -mode so \
  -report pack-report.json \
  -func Java_com_example_demo_NativeBridge_checkLicense \
  -abi 'i32(ptr,ptr,i32)' \
  -o libdemo.vmp.so \
  libdemo.so
```

For a native PIE or `ET_EXEC` input, use `-mode native`. Use `-addr 0xSTART-0xEND:name -abi 'result(params)'` when symbols are unavailable. A single address uses conservative CFG inference and fails with an explicit-range request when control flow is ambiguous. Named selection merges `.symtab` and `.dynsym`. Multiple functions require a manifest-v1 JSON file.

The current development pipeline performs bounded analysis, creates a per-pack opcode map, translates selected functions once, builds and validates an exact-r29 `ET_REL` runtime image, produces a complete immutable 0x4000-aligned W^X rewrite plan, and then fails closed before mutation with `Phase 9 rewrite writer required`. The removed legacy note-hijack/add-segment writer is not a fallback.

## Active checks

```sh
make verify
make runtime-integration ANDROID_NDK=/path/to/android-ndk-r29
make android-fixtures ANDROID_NDK=/path/to/android-ndk-r29
```

Fixture sources remain under `testdata/android/`. Transformation and device-differential smoke tests stay gated until the Phase 8 writer can produce metadata-preserving artifacts.

## Release gates

API 23+ coverage, 4 KiB and 16 KiB page sizes, BTI/PAC behavior, loader compatibility, instruction translation, and physical-device execution are mandatory release gates. They are not currently claimed as passing. See [product-contract.md](product-contract.md) and [android-arm64-test-plan.md](android-arm64-test-plan.md).
