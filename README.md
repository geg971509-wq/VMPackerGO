# VMPacker

VMPacker is a development-stage virtual-machine packer for independent Android AArch64 ELF64 binaries. The official product is a macOS ARM64 command-line application.

## Product scope

- Host product: macOS ARM64 CLI.
- Inputs: independent Android AArch64 ELF64 shared objects (`.so`) and PIE/`ET_EXEC` native executables.
- Minimum Android runtime: API 23.
- Selection: one direct function/address with `-abi`, or multiple manifest-v1 entries with explicit ABIs; names merge `.symtab` and `.dynsym`, while single addresses use fail-closed CFG inference.
- Target output: a transformed ELF plus optional report-v1 JSON and explicit debug map.
- Not active product scope: APK, AAB, GUI, Linux releases, or Windows releases. Historical APK and GUI work is retained under `archive/` and is unsupported.

VMPacker only increases the cost of reverse analysis. It does not make code impossible to inspect, copy, modify, or bypass.

## Development status

This repository is under development and is not a release-ready product. Release gates include API 23+ compatibility, 4 KiB and 16 KiB page-size validation, BTI/PAC behavior, supported-instruction correctness, ELF loader compatibility, and device smoke coverage. The repository must not be described as passing these gates until the checks exist and pass.

See the [product contract](docs/product-contract.md), [development guide](docs/development.md), and [report schema](docs/report-schema-v1.md).

## Development commands

```sh
./build.sh
make packer
make runtime-integration ANDROID_NDK=/path/to/android-ndk-r29
go list ./...
go test ./...
go vet ./cmd/vmpacker ./internal/...
bash scripts/check-contract.sh

./build/vmpacker -ndk /path/to/android-ndk-r29 -mode so \
  -func exported_name -abi 'i32(ptr)' -report pack.json \
  -o libdemo.vmp.so libdemo.so
```

The root `build.sh` entry point builds the current Git checkout as a macOS ARM64 executable, verifies the Mach-O architecture, and writes `dist/vmpacker-darwin-arm64` plus the identical direct runner `dist/vmpacker`.

The fixed interpreter blob has been removed. Each pack attempt creates a per-run opcode map, rebuilds and validates a relocatable runtime from embedded source with exact NDK r29, and then fails closed with `Phase 8 rewrite planner required` before translating or mutating the input. The development runtime now includes the Phase 5 core semantic fixes and a real-r29-validated Phase 6 host implementation: AAPCS64/native atomics, an exact-r29 `-O0/-O2/-Oz` FP/SIMD corpus whitelist with state-preserving native thunks, and continuous closed exclusive-region thunks. Unwind parsing and the exact 85-demo manifest are also present. The physical-device Phase 5/6 gates, C++ exception bridge, writer, device packing evidence, and release gates remain open. No artifact or debug map is emitted at this boundary.

## License and use

Copyright 2026 LeoChen.

VMPacker is licensed under the GNU Affero General Public License, version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Use the software only on binaries you own or are authorized to modify, and comply with applicable law. This is informational guidance and does not add restrictions beyond AGPL-3.0-only.
