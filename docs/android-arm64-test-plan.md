# Android arm64 test plan

## Validation tiers

### Tier 0: repository/static checks

- `scripts/make_stub_blob.py --help` succeeds.
- `scripts/android-device-smoke.sh --check-only` reports an `arm64-v8a` authorized device when hardware is attached.
- `go test ./...` passes when Go is installed.
- `make android-stub` produces `cmd/vmpacker/vm_interp.bin` when Android NDK is installed.
- `make mac-cli` produces a direct host executable at `dist/vmpacker`.
- `make mac-so-pack-smoke` uses `dist/vmpacker` on macOS/current host to transform a fixture `.so` into `libnative_demo.mac.vmp.so` without `adb`.
- `scripts/android-build-smoke-apk.sh` can build/install a minimal APK once Android platform/build-tools are installed.
- `make android-fixtures` builds committed fixture sources from `testdata/android/`.
- `make android-smoke` runs the repository-owned APK and native executable smoke flows.

### Tier 1: packer checks with fixture `.so`

1. Build or obtain an owned test JNI library with a small exported function, for example `Java_com_example_demo_NativeBridge_checkLicense`.
2. Confirm ELF shape:
   ```bash
   ./build/vmpacker.exe -info libdemo.so
   ```
3. Protect the function:
   ```bash
   ./dist/vmpacker -target android -android-mode so -injector auto -profile compat -report libdemo.report.json -func Java_com_example_demo_NativeBridge_checkLicense -o libdemo.vmp.so libdemo.so
   ```
4. Verify output remains an AArch64 shared object and has a new RX payload `PT_LOAD`.
5. Verify `libdemo.report.json` records `target_kind: android-so`, `injector_selected: note`, and `status: ok` for PT_NOTE-capable fixtures.
6. Verify no-note fixtures through add-segment:
   ```bash
   make android-addsegment-smoke
   ```
   The add-segment reports should record `injector_selected: add-segment`, `segment_source: pt_null` or `phdr_append`, and `status: ok`.

### Tier 2: APK app-UID smoke

1. Replace `lib/arm64-v8a/libdemo.so` in an owned APK with `scripts/android-repack-apk.sh`.
2. `zipalign` and `apksigner sign` with a debug/test key.
3. `adb install -r` the APK.
4. Launch the app and call the JNI method from Java/Kotlin.
5. Assert the return value matches the unprotected baseline.
6. Capture logcat around the call:
   ```bash
   adb logcat -c
   adb shell am start -n com.example.demo/.MainActivity
   adb logcat -d | grep -E 'NativeBridge|VMPacker|AndroidRuntime'
   ```

For the built-in generated smoke APK path:

```bash
make android-apk-smoke
make android-apk-workflow-smoke
# or directly:
scripts/android-build-smoke-apk.sh libnative_demo.vmp.so .omx/specs/apk-smoke
```

Expected logcat line:

```text
VMPackerSmoke: check(1234)=29711 check(1111)=19398
```

`make android-apk-workflow-smoke` additionally verifies the first-class `vmpacker -apk ... -lib ... -o protected.apk` path. Expected APK workflow artifacts:

- `build/android/apk-workflow/protected.apk`
- `build/android/apk-workflow/protected.apk.report.json`
- report contains `lib_path: lib/arm64-v8a/libnative_demo.so`, debug signing mode, embedded ELF report, and `status: ok`.

### Tier 2b: Android native executable smoke

1. Build an owned Android arm64 native executable/PIE with a small exported function.
2. Pack it with `-target android -func <function>` or `-addr <range:name>`.
3. Push baseline and packed binaries to `/data/local/tmp`, mark executable, and compare output/exit code:
   ```bash
   adb push native_bin native_bin.vmp /data/local/tmp/vmpacker-arm64/
   adb shell 'chmod 755 /data/local/tmp/vmpacker-arm64/native_bin*'
   adb shell 'cd /data/local/tmp/vmpacker-arm64 && ./native_bin && ./native_bin.vmp'
   ```

Repository shortcut:

```bash
make android-native-smoke
make android-addsegment-native-smoke
```

### Tier 3: root-assisted diagnostics (lab only)

- Use `su` only to inspect maps/files after the app-UID smoke path is working:
  ```bash
  adb shell su -c 'pidof com.example.demo'
  adb shell su -c 'cat /proc/$(pidof com.example.demo)/maps | grep libdemo'
  ```
- Do not make root a runtime dependency for normal APK/JNI calls.

## Pass criteria

- Local static checks pass or document missing host tools.
- Device check shows authorized `arm64-v8a` hardware.
- Protected JNI function returns the same value as baseline under normal app UID.
- Packed Android native executable returns the same output/exit status as baseline when run from `/data/local/tmp`.
- No `AndroidRuntime` crash, linker load error, SELinux denial relevant to the protected library, or VM interpreter range error appears in logcat.

## Current environment evidence

The attached device is authorized, arm64-v8a, Android SDK 35, and `su` works from adb shell. Xcode license acceptance is complete, Homebrew Go is available, and Android SDK platform/build-tools 35 are installed for local APK smoke tests.
