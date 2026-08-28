# VMPacker

VMPacker is a development-stage virtual-machine packer for independent Android AArch64 ELF64 binaries. The official product is a macOS ARM64 command-line application.

## Product scope

- Host product: macOS ARM64 CLI.
- Inputs: independent Android AArch64 ELF64 shared objects (`.so`) and PIE/`ET_EXEC` native executables.
- Minimum Android runtime: API 23.
- Selection: one direct function/address with `-abi`, or multiple manifest-v1 entries with explicit ABIs; names merge `.symtab` and `.dynsym`, while single addresses use fail-closed CFG inference.
- Output: a transformed ELF plus optional report-v1 JSON and explicit debug map.
- Not active product scope: APK, AAB, GUI, Linux releases, or Windows releases. Historical APK and GUI work is retained under `archive/` and is unsupported.

VMPacker only increases the cost of reverse analysis. It does not make code impossible to inspect, copy, modify, or bypass.

## Development status

This repository is under development and is not a release-ready product. Release gates include API 23+ compatibility, 4 KiB and 16 KiB page-size validation, BTI/PAC behavior, supported-instruction correctness, ELF loader compatibility, and device smoke coverage. The repository must not be described as passing these gates until the checks exist and pass.

See the [product contract](docs/product-contract.md), [development guide](docs/development.md), and [report schema](docs/report-schema-v1.md).

## Development commands

```sh
make android-stub ANDROID_NDK=/path/to/android-ndk-r29
make packer ANDROID_NDK=/path/to/android-ndk-r29
go list ./...
go test ./...
go vet ./cmd/vmpacker ./internal/...
bash scripts/check-contract.sh

./build/vmpacker -mode so -func exported_name -abi 'i32(ptr)' \
  -report pack.json -o libdemo.vmp.so libdemo.so
```

Building the current CLI still uses the fixed interpreter-blob path scheduled for replacement in a later phase. See [development.md](docs/development.md).

## License and use

Copyright 2026 LeoChen.

VMPacker is licensed under the GNU Affero General Public License, version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Use the software only on binaries you own or are authorized to modify, and comply with applicable law. This is informational guidance and does not add restrictions beyond AGPL-3.0-only.
