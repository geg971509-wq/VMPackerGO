#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path

HEX40 = re.compile(r"^[0-9a-f]{40}$")

def main(argv=None):
    parser = argparse.ArgumentParser(description="record an independent approval on a VMPackerGO release-evidence draft")
    parser.add_argument("draft", type=Path)
    parser.add_argument("--reviewer", required=True)
    parser.add_argument("--approve", action="store_true")
    parser.add_argument("--out", type=Path, default=Path("release-evidence.json"))
    args = parser.parse_args(argv)
    if not args.approve:
        print("explicit --approve is required", file=sys.stderr)
        return 2
    reviewer = args.reviewer.strip()
    if not reviewer:
        print("reviewer identity must be non-empty", file=sys.stderr)
        return 2
    try:
        data = json.loads(args.draft.read_text())
        commit = data.get("commit_sha")
        if not isinstance(commit, str) or HEX40.fullmatch(commit) is None:
            raise ValueError("draft commit_sha is invalid")
        root = Path(__file__).resolve().parents[1]
        head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
        if head != commit:
            raise ValueError("draft does not describe the current checkout")
        data["independent_review"] = {
            "reviewer": reviewer,
            "reviewed_commit": commit,
            "result": "approved",
        }
        args.out.parent.mkdir(parents=True, exist_ok=True)
        old_umask = os.umask(0o077)
        try:
            args.out.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")
            args.out.chmod(0o600)
        finally:
            os.umask(old_umask)
    except (OSError, json.JSONDecodeError, subprocess.CalledProcessError, ValueError) as exc:
        print(f"cannot record independent review: {exc}", file=sys.stderr)
        return 1
    print(f"recorded independent review by {reviewer}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
