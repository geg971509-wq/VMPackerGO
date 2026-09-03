#!/usr/bin/env python3
import copy
import hashlib
import importlib.util
import json
import subprocess
import tempfile
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
VALIDATOR_PATH = SCRIPT_DIR / "validate-release-evidence.py"
spec = importlib.util.spec_from_file_location("release_evidence", VALIDATOR_PATH)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

HASH = hashlib.sha256(b"").hexdigest()
DEV4 = "c" * 64
DEV16 = "d" * 64


def result():
    return {"exit_code": 0, "signal": None, "stdout_sha256": HASH,
            "stderr_sha256": HASH, "side_effect_sha256": HASH}


def attempts():
    return [{"baseline": result(), "packed": result()} for _ in range(3)]


def make_repo(root: Path):
    (root / "demo").mkdir()
    ids = [f"demo_{i:03d}" for i in range(85)]
    manifest = {"schema_version": 1, "ndk_revision": module.NDK_REVISION,
                "android_api": 23,
                "entries": [{"id": item, "language": "c", "source": f"demo/{item}.c"} for item in ids]}
    raw = (json.dumps(manifest, separators=(",", ":")) + "\n").encode()
    (root / "demo/manifest.json").write_bytes(raw)
    (root / ".go-version").write_text("1.26.0\n")
    subprocess.run(["git", "init", "-q", str(root)], check=True)
    subprocess.run(["git", "-C", str(root), "config", "user.name", "test"], check=True)
    subprocess.run(["git", "-C", str(root), "config", "user.email", "test@example.invalid"], check=True)
    subprocess.run(["git", "-C", str(root), "add", "."], check=True)
    subprocess.run(["git", "-C", str(root), "commit", "-qm", "fixture"], check=True)
    subprocess.run(["git", "-C", str(root), "tag", "v1.2.3"], check=True)
    commit = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    return ids, hashlib.sha256(raw).hexdigest(), commit


def device_document(ids, manifest_sha, commit):
    runs = []
    for demo_id in ids:
        runs.append({"demo_id": demo_id, "device_id": DEV4, "attempts": attempts()})
        runs.append({"demo_id": demo_id, "device_id": DEV16, "attempts": attempts()})
    coverage = {"case_id": "coverage", "device_id": DEV16,
                "tags": sorted(module.device_evidence.REQUIRED_TAGS),
                "threads": 4, "iterations": 10, "attempts": attempts()}
    return {"schema_version": 1, "commit_sha": commit,
            "ndk_revision": module.NDK_REVISION, "manifest_sha256": manifest_sha,
            "devices": [
                {"id_hash": DEV4, "physical": True, "abi": "arm64-v8a", "api": 23,
                 "page_size": 4096, "bti": True, "pac": True},
                {"id_hash": DEV16, "physical": True, "abi": "arm64-v8a", "api": 35,
                 "page_size": 16384, "bti": True, "pac": True},
            ], "demo_runs": runs, "coverage_runs": [coverage]}


def expect_invalid(document, path, root, label):
    try:
        module.validate_document(document, path, root, live_checks=False)
    except (module.ReleaseEvidenceError, module.device_evidence.EvidenceError):
        return
    raise AssertionError(f"invalid release evidence passed: {label}")


def main():
    with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as evidence_tmp:
        root = Path(repo_tmp)
        evidence_dir = Path(evidence_tmp)
        ids, manifest_sha, commit = make_repo(root)

        device_path = evidence_dir / "device-evidence.json"
        device_path.write_text(json.dumps(device_document(ids, manifest_sha, commit)) + "\n")
        artifact = evidence_dir / "vmpacker-darwin-arm64"
        artifact.write_bytes(b"signed-artifact-fixture")
        source = evidence_dir / "vmpacker-v1.2.3-source.tar.gz"
        source.write_bytes(b"source-fixture")

        release = {
            "schema_version": 1, "tag": "v1.2.3", "commit_sha": commit,
            "go_version": "go1.26.0", "ndk_revision": module.NDK_REVISION,
            "device_evidence": {"file": device_path.name, "sha256": module.sha256(device_path)},
            "artifact": {"file": artifact.name, "sha256": module.sha256(artifact)},
            "source": {"file": source.name, "sha256": module.sha256(source)},
            "signing": {"kind": "Developer ID Application", "hardened_runtime": True,
                        "timestamped": True, "codesign_valid": True},
            "notarization": {"status": "Accepted", "submission_id": "fixture-submission",
                             "ticket_mode": "online", "spctl_accepted": True},
            "independent_review": {"reviewer": "independent-reviewer", "reviewed_commit": commit,
                                   "result": "approved"},
        }
        evidence_path = evidence_dir / "release-evidence.json"
        evidence_path.write_text(json.dumps(release) + "\n")
        assert module.validate_document(release, evidence_path, root, live_checks=False)

        bad_hash = copy.deepcopy(release)
        bad_hash["artifact"]["sha256"] = "0" * 64
        expect_invalid(bad_hash, evidence_path, root, "artifact hash")

        pending = copy.deepcopy(release)
        pending["independent_review"]["result"] = "pending"
        expect_invalid(pending, evidence_path, root, "pending review")

        wrong_ticket = copy.deepcopy(release)
        wrong_ticket["notarization"]["ticket_mode"] = "stapled"
        expect_invalid(wrong_ticket, evidence_path, root, "standalone stapling claim")

    print("release evidence validator self-test passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
