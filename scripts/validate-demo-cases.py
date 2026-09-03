#!/usr/bin/env python3
import json
import re
import sys
from pathlib import Path

ALLOWED_ABI = re.compile(r"^(?:void|[iu](?:8|16|32|64)|ptr)\((?:(?:[iu](?:8|16|32|64)|ptr)(?:,(?:[iu](?:8|16|32|64)|ptr))*)?\)$")
PROFILES = {"c-pie", "c-freestanding", "rust-pie", "go-c-shared"}
FEATURES = {"lse", "crc", "crypto"}

def fail(message):
    raise ValueError(message)

def validate(root: Path):
    manifest = json.loads((root / "demo/manifest.json").read_text())
    cases = json.loads((root / "demo/device-cases.json").read_text())
    if manifest.get("schema_version") != 1 or cases.get("schema_version") != 1:
        fail("manifest and device case schemas must both be version 1")
    manifest_entries = manifest.get("entries")
    case_entries = cases.get("entries")
    if not isinstance(manifest_entries, list) or not isinstance(case_entries, list):
        fail("entries must be arrays")
    if len(manifest_entries) != 85 or len(case_entries) != 85:
        fail(f"expected exactly 85 manifest and case entries; got {len(manifest_entries)} and {len(case_entries)}")
    manifest_by_id = {entry.get("id"): entry for entry in manifest_entries}
    case_by_id = {entry.get("id"): entry for entry in case_entries}
    if len(manifest_by_id) != 85 or len(case_by_id) != 85 or set(manifest_by_id) != set(case_by_id):
        fail("device case IDs must exactly match the unique 85-demo manifest IDs")

    for demo_id in sorted(manifest_by_id):
        source = manifest_by_id[demo_id]
        case = case_by_id[demo_id]
        if case.get("language") != source.get("language") or case.get("source") != source.get("source"):
            fail(f"{demo_id}: source/language provenance mismatch")
        if not (root / source["source"]).is_file():
            fail(f"{demo_id}: source file is missing")
        if case.get("outcome") != "equivalent":
            fail(f"{demo_id}: unresolved or non-equivalent device outcome {case.get('outcome')!r}")
        if case.get("build_profile") not in PROFILES:
            fail(f"{demo_id}: invalid build_profile")
        selectors = case.get("selectors")
        if not isinstance(selectors, list) or not selectors:
            fail(f"{demo_id}: at least one explicit protected selector is required")
        seen = set()
        for selector in selectors:
            if not isinstance(selector, dict) or set(selector) != {"name", "abi"}:
                fail(f"{demo_id}: selector must contain name and abi only")
            name, abi = selector["name"], selector["abi"]
            if not isinstance(name, str) or not name or name in seen:
                fail(f"{demo_id}: selector names must be unique and non-empty")
            seen.add(name)
            if not isinstance(abi, str) or ALLOWED_ABI.fullmatch(abi) is None:
                fail(f"{demo_id}:{name}: ABI {abi!r} is outside the protected-entry contract")
        features = case.get("features", [])
        if not isinstance(features, list) or any(feature not in FEATURES for feature in features):
            fail(f"{demo_id}: invalid feature list")

    go_case = case_by_id["demo_go_test"]
    if go_case["build_profile"] != "go-c-shared" or go_case["selectors"] != [{"abi": "u64(u64)", "name": "check_key"}]:
        fail("demo_go_test must use the explicit c-shared AAPCS64 check_key boundary")
    if not (root / "demo/demo_go_test/runner.c").is_file():
        fail("demo_go_test C ABI runner is missing")
    rust_case = case_by_id["demo_rust_test"]
    if {s["name"] for s in rust_case["selectors"]} != {"check_key"}:
        fail("demo_rust_test must use its explicit extern-C check_key boundary")
    return True

def main():
    root = Path(__file__).resolve().parents[1]
    try:
        validate(root)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"demo device cases invalid: {exc}")
        return 1
    print("demo device cases valid: 85/85")
    return 0

if __name__ == "__main__":
    sys.exit(main())
