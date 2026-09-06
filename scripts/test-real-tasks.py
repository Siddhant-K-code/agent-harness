#!/usr/bin/env python3
"""Test budget admission and interrupted-dispatch accounting; never call a model."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

module_spec = importlib.util.spec_from_file_location("baseline", Path(__file__).with_name("run-real-tasks.py"))
baseline = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(baseline)


class AccountingTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.prepared = self.root / "prepared"; self.prepared.mkdir()
        self.output = self.root / "baseline"
        verifier = self.prepared / "verify.sh"; verifier.write_text("exit 1\n")
        cases = []
        for index in range(5):
            name = f"case-{index}"
            spec = {"ref": "a" * 40, "image": "sha256:" + "b" * 64, "goal": name, "repository": str(self.root), "verifier": "verify.sh", "limits": {"max_usd": 0.15}}
            (self.prepared / (name + ".json")).write_text(json.dumps(spec))
            cases.append({"id": name, "task": name + ".json", "base": spec["ref"], "goal": name, "role": "regression", "verifier_sha256": hashlib.sha256(verifier.read_bytes()).hexdigest()})
        (self.prepared / "prepared.json").write_text(json.dumps({"image": spec["image"], "cases": cases}))

    def run_baseline(self, cap="0.75"):
        with patch.object(sys, "argv", ["baseline", "--prepared", str(self.prepared), "--output", str(self.output), "--max-usd", cap]):
            baseline.main()

    def test_budget_and_changed_verifier_reject_before_dispatch(self):
        with patch.object(subprocess, "run") as dispatch:
            with self.assertRaises(RuntimeError):
                self.run_baseline("0.74")
            self.assertFalse(self.output.exists())
            (self.prepared / "verify.sh").write_text("exit 0\n")
            with self.assertRaises(RuntimeError):
                self.run_baseline()
            dispatch.assert_not_called()

    def test_existing_journal_is_not_replayed(self):
        self.output.mkdir()
        with patch.object(subprocess, "run") as dispatch:
            with self.assertRaises(FileExistsError):
                self.run_baseline()
            dispatch.assert_not_called()

    def test_interrupted_dispatch_has_durable_unknown_reservation(self):
        def interrupt(*args, **kwargs):
            journal = json.loads((self.output / "baseline.json").read_text())
            self.assertEqual(journal["reserved_usd"], 0.75)
            self.assertEqual(journal["cases"][0]["status"], "dispatching")
            self.assertTrue(journal["billing_unknown"])
            raise subprocess.TimeoutExpired("harness", 390)
        with patch.object(subprocess, "run", side_effect=interrupt) as dispatch:
            with self.assertRaises(RuntimeError):
                self.run_baseline()
            self.assertEqual(dispatch.call_count, 1)
        journal = json.loads((self.output / "baseline.json").read_text())
        self.assertEqual(journal["status"], "needs_reconciliation")
        self.assertTrue(journal["billing_unknown"])

    def test_reported_unknown_outcome_stops_batch(self):
        def response(*args, **kwargs):
            kwargs["stdout"].write(json.dumps({"run_id": "a" * 32, "state": "failed", "verified": False, "estimated_usd_uncached": 0.02, "billing_unknown": True, "cleanup_confirmed": True}))
            return subprocess.CompletedProcess(args[0], 1)
        with patch.object(subprocess, "run", side_effect=response) as dispatch:
            with self.assertRaises(RuntimeError):
                self.run_baseline()
            self.assertEqual(dispatch.call_count, 1)
        journal = json.loads((self.output / "baseline.json").read_text())
        self.assertEqual(journal["status"], "needs_reconciliation")
        self.assertEqual(journal["estimated_usd_uncached"], 0.02)

    def test_confirmed_failures_are_kept_in_denominator(self):
        def response(*args, **kwargs):
            kwargs["stdout"].write(json.dumps({"run_id": "b" * 32, "state": "failed", "verified": False, "estimated_usd_uncached": 0.01, "billing_unknown": False, "cleanup_confirmed": True}))
            return subprocess.CompletedProcess(args[0], 1)
        with patch.object(subprocess, "run", side_effect=response) as dispatch:
            self.run_baseline()
            self.assertEqual(dispatch.call_count, 5)
        journal = json.loads((self.output / "baseline.json").read_text())
        self.assertEqual(journal["verified_count"], 0)
        self.assertEqual(len(journal["cases"]), 5)
        self.assertFalse(journal["billing_unknown"])


if __name__ == "__main__":
    unittest.main()
