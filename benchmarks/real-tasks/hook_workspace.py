import io
import json
import os
from pathlib import Path
import sys
import tempfile
from unittest.mock import patch

from agent_trace.hooks import hook_main
from agent_trace.store import TraceStore

with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    process, project = root / "plugin", root / "project"
    process.mkdir(); project.mkdir()
    os.chdir(process)
    for key in ("AGENT_TRACE_DIR", "AGENT_TRACE_CLAUDE_SESSION_ID", "AGENT_TRACE_COPILOT_SESSION_ID"):
        os.environ.pop(key, None)

    def start(provider, session, cwd=None):
        payload = {"sessionId" if provider == "copilot" else "session_id": session, "source": "startup"}
        if cwd is not None:
            payload["cwd"] = cwd
        with patch.object(sys, "stdin", io.StringIO(json.dumps(payload))):
            hook_main(["--provider", provider, "session-start"])

    for provider in ("copilot", "claude"):
        session = provider + "-project-session"
        start(provider, session, str(project))
        assert TraceStore(project / ".agent-traces").load_meta(session[:16]) is not None
    assert not (process / ".agent-traces").exists()
    start("copilot", "next-invocation")
    assert TraceStore(process / ".agent-traces").load_meta("next-invocation") is not None
    explicit = root / "explicit"
    os.environ["AGENT_TRACE_DIR"] = str(explicit)
    start("claude", "override-session", str(project))
    assert TraceStore(explicit).load_meta("override-session") is not None
    os.environ.pop("AGENT_TRACE_DIR")
    file = root / "file"; file.write_text("not a directory")
    for index, invalid in enumerate((str(file), "relative", [str(project)], "\x00")):
        session = f"fallback-{index}"
        start("copilot", session, invalid)
        assert TraceStore(process / ".agent-traces").load_meta(session) is not None
    os.chdir(root)
print("PASS: workspace selection, override, fallback and invocation isolation")
