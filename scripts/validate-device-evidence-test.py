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
REJECTION = {"exit_code": 1, "signal": None, "stdout_sha256": HASH,
             "stderr_sha256": HASH, "side_effect_sha256": HASH}
SIGNALLED = {"exit_code": -9, "signal": None, "stdout_sha256": HASH,
             "stderr_sha256": HASH, "side_effect_sha256": HASH}
AAPCS64 = {
    "profile": module.AAPCS64_PROFILE,
    "return_values": {"x0": "000000000000008f"},
    "callee_saved": {**{f"x{index}": f"{index:016x}" for index in range(19, 30)},
                      "sp": "0000000000001000"},
}

def attempts(result=RESULT, *, aapcs64=False):
    value = copy.deepcopy(result)
    if aapcs64:
        value["aapcs64"] = copy.deepcopy(AAPCS64)
    return [{"baseline": copy.deepcopy(value), "packed": copy.deepcopy(value)} for _ in range(3)]

def valid_document():
    runs = []
    for demo_id in DEMO_IDS:
        runs.append({"demo_id": demo_id, "device_id": DEVICE_4K, "attempts": attempts()})
        runs.append({"demo_id": demo_id, "device_id": DEVICE_16K, "attempts": attempts()})
    success_tags = sorted(module.REQUIRED_TAGS - {"malformed_reject"})
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
        "coverage_runs": [
            {"case_id": "matrix", "device_id": DEVICE_16K,
             "tags": success_tags, "threads": 4,
             "iterations": 1000, "attempts": attempts(aapcs64=True)},
            {"case_id": "malformed", "device_id": DEVICE_16K,
             "tags": ["malformed_reject"], "attempts": attempts(REJECTION)},
        ],
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

    equivalent_failure = copy.deepcopy(document)
    equivalent_failure["demo_runs"][0]["attempts"] = attempts(REJECTION)
    expect_invalid(equivalent_failure, "equivalent demo failure")

    malformed_success = copy.deepcopy(document)
    malformed_success["coverage_runs"][1]["attempts"] = attempts()
    expect_invalid(malformed_success, "successful malformed rejection")

    malformed_signal = copy.deepcopy(document)
    malformed_signal["coverage_runs"][1]["attempts"] = attempts(SIGNALLED)
    expect_invalid(malformed_signal, "signal termination masquerading as malformed rejection")

    mixed_malformed = copy.deepcopy(document)
    mixed_malformed["coverage_runs"][1]["tags"] = ["malformed_reject", "exception_throw"]
    expect_invalid(mixed_malformed, "malformed rejection satisfying success coverage")

    missing_aapcs64 = copy.deepcopy(document)
    missing_aapcs64["coverage_runs"][0]["attempts"] = attempts()
    expect_invalid(missing_aapcs64, "missing AAPCS64 observation")

    changed_callee_saved = copy.deepcopy(document)
    changed_callee_saved["coverage_runs"][0]["attempts"][0]["packed"]["aapcs64"]["callee_saved"]["x19"] = "0" * 16
    expect_invalid(changed_callee_saved, "changed AAPCS64 callee-saved register")

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
