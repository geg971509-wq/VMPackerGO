#!/usr/bin/env python3
import argparse
import os
import re
import subprocess
import sys

TARGETS = [
    ("./internal/arch/arm64", "FuzzDecoderPolicyNeverPanics"),
    ("./internal/elf", "FuzzELFMetadataNeverPanics"),
    ("./internal/unwind", "FuzzEHFrameNeverPanics"),
    ("./internal/unwind", "FuzzLSDANeverPanics"),
]
DURATION_RE = re.compile(r"^[1-9][0-9]*(?:ms|s|m)$")


def main(argv=None):
    parser = argparse.ArgumentParser(description="run bounded mutation fuzzing for VMPackerGO host parsers/decoder")
    parser.add_argument("--fuzztime", default=os.environ.get("VMPACKER_FUZZTIME", "3s"))
    parser.add_argument("--parallel", type=int, default=2)
    args = parser.parse_args(argv)
    if DURATION_RE.fullmatch(args.fuzztime) is None:
        parser.error("--fuzztime must be a positive duration such as 500ms, 3s, or 1m")
    if args.parallel < 1 or args.parallel > 8:
        parser.error("--parallel must be between 1 and 8")

    go = os.environ.get("GO", "go")
    for package, target in TARGETS:
        print(f"[fuzz] {package} {target} for {args.fuzztime}", flush=True)
        command = [
            go,
            "test",
            package,
            "-run=^$",
            f"-fuzz=^{target}$",
            f"-fuzztime={args.fuzztime}",
            f"-parallel={args.parallel}",
        ]
        completed = subprocess.run(command)
        if completed.returncode != 0:
            return completed.returncode
    print(f"host fuzz smoke passed: {len(TARGETS)} targets", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
