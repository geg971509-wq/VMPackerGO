# VMPacker

VMPacker is a development-stage virtual-machine packer for independent Android AArch64 ELF64 binaries. The official product is a macOS ARM64 command-line application.

## Product scope

- Host product: macOS ARM64 CLI.
- Inputs: independent Android AArch64 ELF64 shared objects (`.so`) and PIE/`ET_EXEC` native executables, plus a narrow iOS arm64 thin `MH_DYLIB` backend (`-mode ios`).
- Minimum Android runtime: API 23.
- Selection: one function with `-func` or address/range with `-addr`, each with `-abi`, or multiple manifest-v1 entries with explicit ABIs; names merge `.symtab` and `.dynsym`, while single addresses use fail-closed CFG inference.
- Target output: a transformed ELF or (in `-mode ios`) Mach-O dylib, plus optional report-v1 JSON and an Android-only debug map.
- Not active product scope: APK, AAB, GUI, Linux releases, Windows releases, arm64e/simulator/FAT iOS slices, or automatic app-bundle signing. Historical APK and GUI work is retained under `archive/` and is unsupported.

VMPacker only increases the cost of reverse analysis. It does not make code impossible to inspect, copy, modify, or bypass.

## Development status

The host-side productization path is implemented and covered by the repository Verification workflow: fail-closed runtime integrity, guarded/bounded runtime resources, explicit ARM64 capability policy, exact-NDK-r29 runtime construction, plan-first ELF rewriting, bounded near/far transformed-entry transfers, structural C++ exception/unwind bridge generation, the exact 85-demo device-case specification, fuzz/resource-budget gates, and evidence-driven release tooling.

The project is **still not release-ready**. Release acceptance requires real physical Android evidence across API/page-size/BTI/PAC/ASLR and CPU-feature matrices, baseline-versus-packed execution for all 85 demos, atomic-contention and C++ exception/unwind evidence, Developer ID signing, Apple notarization, and an independent release review. Those external facts are never inferred from host tests or fabricated by the build.

See the [product contract](docs/product-contract.md), [current support matrix](docs/support-matrix.md), [device evidence schema](docs/device-evidence-schema-v1.md), [release process](docs/release-process.md), [remediation audit](docs/remediation-audit-20260903.md), and [report schema](docs/report-schema-v1.md).

The iOS Mach-O boundary and signing requirements are documented in [docs/ios-dylib.md](docs/ios-dylib.md).

## Development commands

```sh
./build.sh
make packer
make verify
make demo-cases
make evidence-self-test
make runtime-integration ANDROID_NDK=/path/to/android-ndk-r29

./build/vmpacker -ndk /path/to/android-ndk-r29 -mode so \
  -func exported_name -abi 'i32(ptr)' -report pack.json \
  -o libdemo.vmp.so libdemo.so
```

The root `build.sh` builds the current Git checkout as a macOS ARM64 executable, verifies the Mach-O architecture, and writes `dist/vmpacker-darwin-arm64` plus the identical direct runner `dist/vmpacker`.

Manifest-v1 input must be a regular local file no larger than 16 MiB and may select at most 4096 functions. The size and file-type checks happen before JSON parsing so malformed or special-file input cannot turn manifest parsing into an unbounded read.

Each pack attempt creates a per-run opcode map, translates selected functions, rebuilds and validates a relocatable AArch64 runtime from embedded source with exact Android NDK `29.0.14206865`, constructs an immutable rewrite plan, applies that plan to a fresh in-memory ELF image, and reparses the result before publication. The runtime uses explicit fail-closed fault classes, a separately mapped guarded shadow stack and dynamically bounded protected-call frames. The plan covers 0x4000-aligned W^X runtime loads, runtime relocations, encrypted bytecode/token descriptors, BTI-aware entry patches, inline `ADRP+ADD+BR` long entry veneers when a direct `B` cannot reach, program-header mutations, and supported GNU unwind-index integration. Generic native external tail branches are rejected rather than approximated as call+return.

The physical-device harnesses under `scripts/` qualify devices, run the exact 85-demo differential matrix and semantic coverage fixtures, merge evidence, and validate it against the exact commit and manifest. Release tooling then validates that evidence before signing/notarizing a tagged macOS ARM64 candidate and creating source/checksum/evidence artifacts; the final release contract also reconstructs the tagged Git archive and rejects a source package that does not describe the exact tag. An independent approval remains a separate mandatory gate.

## License and use

Copyright 2026 LeoChen.

VMPacker is licensed under the GNU Affero General Public License, version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Use the software only on binaries you own or are authorized to modify, and comply with applicable law. This is informational guidance and does not add restrictions beyond AGPL-3.0-only.
