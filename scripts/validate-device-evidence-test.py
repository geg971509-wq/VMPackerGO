#!/usr/bin/env python3
import copy
import importlib.util
from pathlib import Path

path = Path(__file__).with_name("validate-device-evidence.py")
spec = importlib.util.spec_from_file_location("device_evidence", path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

COMMIT = "a" * 40
MANIFEST_SHA = "b" * 64
HASH = "0" * 64
DEVICE_4K = "c" * 64
DEVICE_16K = "d" * 64
DEMO_IDS = [f"demo_{i:03d}" for i in range(85)]
RESULT = {"exit_code": 0, "signal": None, "stdout_sha256": HASH,
          "stderr_sha256": HASH, "side_effect_sha256": HASH}

def attempts():
    return [{"baseline": copy.deepcopy(RESULT), "packed": copy.deepcopy(RESULT)} for _ in range(3)]

def valid_document():
    runs = []
    for demo_id in DEMO_IDS:
        runs.append({"demo_id": demo_id, "device_id": DEVICE_4K, "attempts": attempts()})
        runs.append({"demo_id": demo_id, "device_id": DEVICE_16K, "attempts": attempts()})
    return {
        "schema_version": 1, "commit_sha": COMMIT,
        "ndk_revision": module.NDK_REVISION, "manifest_sha256": MANIFEST_SHA,
        "devices": [
            {"id_hash": DEVICE_4K, "physical": True, "abi": "arm64-v8a", "api": 23,
             "page_size": 4096, "bti": True, "pac": True},
            {"id_hash": DEVICE_16K, "physical": True, "abi": "arm64-v8a", "api": 35,
             "page_size": 16384, "bti": True, "pac": True},
        ],
        "demo_runs": runs,
        "coverage_runs": [{"case_id": "matrix", "device_id": DEVICE_16K,
                           "tags": sorted(module.REQUIRED_TAGS), "threads": 4,
                           "iterations": 1000, "attempts": attempts()}],
    }

def expect_invalid(document, label):
    try:
        module.validate_document(document, DEMO_IDS, MANIFEST_SHA, COMMIT)
    except module.EvidenceError:
        return
    raise AssertionError(f"invalid evidence passed: {label}")

def main():
    document = valid_document()
    assert module.validate_document(document, DEMO_IDS, MANIFEST_SHA, COMMIT)

    mismatch = copy.deepcopy(document)
    mismatch["demo_runs"][0]["attempts"][0]["packed"]["exit_code"] = 1
    expect_invalid(mismatch, "behavior mismatch")

    incomplete = copy.deepcopy(document)
    incomplete["demo_runs"] = [r for r in incomplete["demo_runs"]
                               if not (r["demo_id"] == DEMO_IDS[0] and r["device_id"] == DEVICE_16K)]
    expect_invalid(incomplete, "missing 16K coverage")

    nonphysical = copy.deepcopy(document)
    nonphysical["devices"][0]["physical"] = False
    expect_invalid(nonphysical, "nonphysical device")

    wrong_commit = copy.deepcopy(document)
    wrong_commit["commit_sha"] = "e" * 40
    expect_invalid(wrong_commit, "wrong commit")

    print("device evidence validator self-test passed")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
