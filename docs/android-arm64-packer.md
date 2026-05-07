# Android arm64 APK/JNI support design

## Goal

VMPacker now has an explicit Android target lane for **authorized** `arm64-v8a` JNI shared libraries and standalone Android native executables. The primary APK runtime shape is a normal APK process running as the app UID (`u:r:untrusted_app*` or equivalent). Root is useful for lab inspection and file collection, but the protected `.so` must be callable through ordinary Java/Kotlin/JNI code without `su` at runtime.

## Supported input

- ELF64 AArch64 (`EM_AARCH64`) shared object from `lib/arm64-v8a/*.so` or Android native executable.
- JNI shared libraries: `ET_DYN` with `PT_DYNAMIC` so Android's linker can load them from an APK.
- Native executables: Android PIE `ET_DYN` with `PT_INTERP`/`PT_DYNAMIC`, or standalone `ET_EXEC` test binaries when the device permits them.
- At least one executable `PT_LOAD`.
- Injector support:
  - `note`: converts a spare `PT_NOTE` program header into a payload `PT_LOAD`.
  - `add-segment`: uses a safe `PT_NULL` slot, or appends a PHDR only when verified padding allows it.

If a target Android `.so` or executable has no `PT_NOTE`, use `-injector add-segment` or `-injector auto`. The current add-segment implementation is intentionally conservative: it will not relocate PHDR/LOAD contents; it fails with remediation if there is no `PT_NULL` slot and no safe PHDR-table padding.

## CLI flow

```bash
# 0) Build a direct macOS/current-host CLI with the Android arm64 VM blob
# embedded. This is the normal "process .so on Mac, output final .so" path.
make mac-cli

# 1) Build the Android-compatible interpreter blob with the NDK.
make android-stub ANDROID_NDK=/path/to/android-ndk ANDROID_API=23

# 2) Protect one or more JNI/native functions in the arm64-v8a library.
./dist/vmpacker \
  -target android \
  -android-mode so \
  -injector auto \
  -profile compat \
  -report pack-report.json \
  -func Java_com_example_demo_NativeBridge_checkLicense \
  -o libdemo.vmp.so \
  app-unpacked/lib/arm64-v8a/libdemo.so

# Address mode also works when symbols are stripped.
./build/vmpacker.exe \
  -target android \
  -addr '0x12340-0x12480:native_check' \
  -o libdemo.vmp.so \
  libdemo.so

# Android native executable / PIE binary.
./build/vmpacker.exe \
  -target android \
  -android-mode native \
  -injector auto \
  -report native-report.json \
  -func protected_calc \
  -o native_bin.vmp \
  native_bin
```

Strategy flags:

- `-android-mode auto|so|native` classifies APK-loadable shared objects separately from Android PIE/native executables.
- `-injector auto|note|add-segment` exposes the injector contract. `auto` selects `note` when `PT_NOTE` exists and `add-segment` when no note exists. `add-segment` currently supports safe `PT_NULL` slot reuse and verified in-place PHDR growth.
- `-profile compat|balanced|strong` records the requested compatibility/strength preset for automation and GUI use.
- `-report <file.json>` writes a machine-readable pack report including target kind, selected injector, protected functions, payload location, and status.

Host-only `.so` transform smoke:

```bash
make mac-so-pack-smoke
```

This verifies `dist/vmpacker` can run on macOS and transform an Android arm64 `.so` into a final packed `.so` without using `adb` or a connected device. Device smoke is still required to prove runtime loading.

## APK repack flow for local testing

First-class APK workflow:

```bash
./build/vmpacker.exe \
  -apk app.apk \
  -lib libdemo.so \
  -func Java_com_example_demo_NativeBridge_checkLicense \
  -injector auto \
  -profile compat \
  -apk-sign debug \
  -report app-vmp.report.json \
  -o app-vmp.apk
```

This path extracts `lib/arm64-v8a/libdemo.so`, protects it with the Android ELF pipeline, rebuilds the APK, runs `zipalign`, and signs with the standard debug keystore. Use `-apk-sign none` when you need an unsigned aligned artifact for an external signing step.

Repository smoke:

```bash
make android-apk-workflow-smoke
```

The lower-level replacement helper remains available when you want manual signing:

```bash
scripts/android-repack-apk.sh app.apk arm64-v8a/libdemo.so libdemo.vmp.so app-vmp-unsigned.apk
zipalign -f -p 4 app-vmp-unsigned.apk app-vmp-aligned.apk
apksigner sign --ks ~/.android/debug.keystore app-vmp-aligned.apk
adb install -r app-vmp-aligned.apk
```

The helper only replaces the library entry. Signing remains explicit because release keys and keystores are user-owned secrets.

For a fully generated smoke APK that loads `libnative_demo.so` and calls `Java_com_example_demo_NativeBridge_checkLicense(int)`, use:

```bash
scripts/android-build-smoke-apk.sh libnative_demo.vmp.so .omx/specs/apk-smoke
```

That script builds, signs with a debug key, installs, launches, and checks logcat for the JNI result.

## Runtime model

1. Original JNI/native function entry is replaced by a 12-byte token trampoline.
2. The trampoline preserves the caller's normal app-process arguments (`X0`-`X7`) and branches to the injected VM token entry.
3. The VM interpreter decrypts bytecode in app-private anonymous memory using normal Android/Linux syscalls (`mmap`/`munmap`) with read/write pages only.
4. JNI calls and native calls execute inside the app process under the app UID. The VM does not require root, daemon processes, ptrace, or privileged SELinux domains.
5. `su` is reserved for lab-only checks such as pulling files from protected app storage or collecting diagnostics.

## Android compatibility guardrails

- `-target android` validates Android ELF shape and executable `PT_LOAD` before translation/injection. JNI shared libraries must have `PT_DYNAMIC`; standalone native executables may be dynamic PIE or `ET_EXEC`.
- `-report` records `target_kind` (`android-so`, `android-pie`, or `android-exec`) and the selected injector so APK wrappers can make deterministic decisions.
- Token branch generation now checks AArch64 `B imm26` reachability (±128 MiB) and fails instead of emitting a wrapped branch.
- The Makefile has a POSIX/Python blob generator and an `android-stub` target so macOS/Linux hosts are not dependent on PowerShell.
- The interpreter uses raw arm64 Linux syscall numbers that are valid for Android arm64. It does not allocate RWX memory.

## Current limitations

- APK workflow currently supports one selected native library per invocation and debug/unsigned signing modes. Release keystore/password handling remains an explicit external signing concern.
- Files without `PT_NOTE` need either a reusable `PT_NULL` slot or safe PHDR-table padding. Full PHDR relocation remains intentionally unsupported.
- C++ exception unwinding, signal-handler-heavy functions, and functions depending on exact frame-unwind metadata need targeted tests before production use.
- JNI functions should be small, deterministic native routines first: license checks, arithmetic/string checks, or security-sensitive decision helpers owned by the app developer.

## Roadmap to product quality

1. Extend add-segment beyond safe slot/gap cases with carefully verified PHDR relocation where Android linker constraints allow it.
2. Extend APK workflow to multi-library config files and release-signing handoff.
3. Add an instrumented sample APK with JNI tests and logcat assertions.
4. Add device CI hooks for authorized lab devices and emulator fallback.
5. Add compatibility reports for API level, ABI, linker segment layout, and protected JNI symbol coverage.
