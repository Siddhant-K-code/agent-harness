#!/usr/bin/env python3
"""Exercise an actual packaged CLI in a disposable home, without model requests."""
import hashlib
import json
import os
import pty
from pathlib import Path
import select
import subprocess
import sys
import tarfile
import tempfile
import termios
import time


archive = Path(sys.argv[1]).resolve()
with tempfile.TemporaryDirectory(prefix="harness-package-") as temporary:
    root = Path(temporary).resolve()
    bundle = root / "bundle"
    bundle.mkdir()
    with tarfile.open(archive) as package:
        for member in package.getmembers():
            assert not member.issym() and not member.islnk()
            assert not Path(member.name).is_absolute() and ".." not in Path(member.name).parts
        package.extractall(bundle)
    env = {k: v for k, v in os.environ.items() if not k.startswith(("OPENAI_", "AWS_", "GIT_"))}
    env.update(HOME=str(root / "home"), XDG_CONFIG_HOME=str(root / "config"))
    Path(env["HOME"]).mkdir()

    def run(*args, ok=True, cwd=root, input=None):
        p = subprocess.run(args, cwd=cwd, env=env, input=input, text=True, capture_output=True, timeout=40)
        assert (p.returncode == 0) == ok, (args, p.returncode, p.stdout, p.stderr)
        assert "package-test-credential" not in p.stdout + p.stderr, "credential leaked"
        return p.stdout

    bindir = root / "installed bin"
    installer = str(bundle / "install.sh")
    run("sh", installer, "--bin-dir", str(bindir))
    binary = str(bindir / "harness")
    assert "commit" in run(binary, "version")
    assert "independent verification" in run(binary, "--help")
    run(binary, "run", "--help")
    run(binary, "auth", "login", "--help")
    run(binary, "unknown-command", ok=False)
    run("sh", installer, "--bin-dir", str(bindir), ok=False)
    run("sh", installer, "--bin-dir", str(bindir), "--force")
    before = hashlib.sha256(Path(binary).read_bytes()).hexdigest()
    with (bundle / "harness").open("ab") as f:
        f.write(b"corruption")
    run("sh", installer, "--bin-dir", str(bindir), "--force", ok=False)
    assert hashlib.sha256(Path(binary).read_bytes()).hexdigest() == before
    run(binary, "auth", "status", ok=False)
    # Exercise hidden input on a real pseudo-terminal, not only the pipe path.
    master, slave = pty.openpty()
    process = subprocess.Popen([binary, "auth", "login"], cwd=root, env=env, stdin=slave, stdout=slave, stderr=slave)
    transcript = b""
    deadline = time.monotonic() + 15
    try:
        while b"OpenAI API key (hidden): " not in transcript or termios.tcgetattr(slave)[3] & termios.ECHO:
            assert time.monotonic() < deadline, "hidden prompt did not become ready"
            if select.select([master], [], [], 0.05)[0]:
                transcript += os.read(master, 8192)
        os.write(master, b"package-test-credential\n")
        assert process.wait(timeout=15) == 0
        while select.select([master], [], [], 0.05)[0]:
            transcript += os.read(master, 8192)
        assert b"package-test-credential" not in transcript, "terminal echoed key"
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
        os.close(slave)
    run(binary, "auth", "logout")
    run(binary, "auth", "login", "--stdin", input="package-test-credential\n")
    status = json.loads(run(binary, "auth", "status"))
    assert status["configured"] and not status["api_validated"]
    assert status["credential"]["source"] == "user_config"
    key_path = Path(status["credential"]["path"])
    assert key_path.stat().st_mode & 0o777 == 0o600
    assert key_path.parent.stat().st_mode & 0o777 == 0o700
    run(binary, "auth", "login", "--stdin", input="package-test-credential\n", ok=False)
    run(binary, "init", "demo with spaces")
    demo = root / "demo with spaces"
    task = json.loads((demo / "harness.task.json").read_text())
    assert task["model"] == "gpt-5.4" and task["limits"]["max_usd"] == 0.5
    run(binary, "init", str(demo), ok=False)
    run("git", "-C", str(demo / "repo"), "rev-parse", "HEAD")
    run(binary, "init", "--repo", str(demo / "repo"), "--goal", "Fix tags", "--image", "node:22-alpine", "--verifier", str(demo / "harness.verify.sh"), "own-task")
    own = json.loads((root / "own-task" / "harness.task.json").read_text())
    assert len(own["ref"]) == 40
    catalog = json.loads(run(binary, "models", "--json"))
    assert {p["id"]: p["context_window_tokens"] for p in catalog}["gpt-5.4-mini"] == 400000
    before_config = (demo / "harness.task.json").read_bytes()
    preview = json.loads(run(binary, "config", "show", "--model", "gpt-5.4-mini", "--context-window", "128000", cwd=demo))
    assert preview["effective"]["model"] == "gpt-5.4-mini" and not preview["saved"]
    assert (demo / "harness.task.json").read_bytes() == before_config
    configured = json.loads(run(binary, "config", "set", "--model", "gpt-5.4-mini-2026-03-17", "--context-window", "128000", "--max-output-tokens", "4096", "--max-total-tokens", "250000", cwd=demo))
    assert configured["saved"] and configured["effective"]["max_input_tokens"] == 123904
    saved = json.loads((demo / "harness.task.json").read_text())
    assert saved["repository"] == "repo" and saved["verifier"] == "harness.verify.sh"
    before_config = (demo / "harness.task.json").read_bytes()
    for flags in [("--context-window", "500000"), ("--max-output-tokens", "128000"), ("--max-total-tokens", "1"), ("--model", "unknown")]:
        run(binary, "config", "set", *flags, cwd=demo, ok=False)
        assert (demo / "harness.task.json").read_bytes() == before_config
    # Invalid per-run overrides are rejected before creating a run database.
    run(binary, "run", "--context-window", "500000", cwd=demo, ok=False)
    assert not (demo / ".harness" / "harness.db").exists()
    assert (demo / "harness.task.json").read_bytes() == before_config
    run(binary, "init", "--model", "gpt-5.4-mini", "--context-window", "64000", "--max-output-tokens", "4096", "--max-total-tokens", "200000", "configured-demo")
    initialized = json.loads((root / "configured-demo" / "harness.task.json").read_text())
    assert initialized["limits"]["context_window_tokens"] == 64000 and initialized["limits"]["max_output_tokens"] == 4096
    # An unpriced model must fail diagnostics, without ever calling the provider.
    task["model"] = "unpriced-model"
    (demo / "harness.task.json").write_text(json.dumps(task))
    diagnostic = json.loads(run(binary, "doctor", "--json", cwd=demo, ok=False))
    assert any(c["name"] == "Model" and not c["ok"] for c in diagnostic["checks"])
    assert not (demo / ".harness" / "harness.db").exists()
    assert not (demo / ".harness" / "runs").exists()
    run(binary, "auth", "logout")
    assert not key_path.exists()
    run(binary, "auth", "status", ok=False)
print("PASS: native package install/upgrade, corruption rejection, CLI setup, local BYOK privacy, real Git tasks, model/context configuration, unpaid diagnostics")
