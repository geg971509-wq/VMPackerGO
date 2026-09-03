#!/usr/bin/env python3
import argparse
import json
import os
import sys
from pathlib import Path

KEYS = ("schema_version", "commit_sha", "ndk_revision", "manifest_sha256")

def main(argv=None):
    parser = argparse.ArgumentParser(description="merge VMPackerGO physical-device evidence fragments")
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("inputs", nargs="+", type=Path)
    args = parser.parse_args(argv)
    try:
        documents = [json.loads(path.read_text()) for path in args.inputs]
        if not documents:
            raise ValueError("at least one evidence fragment is required")
        first = documents[0]
        for index, document in enumerate(documents[1:], 2):
            for key in KEYS:
                if document.get(key) != first.get(key):
                    raise ValueError(f"fragment {index} disagrees on {key}")
        devices = {}
        demo_runs = []
        coverage_runs = []
        for document in documents:
            for device in document.get("devices", []):
                device_id = device.get("id_hash")
                if not isinstance(device_id, str):
                    raise ValueError("device fragment lacks id_hash")
                if device_id in devices and devices[device_id] != device:
                    raise ValueError("same device id_hash has conflicting qualification metadata")
                devices[device_id] = device
            demo_runs.extend(document.get("demo_runs", []))
            coverage_runs.extend(document.get("coverage_runs", []))
        demo_runs.sort(key=lambda run: (run.get("demo_id", ""), run.get("device_id", "")))
        coverage_runs.sort(key=lambda run: (run.get("case_id", ""), run.get("device_id", "")))
        merged = {key: first[key] for key in KEYS}
        merged["devices"] = [devices[key] for key in sorted(devices)]
        merged["demo_runs"] = demo_runs
        merged["coverage_runs"] = coverage_runs
        args.out.parent.mkdir(parents=True, exist_ok=True)
        old_umask = os.umask(0o077)
        try:
            args.out.write_text(json.dumps(merged, indent=2, sort_keys=True) + "\n")
            args.out.chmod(0o600)
        finally:
            os.umask(old_umask)
    except (OSError, json.JSONDecodeError, KeyError, ValueError) as exc:
        print(f"device evidence merge failed: {exc}", file=sys.stderr)
        return 1
    print(f"merged {len(args.inputs)} device evidence fragment(s)")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
