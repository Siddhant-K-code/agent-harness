#!/usr/bin/env python3
"""Prepare and validate historical repository tasks without calling a model.

Requires Python 3.12+, Git and Docker. Verification runs in read-only containers
without network access. Model runs are a separate, explicitly budgeted command.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

REPO = Path(__file__).resolve().parents[1]
CORPUS = REPO / "benchmarks" / "real-tasks"
GIT_ENV = {"PATH": os.environ["PATH"], "HOME": "/nonexistent", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_TERMINAL_PROMPT": "0", "GIT_LFS_SKIP_SMUDGE": "1"}


def git(repo, *args):
    result = subprocess.run(["git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", str(repo), *args], env=GIT_ENV, capture_output=True, text=True, timeout=180)
    if result.returncode:
        raise RuntimeError("Git preparation failed: " + result.stderr[-2000:])
    return result.stdout.strip()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


def validate_case(repo, case, verifier, image, output):
    outcomes = []
    for label in ("base", "reference"):
        revision = case[label]
        if git(repo, "rev-parse", "--verify", revision + "^{commit}") != revision:
            raise RuntimeError("revision does not resolve exactly")
        # Keep mounts beside the output, in a directory shared with Docker/Colima.
        with tempfile.TemporaryDirectory(prefix="harness-corpus-", dir=output) as temporary:
            root = Path(temporary)
            archive = root / "source.tar"
            git(repo, "archive", "--format=tar", "--output=" + str(archive), revision)
            checkout = root / "workspace"; checkout.mkdir()
            with tarfile.open(archive) as tar:
                tar.extractall(checkout, filter="data")
            # No host commands from the target repository are executed.
            command = ["docker", "run", "--rm", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--cpus=2", "--user", f"{os.getuid()}:{os.getgid()}", "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m", "-e", "HOME=/tmp", "-e", "PYTHONDONTWRITEBYTECODE=1", "-v", f"{checkout}:/workspace:ro", "-v", f"{verifier}:/verifier.sh:ro", "-w", "/workspace", image, "/bin/sh", "/verifier.sh"]
            result = subprocess.run(command, capture_output=True, text=True, timeout=90)
            log = (result.stdout + result.stderr)[-16000:]
            (output / f"{case['id']}.{label}.log").write_text(log)
            passed = result.returncode == 0 and "PASS:" in result.stdout
            if (label == "reference" and not passed) or (label == "base" and result.returncode == 0):
                raise RuntimeError(f"{case['id']} {label} validation unexpected (exit {result.returncode}):\n{log}")
            if result.returncode in (125, 126, 127, 137):
                raise RuntimeError("container/runtime failure is not a regression result: " + log)
            outcomes.append({"revision": revision, "kind": label, "exit_code": result.returncode, "passed": passed, "log": f"{case['id']}.{label}.log"})
    return outcomes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".harness/real-tasks"))
    parser.add_argument("--sources", type=Path, help="reuse existing clones after checking their origin; never modifies their checkout")
    parser.add_argument("--image", default="python:3.12-slim", help="already prepared local Docker image; its immutable ID is recorded")
    parser.add_argument("--model", default="gpt-5.4")
    parser.add_argument("--case-usd", type=float, default=0.15)
    args = parser.parse_args()
    if sys.version_info < (3, 12):
        parser.error("Python 3.12+ is required for safe archive extraction")
    if not 0 < args.case_usd <= 20:
        parser.error("--case-usd must be greater than zero and at most 20")
    image = subprocess.check_output(["docker", "image", "inspect", "--format", "{{.Id}}", args.image], text=True).strip()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image):
        raise RuntimeError("invalid Docker image ID")
    output = args.output.resolve(); output.mkdir(mode=0o700, parents=True, exist_ok=False)
    manifest = json.loads((CORPUS / "manifest.json").read_text())
    sources = args.sources.resolve() if args.sources else output / "sources"
    sources.mkdir(mode=0o700, parents=True, exist_ok=True)
    clones = {}
    for case in manifest["cases"]:
        repo_name = case["repository"]
        if repo_name in clones:
            continue
        path = sources / repo_name.split("/")[1]
        if not path.exists():
            subprocess.run(["git", "-c", "core.hooksPath=/dev/null", "clone", "--quiet", "--", "https://github.com/" + repo_name + ".git", str(path)], env=GIT_ENV, check=True, timeout=300)
        origin = git(path, "remote", "get-url", "origin").removesuffix(".git").lower()
        if origin not in ("https://github.com/" + repo_name.lower(), "git@github.com:" + repo_name.lower()):
            raise RuntimeError("existing clone has an unexpected origin")
        clones[repo_name] = path
    prepared = {"schema": 1, "name": manifest["name"], "image": image, "image_requested": args.image, "model": args.model, "cases": []}
    for case in manifest["cases"]:
        verifier = output / (case["id"] + ".verify.sh")
        verifier.write_text("#!/bin/sh\nset -eu\nexport PYTHONPATH=/workspace/src:/workspace\nexport PYTHONDONTWRITEBYTECODE=1\npython3 - <<'HARNESS_INDEPENDENT_CHECK'\n" + (CORPUS / case["verifier"]).read_text() + "\nHARNESS_INDEPENDENT_CHECK\n")
        verifier.chmod(0o600)
        outcomes = validate_case(clones[case["repository"]], case, verifier, image, output)
        spec = {"schema_version": 1, "name": case["id"], "goal": case["goal"], "repository": str(clones[case["repository"]]), "ref": case["base"], "model": args.model, "image": image, "verifier": verifier.name, "backend": "docker", "learn": True, "compaction": {"trigger_percent": 75, "keep_recent_turns": 1, "max_summary_tokens": 1024, "max_compactions": 2}, "limits": {"context_window_tokens": 32768, "max_steps": 16, "timeout_ms": 300000, "tool_timeout_ms": 30000, "max_output_tokens": 2048, "max_total_tokens": 50000, "max_repairs": 2, "max_usd": args.case_usd}}
        task_path = output / (case["id"] + ".task.json")
        write(task_path, spec)
        prepared["cases"].append({**case, "task": task_path.name, "validation": outcomes, "verifier_sha256": hashlib.sha256(verifier.read_bytes()).hexdigest()})
        print(case["id"] + ": base fails; upstream fix passes", flush=True)
    write(output / "prepared.json", prepared)
    write(output / "agent-trace.suite.json", {"schema": 1, "name": "AgentTrace historical regressions and reserved hook holdout", "repetitions": 2, "cases": [{"id": case["id"], "task": case["task"], "role": case["role"]} for case in prepared["cases"] if case["repository"].endswith("/agent-trace")]})
    print(f"Prepared {len(prepared['cases'])} cases in {output}. No model requests. Baseline reservation: ${args.case_usd * 5:.2f}; paired comparison: ${args.case_usd * 16:.2f} plus proposal.")


if __name__ == "__main__":
    main()
