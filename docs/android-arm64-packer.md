# Android AArch64 ELF development lane

## Scope

The active packer accepts independent Android AArch64 ELF64 shared objects (`.so`) and PIE or `ET_EXEC` native executables. The official host product is a macOS ARM64 CLI, and the minimum target runtime is Android API 23. Container formats and desktop frontends are outside the active product; historical snapshots are isolated under `archive/`.

This repository is under development. The behavior below describes current interfaces, not completed release certification.

## Input requirements

- ELF64 with `EM_AARCH64`.
- Shared objects use `ET_DYN` and `PT_DYNAMIC`.
- Native executables may be PIE (`ET_DYN` with `PT_INTERP`) or `ET_EXEC`.
- At least one executable `PT_LOAD`.
- `note` injection requires a spare `PT_NOTE`.
- `add-segment` requires a reusable `PT_NULL` entry or verified program-header padding.

## Development CLI

```sh
make mac-cli ANDROID_NDK=/path/to/android-ndk-r29 ANDROID_API=23

./dist/vmpacker \
  -mode so \
  -report pack-report.json \
  -func Java_com_example_demo_NativeBridge_checkLicense \
  -abi 'i32(ptr,ptr,i32)' \
  -o libdemo.vmp.so \
  libdemo.so
```

For a native PIE or `ET_EXEC` input, use `-mode native`. Use `-addr 0xSTART-0xEND:name -abi 'result(params)'` when symbols are unavailable. A single address uses conservative CFG inference and fails with an explicit-range request when control flow is ambiguous. Named selection merges `.symtab` and `.dynsym`. Multiple functions require a manifest-v1 JSON file.

The current internal development strategy selects note hijacking when a suitable note exists and otherwise attempts the conservative add-segment path; it is not a public CLI choice. This is not target-compliant: the complete target contract never hijacks `PT_NOTE` and requires plan-first program-header/segment relocation.

## Active checks

```sh
make android-fixtures
make mac-so-pack-smoke
make android-native-smoke
make android-addsegment-native-smoke
```

Fixture sources remain under `testdata/android/`. The host smoke transforms the independent shared-object fixture. Device smoke compares baseline and transformed native-executable output.

## Release gates

API 23+ coverage, 4 KiB and 16 KiB page sizes, BTI/PAC behavior, loader compatibility, instruction translation, and physical-device execution are mandatory release gates. They are not currently claimed as passing. See [product-contract.md](product-contract.md) and [android-arm64-test-plan.md](android-arm64-test-plan.md).
