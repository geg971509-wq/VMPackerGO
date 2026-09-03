#!/usr/bin/env python3
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def run(command, *, env=None):
    print("[rehearsal]", " ".join(command), flush=True)
    subprocess.run(command, cwd=ROOT, env=env, check=True)


def main():
    try:
        run([sys.executable, "scripts/validate-demo-cases.py"])
        run([sys.executable, "scripts/validate-device-evidence-test.py"])
        run([sys.executable, "scripts/validate-release-evidence-test.py"])
        run(["bash", "scripts/check-contract.sh"])
        run(["bash", "scripts/check-contract-test.sh"])

        env = os.environ.copy()
        env.pop("VMPACKER_RELEASE_EVIDENCE", None)
        completed = subprocess.run(
            ["bash", "scripts/check-contract.sh", "--release"],
            cwd=ROOT,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        combined = completed.stdout + completed.stderr
        if completed.returncode == 0:
            raise RuntimeError("release contract unexpectedly passed without external evidence")
        if "release evidence is required in VMPACKER_RELEASE_EVIDENCE" not in combined:
            raise RuntimeError("release contract failed for an unexpected reason")
        print("release rehearsal passed: missing external evidence remains fail-closed", flush=True)
        return 0
    except (OSError, subprocess.CalledProcessError, RuntimeError) as exc:
        print(f"release rehearsal failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
