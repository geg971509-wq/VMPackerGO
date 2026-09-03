#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import platform
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEVICE_VALIDATOR_PATH = ROOT / "scripts/validate-device-evidence.py"
spec = importlib.util.spec_from_file_location("device_evidence", DEVICE_VALIDATOR_PATH)
device_evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(device_evidence)

NDK_REVISION = "29.0.14206865"
HEX40 = re.compile(r"^[0-9a-f]{40}$")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
PRERELEASE_ID = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
SEMVER = re.compile(
    rf"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    rf"(?:-{PRERELEASE_ID}(?:\.{PRERELEASE_ID})*)?$"
)

class ReleaseEvidenceError(ValueError):
    pass

def require(condition, message):
    if not condition:
        raise ReleaseEvidenceError(message)

def sha256(path: Path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

def git(root: Path, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()

def root_manifest(root: Path):
    raw = (root / "demo/manifest.json").read_bytes()
    manifest = json.loads(raw)
    ids = [entry["id"] for entry in manifest["entries"]]
    return ids, hashlib.sha256(raw).hexdigest()

def safe_sibling(base: Path, name, label):
    require(isinstance(name, str) and name and Path(name).name == name and not Path(name).is_absolute(),
            f"{label}.file must be a basename beside the evidence JSON")
    path = base / name
    require(not path.is_symlink(), f"{label}.file must be a regular file, not a symbolic link")
    require(path.is_file(), f"{label} file {name!r} is missing")
    return path

def validate_document(document, evidence_path: Path, root: Path, *, live_checks=True):
    require(isinstance(document, dict), "release evidence root must be an object")
    require(document.get("schema_version") == 1, "schema_version must be 1")
    tag = document.get("tag")
    commit = document.get("commit_sha")
    require(isinstance(tag, str) and SEMVER.fullmatch(tag),
            "tag must be a v-prefixed SemVer release tag without build metadata")
    require(isinstance(commit, str) and HEX40.fullmatch(commit), "commit_sha must be lowercase 40-hex")
    require(document.get("go_version") == "go" + (root / ".go-version").read_text().strip(),
            "go_version does not match .go-version")
    require(document.get("ndk_revision") == NDK_REVISION, f"ndk_revision must be {NDK_REVISION}")
    require(git(root, "rev-parse", "HEAD") == commit, "release evidence commit_sha does not match checkout HEAD")
    require(git(root, "status", "--porcelain") == "", "release checkout must be clean")
    require(git(root, "rev-list", "-n", "1", tag) == commit, "release tag does not point at commit_sha")
    require(git(root, "describe", "--tags", "--exact-match", "HEAD") == tag, "HEAD is not exactly the release tag")

    base = evidence_path.parent
    refs = {}
    for label in ("device_evidence", "artifact", "source"):
        item = document.get(label)
        require(isinstance(item, dict) and set(item) == {"file", "sha256"},
                f"{label} must contain file and sha256 only")
        require(isinstance(item["sha256"], str) and HEX64.fullmatch(item["sha256"]),
                f"{label}.sha256 must be lowercase SHA-256 hex")
        path = safe_sibling(base, item["file"], label)
        require(sha256(path) == item["sha256"], f"{label} SHA-256 mismatch")
        refs[label] = path

    ids, manifest_sha = root_manifest(root)
    device_document = json.loads(refs["device_evidence"].read_text())
    device_evidence.validate_document(device_document, ids, manifest_sha, commit)

    signing = document.get("signing")
    require(isinstance(signing, dict), "signing must be an object")
    require(signing.get("kind") == "Developer ID Application", "release signing kind must be Developer ID Application")
    for key in ("hardened_runtime", "timestamped", "codesign_valid"):
        require(signing.get(key) is True, f"signing.{key} must be true")

    notarization = document.get("notarization")
    require(isinstance(notarization, dict), "notarization must be an object")
    require(notarization.get("status") == "Accepted", "notarization.status must be Accepted")
    require(isinstance(notarization.get("submission_id"), str) and notarization["submission_id"].strip(),
            "notarization.submission_id must be non-empty")
    require(notarization.get("ticket_mode") == "online",
            "standalone CLI notarization.ticket_mode must be online")
    require(notarization.get("spctl_accepted") is True, "notarization.spctl_accepted must be true")

    review = document.get("independent_review")
    require(isinstance(review, dict), "independent_review must be an object")
    require(isinstance(review.get("reviewer"), str) and review["reviewer"].strip(),
            "independent_review.reviewer must be non-empty")
    require(review.get("reviewed_commit") == commit, "independent review is for a different commit")
    require(review.get("result") == "approved", "independent_review.result must be approved")

    artifact = refs["artifact"]
    source = refs["source"]
    require(artifact.name == "vmpacker-darwin-arm64", "canonical release artifact must be vmpacker-darwin-arm64")
    require(source.name == f"vmpacker-{tag}-source.tar.gz", "source archive filename does not match release tag")

    if live_checks:
        require(platform.system() == "Darwin", "live release validation must run on macOS")
        file_output = subprocess.check_output(["file", str(artifact)], text=True)
        require("Mach-O" in file_output and "arm64" in file_output, "release artifact is not macOS ARM64 Mach-O")
        subprocess.run(["codesign", "--verify", "--deep", "--strict", "--verbose=2", str(artifact)], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        details = subprocess.check_output(["codesign", "-dvv", str(artifact)], text=True, stderr=subprocess.STDOUT)
        require("Runtime Version" in details or "flags=0x10000(runtime)" in details,
                "codesign output does not prove hardened runtime")
        require("Authority=Developer ID Application" in details,
                "codesign authority is not Developer ID Application")
        subprocess.run(["spctl", "--assess", "--type", "execute", "--verbose=2", str(artifact)], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return True

def main(argv=None):
    parser = argparse.ArgumentParser(description="validate VMPackerGO release evidence")
    parser.add_argument("evidence", type=Path)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--no-live-checks", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    try:
        evidence_path = args.evidence.resolve()
        document = json.loads(evidence_path.read_text())
        validate_document(document, evidence_path, args.root.resolve(), live_checks=not args.no_live_checks)
    except (OSError, json.JSONDecodeError, subprocess.CalledProcessError, ReleaseEvidenceError,
            device_evidence.EvidenceError) as exc:
        print(f"release evidence invalid: {exc}", file=sys.stderr)
        return 1
    print("release evidence valid")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
