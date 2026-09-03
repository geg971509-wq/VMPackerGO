#!/usr/bin/env python3
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path

NDK_REVISION = "29.0.14206865"
HEX40 = re.compile(r"^[0-9a-f]{40}$")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
REQUIRED_TAGS = {
    "shared_object", "pie", "et_exec", "dynamic_load", "aslr", "bti", "pac",
    "atomics_contention", "exception_throw", "exception_catch",
    "exception_destructor", "exception_rethrow", "malformed_reject",
}
FORBIDDEN_KEYS = {
    "serial", "device_serial", "ndk_path", "home", "home_path", "seed",
    "opcode_map", "encryption_key", "signing_credentials", "apple_password",
}

class EvidenceError(ValueError):
    pass

def _require(condition, message):
    if not condition:
        raise EvidenceError(message)

def _walk_forbidden(value, path="$"):
    if isinstance(value, dict):
        for key, child in value.items():
            _require(key not in FORBIDDEN_KEYS, f"{path}: forbidden evidence key {key!r}")
            _walk_forbidden(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _walk_forbidden(child, f"{path}[{index}]")

def _validate_result(value, where):
    _require(isinstance(value, dict), f"{where} must be an object")
    _require(set(value) == {"exit_code", "signal", "stdout_sha256", "stderr_sha256", "side_effect_sha256"},
             f"{where} has unexpected or missing result fields")
    _require(isinstance(value["exit_code"], int) and not isinstance(value["exit_code"], bool),
             f"{where}.exit_code must be an integer")
    signal = value["signal"]
    _require(signal is None or (isinstance(signal, str) and signal.strip()),
             f"{where}.signal must be null or a non-empty string")
    for key in ("stdout_sha256", "stderr_sha256", "side_effect_sha256"):
        _require(isinstance(value[key], str) and HEX64.fullmatch(value[key]) is not None,
                 f"{where}.{key} must be lowercase SHA-256 hex")

def _validate_attempts(attempts, where):
    _require(isinstance(attempts, list) and len(attempts) >= 3,
             f"{where}.attempts must contain at least three executions")
    for index, attempt in enumerate(attempts):
        _require(isinstance(attempt, dict) and set(attempt) == {"baseline", "packed"},
                 f"{where}.attempts[{index}] must contain baseline and packed only")
        _validate_result(attempt["baseline"], f"{where}.attempts[{index}].baseline")
        _validate_result(attempt["packed"], f"{where}.attempts[{index}].packed")
        _require(attempt["baseline"] == attempt["packed"],
                 f"{where}.attempts[{index}] baseline/packed behavior differs")

def validate_document(document, manifest_ids, manifest_sha256, expected_commit):
    _walk_forbidden(document)
    _require(isinstance(document, dict), "evidence root must be an object")
    _require(document.get("schema_version") == 1, "schema_version must be 1")
    _require(document.get("ndk_revision") == NDK_REVISION,
             f"ndk_revision must be {NDK_REVISION}")
    commit = document.get("commit_sha")
    _require(isinstance(commit, str) and HEX40.fullmatch(commit) is not None,
             "commit_sha must be lowercase 40-hex")
    _require(commit == expected_commit, "evidence commit_sha does not match the certified checkout")
    _require(document.get("manifest_sha256") == manifest_sha256,
             "manifest_sha256 does not match demo/manifest.json")

    devices = document.get("devices")
    _require(isinstance(devices, list) and devices, "devices must be a non-empty array")
    device_by_id = {}
    for index, device in enumerate(devices):
        where = f"devices[{index}]"
        _require(isinstance(device, dict), f"{where} must be an object")
        device_id = device.get("id_hash")
        _require(isinstance(device_id, str) and HEX64.fullmatch(device_id) is not None,
                 f"{where}.id_hash must be lowercase SHA-256 hex")
        _require(device_id not in device_by_id, f"duplicate device id_hash {device_id}")
        _require(device.get("physical") is True, f"{where} is not recorded as physical")
        _require(device.get("abi") == "arm64-v8a", f"{where}.abi must be arm64-v8a")
        api = device.get("api")
        _require(isinstance(api, int) and not isinstance(api, bool) and api >= 23,
                 f"{where}.api must be >= 23")
        _require(device.get("page_size") in (4096, 16384),
                 f"{where}.page_size must be 4096 or 16384")
        _require(isinstance(device.get("bti"), bool) and isinstance(device.get("pac"), bool),
                 f"{where}.bti and .pac must be booleans")
        device_by_id[device_id] = device

    _require(any(d["api"] == 23 for d in devices), "release evidence requires an Android API 23 physical device")
    _require(any(d["api"] > 23 for d in devices), "release evidence requires a representative later Android API device")
    _require({d["page_size"] for d in devices} == {4096, 16384},
             "release evidence requires both 4 KiB and 16 KiB page-size devices")
    _require(any(d["bti"] for d in devices), "release evidence has no BTI-capable device/path")
    _require(any(d["pac"] for d in devices), "release evidence has no PAC-capable device/path")

    manifest_ids = set(manifest_ids)
    _require(len(manifest_ids) == 85, f"approved manifest must contain exactly 85 unique IDs, found {len(manifest_ids)}")
    demo_coverage = {demo_id: set() for demo_id in manifest_ids}
    demo_runs = document.get("demo_runs")
    _require(isinstance(demo_runs, list), "demo_runs must be an array")
    for index, run in enumerate(demo_runs):
        where = f"demo_runs[{index}]"
        _require(isinstance(run, dict), f"{where} must be an object")
        demo_id = run.get("demo_id")
        device_id = run.get("device_id")
        _require(demo_id in manifest_ids, f"{where}.demo_id {demo_id!r} is not in the approved manifest")
        _require(device_id in device_by_id, f"{where}.device_id is unknown")
        _validate_attempts(run.get("attempts"), where)
        demo_coverage[demo_id].add(device_by_id[device_id]["page_size"])
    for demo_id, page_sizes in sorted(demo_coverage.items()):
        _require(page_sizes == {4096, 16384},
                 f"demo {demo_id!r} lacks passing 4 KiB and 16 KiB physical-device coverage")

    observed_tags = set()
    coverage_runs = document.get("coverage_runs")
    _require(isinstance(coverage_runs, list), "coverage_runs must be an array")
    for index, run in enumerate(coverage_runs):
        where = f"coverage_runs[{index}]"
        _require(isinstance(run, dict), f"{where} must be an object")
        _require(isinstance(run.get("case_id"), str) and run["case_id"].strip(),
                 f"{where}.case_id must be non-empty")
        device_id = run.get("device_id")
        _require(device_id in device_by_id, f"{where}.device_id is unknown")
        tags = run.get("tags")
        _require(isinstance(tags, list) and tags and all(isinstance(tag, str) and tag for tag in tags),
                 f"{where}.tags must be a non-empty string array")
        _validate_attempts(run.get("attempts"), where)
        observed_tags.update(tags)
        if "bti" in tags:
            _require(device_by_id[device_id]["bti"], f"{where} claims BTI on a non-BTI device/path")
        if "pac" in tags:
            _require(device_by_id[device_id]["pac"], f"{where} claims PAC on a non-PAC device/path")
        if "atomics_contention" in tags:
            _require(isinstance(run.get("threads"), int) and run["threads"] >= 2,
                     f"{where}.threads must be >= 2 for atomics_contention")
            _require(isinstance(run.get("iterations"), int) and run["iterations"] >= 1,
                     f"{where}.iterations must be >= 1 for atomics_contention")
    missing = sorted(REQUIRED_TAGS - observed_tags)
    _require(not missing, "coverage_runs are missing required tags: " + ", ".join(missing))
    return True

def _load_manifest(root):
    path = root / "demo" / "manifest.json"
    raw = path.read_bytes()
    manifest = json.loads(raw)
    _require(manifest.get("schema_version") == 1, "demo manifest schema_version must be 1")
    _require(manifest.get("ndk_revision") == NDK_REVISION, "demo manifest NDK revision mismatch")
    entries = manifest.get("entries")
    _require(isinstance(entries, list), "demo manifest entries must be an array")
    ids = [entry.get("id") for entry in entries if isinstance(entry, dict)]
    _require(len(ids) == len(entries) and all(isinstance(item, str) and item for item in ids),
             "demo manifest contains an invalid ID")
    _require(len(set(ids)) == len(ids), "demo manifest contains duplicate IDs")
    return ids, hashlib.sha256(raw).hexdigest()

def _head(root):
    result = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"],
                            check=True, capture_output=True, text=True)
    return result.stdout.strip()

def main(argv=None):
    parser = argparse.ArgumentParser(description="validate VMPackerGO physical-device evidence")
    parser.add_argument("evidence", type=Path)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args(argv)
    try:
        root = args.root.resolve()
        document = json.loads(args.evidence.read_text())
        ids, manifest_sha = _load_manifest(root)
        validate_document(document, ids, manifest_sha, _head(root))
    except (OSError, json.JSONDecodeError, subprocess.CalledProcessError, EvidenceError) as exc:
        print(f"device evidence invalid: {exc}")
        return 1
    print("device evidence valid")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
