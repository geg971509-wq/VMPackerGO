#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

NDK_REVISION = "29.0.14206865"
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()

def fail(message):
    raise RuntimeError(message)

def run(command, *, cwd=None, env=None, input_bytes=None):
    result = subprocess.run(command, cwd=cwd, env=env, input=input_bytes,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode != 0:
        raise RuntimeError("command failed")
    return result.stdout

def ndk_clang(ndk: Path):
    props = (ndk / "source.properties").read_text()
    if f"Pkg.Revision = {NDK_REVISION}" not in props and f"Pkg.Revision={NDK_REVISION}" not in props:
        fail(f"Android NDK {NDK_REVISION} is required")
    prebuilt = ndk / "toolchains/llvm/prebuilt"
    for host in ("darwin-arm64", "darwin-x86_64"):
        clang = prebuilt / host / "bin/aarch64-linux-android23-clang"
        if clang.is_file():
            return clang
    fail("exact-r29 Android clang was not found")

def march(features):
    suffix = []
    for feature in ("lse", "crc", "crypto"):
        if feature in features:
            suffix.append(feature)
    return "armv8-a" + "".join("+" + item for item in suffix)

def manifest_for(case, path: Path):
    functions = []
    for selector in case["selectors"]:
        match = re.fullmatch(r"(void|[iu](?:8|16|32|64)|ptr)\((.*)\)", selector["abi"])
        if not match:
            fail(f"{case['id']}: invalid ABI in device case spec")
        params = [] if match.group(2) == "" else match.group(2).split(",")
        functions.append({"name": selector["name"], "abi": {"params": params, "result": match.group(1)}})
    path.write_text(json.dumps({"schema_version": 1, "functions": functions}, indent=2) + "\n")

def build_case(root: Path, case, work: Path, clang: Path):
    case_dir = work / case["id"]
    case_dir.mkdir(parents=True, exist_ok=True)
    profile = case["build_profile"]
    source = root / case["source"]
    features = case.get("features", [])
    if profile == "c-pie":
        output = case_dir / "baseline"
        command = [str(clang), "-O2", "-g", "-fPIE", "-pie", f"-march={march(features)}",
                   str(source), "-o", str(output)]
        run(command)
        return output, None, "native"
    if profile == "c-freestanding":
        output = case_dir / "baseline"
        command = [str(clang), "-O2", "-g", "-fPIE", "-pie", "-nostdlib", "-Wl,-e,_start",
                   f"-march={march(features)}", str(source), "-o", str(output)]
        run(command)
        return output, None, "native"
    if profile == "rust-pie":
        if shutil.which("cargo") is None:
            fail("Rust device case requires cargo")
        env = os.environ.copy()
        env["CARGO_TARGET_AARCH64_LINUX_ANDROID_LINKER"] = str(clang)
        env["CC_aarch64_linux_android"] = str(clang)
        crate = root / "demo/demo_rust_test"
        run(["cargo", "build", "--locked", "--release", "--target", "aarch64-linux-android"], cwd=crate, env=env)
        source_binary = crate / "target/aarch64-linux-android/release/demo_rust_test"
        output = case_dir / "baseline"
        shutil.copy2(source_binary, output)
        return output, None, "native"
    if profile == "go-c-shared":
        if shutil.which("go") is None:
            fail("Go device case requires Go")
        output = case_dir / "baseline.so"
        env = os.environ.copy()
        env.update({"GOOS": "android", "GOARCH": "arm64", "CGO_ENABLED": "1", "CC": str(clang)})
        run(["go", "build", "-trimpath", "-buildmode=c-shared", "-o", str(output), "./demo/demo_go_test"], cwd=root, env=env)
        runner = case_dir / "runner"
        run([str(clang), "-O2", "-fPIE", "-pie", str(root / "demo/demo_go_test/runner.c"), "-ldl", "-o", str(runner)])
        return output, runner, "so"
    fail(f"{case['id']}: unsupported build profile {profile}")

def adb_base():
    command = ["adb"]
    if os.environ.get("ANDROID_SERIAL"):
        command += ["-s", os.environ["ANDROID_SERIAL"]]
    return command

def adb(*args, capture=True):
    command = adb_base() + list(args)
    result = subprocess.run(command, stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE)
    if result.returncode != 0:
        raise RuntimeError("adb operation failed")
    return result.stdout if capture else b""

def normalized_hash(data):
    normalized = data.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
    return hashlib.sha256(normalized).hexdigest()

def aapcs64_observation(data):
    lines = [line for line in data.splitlines() if line.startswith(b"AAPCS64 ")]
    if not lines:
        return None
    try:
        fields = dict(token.split("=", 1) for token in lines[-1].decode("ascii", "strict").split()[1:])
        registers = {f"x{index}" for index in range(19, 30)} | {"sp"}
        if set(fields) != {"return", "memory", *registers}:
            raise RuntimeError("AAPCS64 observation has an unexpected field set")
        if any(len(fields[key]) != 16 for key in ("return", *registers)) or len(fields["memory"]) != 48:
            raise RuntimeError("AAPCS64 observation has an invalid value width")
        bytes.fromhex("".join(fields[key] for key in ("return", *registers, "memory")))
    except (UnicodeError, ValueError) as exc:
        raise RuntimeError("AAPCS64 observation is not valid ASCII key/value data") from exc
    return {
        "profile": "aapcs64-callee-saved",
        "return_values": {"x0": fields["return"]},
        "callee_saved": {key: fields[key] for key in sorted(registers, key=lambda item: (item == "sp", item))},
        "memory_sha256": hashlib.sha256(bytes.fromhex(fields["memory"])).hexdigest(),
    }

def remote_result(remote_dir, executable, *, capture_aapcs64=False):
    prefix = re.sub(r"[^A-Za-z0-9_.-]", "_", executable.replace("/", "_"))
    out = f"{remote_dir}/{prefix}.stdout"
    err = f"{remote_dir}/{prefix}.stderr"
    code = f"{remote_dir}/{prefix}.exit"
    shell = f"cd {remote_dir}; {executable} >{out} 2>{err}; rc=$?; printf '%s' \"$rc\" >{code}"
    adb("shell", "sh", "-c", shell)
    stdout = adb("exec-out", "cat", out)
    stderr = adb("exec-out", "cat", err)
    raw_code = adb("exec-out", "cat", code).decode("ascii", "strict").strip()
    exit_code = int(raw_code)
    signal = f"SIGNAL_{exit_code - 128}" if 128 <= exit_code <= 255 else None
    result = {"exit_code": exit_code, "signal": signal,
            "stdout_sha256": normalized_hash(stdout), "stderr_sha256": normalized_hash(stderr),
            "side_effect_sha256": EMPTY_SHA256}
    observation = aapcs64_observation(stdout) if capture_aapcs64 else None
    if observation is not None:
        result["aapcs64"] = {key: value for key, value in observation.items() if key != "memory_sha256"}
        result["side_effect_sha256"] = observation["memory_sha256"]
    return result

def execute_case(case, baseline: Path, packed: Path, runner: Path | None, *, capture_aapcs64=False):
    remote_dir = f"/data/local/tmp/vmpacker-evidence-{case['id']}-{os.getpid()}"
    adb("shell", "rm", "-rf", remote_dir)
    adb("shell", "mkdir", "-p", remote_dir)
    try:
        if runner is None:
            adb("push", str(baseline), f"{remote_dir}/baseline")
            adb("push", str(packed), f"{remote_dir}/packed")
            adb("shell", "chmod", "700", f"{remote_dir}/baseline", f"{remote_dir}/packed")
            baseline_cmd = "./baseline"
            packed_cmd = "./packed"
        else:
            adb("push", str(runner), f"{remote_dir}/runner")
            adb("push", str(baseline), f"{remote_dir}/baseline.so")
            adb("push", str(packed), f"{remote_dir}/packed.so")
            adb("shell", "chmod", "700", f"{remote_dir}/runner")
            baseline_cmd = "./runner ./baseline.so"
            packed_cmd = "./runner ./packed.so"
        attempts = []
        for _ in range(3):
            baseline_result = remote_result(remote_dir, baseline_cmd, capture_aapcs64=capture_aapcs64)
            packed_result = remote_result(remote_dir, packed_cmd, capture_aapcs64=capture_aapcs64)
            if baseline_result != packed_result:
                fail(f"{case['id']}: baseline and packed execution differ")
            attempts.append({"baseline": baseline_result, "packed": packed_result})
        return attempts
    finally:
        try:
            adb("shell", "rm", "-rf", remote_dir)
        except RuntimeError as exc:
            print(f"warning: could not clean remote evidence directory {remote_dir}: {exc}",
                  file=sys.stderr)


def main(argv=None):
    parser = argparse.ArgumentParser(description="run VMPackerGO 85-demo differential matrix on one physical device")
    parser.add_argument("--ndk", required=True, type=Path)
    parser.add_argument("--packer", required=True, type=Path)
    parser.add_argument("--qualification", required=True, type=Path)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--work", type=Path, default=Path("build/device-demo-matrix"))
    parser.add_argument("--case", action="append", dest="selected")
    args = parser.parse_args(argv)
    root = Path(__file__).resolve().parents[1]
    try:
        subprocess.run([sys.executable, str(root / "scripts/validate-demo-cases.py")], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        qualification = json.loads(args.qualification.read_text())
        if qualification.get("physical") is not True or qualification.get("abi") != "arm64-v8a":
            fail("qualification is not an attested physical arm64-v8a device")
        clang = ndk_clang(args.ndk.resolve())
        cases = json.loads((root / "demo/device-cases.json").read_text())["entries"]
        if args.selected:
            wanted = set(args.selected)
            known = {case["id"] for case in cases}
            if not wanted <= known:
                fail("unknown --case selection")
            cases = [case for case in cases if case["id"] in wanted]
        available = set(qualification.get("cpu_features", []))
        work = args.work.resolve()
        work.mkdir(parents=True, exist_ok=True)
        runs = []
        for case in cases:
            required = set(case.get("features", []))
            if not required <= available:
                fail(f"{case['id']}: device lacks required CPU feature profile")
            baseline, runner, mode = build_case(root, case, work, clang)
            case_dir = baseline.parent
            selection_manifest = case_dir / "protect.json"
            manifest_for(case, selection_manifest)
            packed = case_dir / ("packed.so" if mode == "so" else "packed")
            try:
                run([str(args.packer.resolve()), "-ndk", str(args.ndk.resolve()), "-mode", mode,
                     "-manifest", str(selection_manifest), "-force", "-o", str(packed), str(baseline)])
            except RuntimeError:
                fail(f"{case['id']}: packing failed")
            attempts = execute_case(case, baseline, packed, runner)
            runs.append({"demo_id": case["id"], "device_id": qualification["id_hash"], "attempts": attempts})
            print(f"PASS {case['id']}")

        manifest_raw = (root / "demo/manifest.json").read_bytes()
        commit = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
        document = {"schema_version": 1, "commit_sha": commit, "ndk_revision": NDK_REVISION,
                    "manifest_sha256": hashlib.sha256(manifest_raw).hexdigest(),
                    "devices": [qualification], "demo_runs": runs, "coverage_runs": []}
        args.out.parent.mkdir(parents=True, exist_ok=True)
        old_umask = os.umask(0o077)
        try:
            args.out.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
            args.out.chmod(0o600)
        finally:
            os.umask(old_umask)
    except (OSError, json.JSONDecodeError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"device demo matrix failed: {exc}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
