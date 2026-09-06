#!/usr/bin/env python3
"""Run a prepared five-task baseline once, with a durable total budget reservation."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import tempfile


def save(path, value):
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as temporary:
        json.dump(value, temporary, indent=2); temporary.write("\n"); temporary.flush(); os.fsync(temporary.fileno())
    os.replace(temporary.name, path)
    directory = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prepared", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path, help="new directory; existing journals are never replayed")
    parser.add_argument("--harness", default="harness")
    parser.add_argument("--max-usd", required=True, type=float)
    parser.add_argument("--api-key-file", type=Path)
    args = parser.parse_args()
    if not math.isfinite(args.max_usd) or not 0 < args.max_usd <= 100:
        parser.error("--max-usd must be greater than zero and at most 100")
    prepared = args.prepared.resolve()
    manifest = json.loads((prepared / "prepared.json").read_text())
    cases, reservation = [], 0.0
    for case in manifest["cases"]:
        spec = json.loads((prepared / case["task"]).read_text())
        verifier = (prepared / spec["verifier"]).resolve()
        if hashlib.sha256(verifier.read_bytes()).hexdigest() != case["verifier_sha256"]:
            raise RuntimeError("verifier changed since corpus validation")
        if spec["ref"] != case["base"] or spec["image"] != manifest["image"] or spec["goal"] != case["goal"]:
            raise RuntimeError("prepared task changed; validate the corpus again")
        cap = spec["limits"]["max_usd"]
        if not math.isfinite(cap) or not 0 < cap <= 100:
            raise RuntimeError("invalid case reservation")
        reservation += cap
        spec["repository"] = str((prepared / spec["repository"]).resolve())
        spec["verifier"] = str(verifier)
        cases.append((case, spec))
    if len(cases) != 5 or reservation > args.max_usd + 1e-9:
        raise RuntimeError(f"five-case reservation ${reservation:.4f} exceeds cap or corpus is incomplete")
    output = args.output.resolve(); output.mkdir(mode=0o700, parents=True, exist_ok=False)
    journal = {"schema": 1, "started_at": datetime.now(timezone.utc).isoformat(), "status": "running", "reserved_usd": reservation, "cap_usd": args.max_usd, "estimated_usd_uncached": 0.0, "billing_unknown": False, "cases": []}
    path = output / "baseline.json"
    save(path, journal)
    for case, spec in cases:
        # Copy the verifier into the private controller directory before dispatch.
        verifier = output / (case["id"] + ".verify.sh")
        verifier.write_bytes(Path(spec["verifier"]).read_bytes()); verifier.chmod(0o600)
        spec["verifier"] = str(verifier)
        task = output / (case["id"] + ".task.json"); save(task, spec)
        row = {"id": case["id"], "role": case["role"], "reserved_usd": spec["limits"]["max_usd"], "status": "dispatching", "billing_unknown": True}
        journal["cases"].append(row); journal["billing_unknown"] = True; save(path, journal)
        command = [args.harness, "run", "--task", str(task), "--state-dir", str(output / "state")]
        if args.api_key_file:
            command += ["--api-key-file", str(args.api_key_file.resolve())]
        # CLI owns the key lookup and request budget. No script sees its value.
        try:
            with (output / (case["id"] + ".report.json")).open("w") as stdout, (output / (case["id"] + ".log")).open("w") as stderr:
                result = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=390)
            report_path = output / (case["id"] + ".report.json")
            report = json.loads(report_path.read_text())
        except (OSError, subprocess.TimeoutExpired, json.JSONDecodeError):
            journal["status"] = "needs_reconciliation"
            save(path, journal)
            raise RuntimeError("dispatch did not return a complete report; inspect the recorded controller state before another experiment") from None
        row.update({"status": "finished", "exit_code": result.returncode, "run_id": report.get("run_id"), "verified": report.get("verified", False), "state": report.get("state"), "reason": report.get("reason"), "estimated_usd_uncached": report.get("estimated_usd_uncached", 0), "billing_unknown": report.get("billing_unknown", True), "observation": report.get("observation"), "cleanup_confirmed": report.get("cleanup_confirmed", False)})
        journal["estimated_usd_uncached"] += row["estimated_usd_uncached"]
        journal["billing_unknown"] = any(c["billing_unknown"] for c in journal["cases"])
        save(path, journal)
        print(f"{case['id']}: {row['state']}; verified={row['verified']}; estimate=${row['estimated_usd_uncached']:.6f}", flush=True)
        if row["billing_unknown"] or report.get("external_outcome_unknown") or not row["cleanup_confirmed"]:
            journal["status"] = "needs_reconciliation"; save(path, journal)
            raise RuntimeError("stopped on an uncertain outcome; inspect and reconcile the recorded run without repeating this script")
    journal["status"] = "complete"
    journal["verified_count"] = sum(c["verified"] and c["state"] == "completed" for c in journal["cases"])
    save(path, journal)
    print(f"Baseline: {journal['verified_count']}/5 independently verified. Evidence: {path}")


if __name__ == "__main__":
    # Protect reports, verifiers and transcripts as well as the atomic journal.
    os.umask(0o077)
    main()
