#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

NDK_REVISION = "29.0.14206865"
ROOT = Path(__file__).resolve().parents[1]
COMMON_PATH = ROOT / "scripts/run-device-demo-matrix.py"
spec = importlib.util.spec_from_file_location("device_common", COMMON_PATH)
common = importlib.util.module_from_spec(spec)
spec.loader.exec_module(common)

def fail(message):
    raise RuntimeError(message)

def protect(packer, ndk, source, output, mode, name, abi, work):
    case = {"id": "coverage", "selectors": [{"name": name, "abi": abi}]}
    manifest = work / (output.name + ".protect.json")
    common.manifest_for(case, manifest)
    common.run([str(packer), "-ndk", str(ndk), "-mode", mode, "-manifest", str(manifest),
                "-force", "-o", str(output), str(source)])

def build_and_run_native(case_id, source, packed, runner=None):
    return common.execute_case({"id": case_id}, source, packed, runner)

def malformed_attempt(packer, ndk, malformed, output):
    if output.is_symlink() or output.exists():
        output.unlink()
    result = subprocess.run([str(packer), "-ndk", str(ndk), "-mode", "native",
                             "-func", "protected_calc", "-abi", "i32(i32)",
                             "-o", str(output), str(malformed)],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode == 0 or output.exists():
        fail("malformed ELF was not rejected atomically")
    normalize = lambda data: hashlib.sha256(data.replace(b"\r\n", b"\n").replace(b"\r", b"\n")).hexdigest()
    return {"exit_code": result.returncode, "signal": None,
            "stdout_sha256": normalize(result.stdout), "stderr_sha256": normalize(result.stderr),
            "side_effect_sha256": common.EMPTY_SHA256}

def main(argv=None):
    parser = argparse.ArgumentParser(description="run VMPackerGO semantic release fixtures on one physical device")
    parser.add_argument("--ndk", required=True, type=Path)
    parser.add_argument("--packer", required=True, type=Path)
    parser.add_argument("--qualification", required=True, type=Path)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--work", type=Path, default=Path("build/device-coverage"))
    args = parser.parse_args(argv)
    try:
        qualification = json.loads(args.qualification.read_text())
        if qualification.get("physical") is not True or qualification.get("abi") != "arm64-v8a":
            fail("qualification is not a physical arm64-v8a device")
        if not qualification.get("bti") or not qualification.get("pac"):
            fail("coverage device must expose BTI and PAC capability")
        cpu_features = set(qualification.get("cpu_features", []))
        if "lse" not in cpu_features:
            fail("coverage device must expose the LSE feature profile")
        ndk = args.ndk.resolve()
        packer = args.packer.resolve()
        clang = common.ndk_clang(ndk)
        clangxx = clang.with_name(clang.name.replace("clang", "clang++"))
        if not clangxx.is_file():
            fail("exact-r29 Android clang++ was not found")
        work = args.work.resolve()
        work.mkdir(parents=True, exist_ok=True)
        device_id = qualification["id_hash"]
        runs = []

        # Shared object + dynamic loading.
        so_dir = work / "shared"
        so_dir.mkdir(exist_ok=True)
        baseline_so = so_dir / "baseline.so"
        common.run([str(clang), "-shared", "-fPIC", "-O2", "-g", "-mbranch-protection=pac-ret+bti",
                    str(ROOT / "testdata/android/so_jni/native_demo.c"), "-o", str(baseline_so)])
        so_runner = so_dir / "runner"
        common.run([str(clang), "-O2", "-fPIE", "-pie", str(ROOT / "testdata/android/so_jni/runner.c"),
                    "-ldl", "-o", str(so_runner)])
        packed_so = so_dir / "packed.so"
        protect(packer, ndk, baseline_so, packed_so, "so",
                "Java_com_example_demo_NativeBridge_checkLicense", "i32(ptr,ptr,i32)", so_dir)
        runs.append({"case_id": "shared-dlopen", "device_id": device_id,
                     "tags": ["shared_object", "dynamic_load"],
                     "attempts": build_and_run_native("coverage-shared", baseline_so, packed_so, so_runner)})

        # PIE + ASLR + BTI/PAC.
        pie_dir = work / "pie"
        pie_dir.mkdir(exist_ok=True)
        baseline_pie = pie_dir / "baseline"
        common.run([str(clang), "-O2", "-g", "-fPIE", "-pie", "-mbranch-protection=pac-ret+bti",
                    str(ROOT / "testdata/android/native_bin/native_bin.c"), "-o", str(baseline_pie)])
        packed_pie = pie_dir / "packed"
        protect(packer, ndk, baseline_pie, packed_pie, "native", "protected_calc", "i32(i32)", pie_dir)
        runs.append({"case_id": "pie-aslr-bti-pac", "device_id": device_id,
                     "tags": ["pie", "aslr", "bti", "pac"],
                     "attempts": build_and_run_native("coverage-pie", baseline_pie, packed_pie)})

        # Static non-PIE ET_EXEC, avoiding Android's dynamic-PIE loader rule.
        exec_dir = work / "et-exec"
        exec_dir.mkdir(exist_ok=True)
        baseline_exec = exec_dir / "baseline"
        common.run([str(clang), "-O2", "-g", "-static", "-fno-pie", "-no-pie",
                    str(ROOT / "testdata/android/native_bin/native_bin.c"), "-o", str(baseline_exec)])
        packed_exec = exec_dir / "packed"
        protect(packer, ndk, baseline_exec, packed_exec, "native", "protected_calc", "i32(i32)", exec_dir)
        runs.append({"case_id": "static-et-exec", "device_id": device_id, "tags": ["et_exec"],
                     "attempts": build_and_run_native("coverage-et-exec", baseline_exec, packed_exec)})

        # Multithreaded LSE contention through a protected function.
        atomic_dir = work / "atomic"
        atomic_dir.mkdir(exist_ok=True)
        baseline_atomic = atomic_dir / "baseline"
        common.run([str(clang), "-O2", "-g", "-fPIE", "-pie", "-pthread", "-march=armv8.1-a+lse",
                    str(ROOT / "testdata/android/coverage/atomic_contention.c"), "-o", str(baseline_atomic)])
        packed_atomic = atomic_dir / "packed"
        protect(packer, ndk, baseline_atomic, packed_atomic, "native", "protected_increment", "u64(ptr)", atomic_dir)
        runs.append({"case_id": "atomic-contention", "device_id": device_id,
                     "tags": ["atomics_contention"], "threads": 4, "iterations": 10000,
                     "attempts": build_and_run_native("coverage-atomic", baseline_atomic, packed_atomic)})

        # Native throw -> protected landing/catch/destructor -> protected rethrow -> native catch.
        exc_dir = work / "exception"
        exc_dir.mkdir(exist_ok=True)
        baseline_exc = exc_dir / "baseline"
        common.run([str(clangxx), "-O2", "-g", "-fPIE", "-pie", "-fexceptions", "-frtti",
                    "-static-libstdc++", "-mbranch-protection=pac-ret+bti",
                    str(ROOT / "testdata/android/coverage/exception_bridge.cpp"), "-o", str(baseline_exc)])
        packed_exc = exc_dir / "packed"
        protect(packer, ndk, baseline_exc, packed_exc, "native", "protected_exception_bridge", "i32(i32)", exc_dir)
        runs.append({"case_id": "exception-unwind", "device_id": device_id,
                     "tags": ["exception_throw", "exception_catch", "exception_destructor", "exception_rethrow"],
                     "attempts": build_and_run_native("coverage-exception", baseline_exc, packed_exc)})

        # Host-side deterministic malformed-input rejection is recorded separately.
        malformed = work / "malformed.elf"
        malformed.write_bytes(baseline_pie.read_bytes()[:64])
        reject_attempts = []
        for index in range(3):
            first = malformed_attempt(packer, ndk, malformed, work / f"bad-{index}-a")
            second = malformed_attempt(packer, ndk, malformed, work / f"bad-{index}-b")
            if first != second:
                fail("malformed rejection is not deterministic")
            reject_attempts.append({"baseline": first, "packed": second})
        runs.append({"case_id": "malformed-reject", "device_id": device_id, "scope": "host",
                     "tags": ["malformed_reject"], "attempts": reject_attempts})

        manifest_raw = (ROOT / "demo/manifest.json").read_bytes()
        commit = subprocess.check_output(["git", "-C", str(ROOT), "rev-parse", "HEAD"], text=True).strip()
        document = {"schema_version": 1, "commit_sha": commit, "ndk_revision": NDK_REVISION,
                    "manifest_sha256": hashlib.sha256(manifest_raw).hexdigest(),
                    "devices": [qualification], "demo_runs": [], "coverage_runs": runs}
        args.out.parent.mkdir(parents=True, exist_ok=True)
        old_umask = os.umask(0o077)
        try:
            args.out.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
            args.out.chmod(0o600)
        finally:
            os.umask(old_umask)
        for item in runs:
            print(f"PASS {item['case_id']}")
    except (OSError, json.JSONDecodeError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"device coverage matrix failed: {exc}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
