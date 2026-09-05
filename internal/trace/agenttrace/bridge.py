"""Embedded export bridge. No network calls; use the pinned native AgentTrace API."""
import fcntl
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile

REVISION = "b109ec5b3714b842746e97ee8e975329d8582667"
SCHEMA = "agent-harness.agenttrace.v1"
FILES = {"meta.json", "events.ndjson", "harness.json", "manifest.json"}


def check_dependency():
    dist = importlib.metadata.distribution("agent-strace")
    provenance = json.loads(dist.read_text("direct_url.json") or "{}")
    if provenance.get("vcs_info", {}).get("commit_id") != REVISION:
        raise ValueError("install integrations/agenttrace/requirements.txt in the selected Python environment")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_json(path, value):
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(canonical(value) + b"\n")
        stream.flush()
        os.fsync(stream.fileno())


def read_regular(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 32 << 20:
        raise ValueError("unsafe or oversized export artifact")
    return path.read_bytes()


def validate_session(root, sid):
    from agent_trace.store import TraceStore

    store = TraceStore(root, use_workspace_env=False, redact=True)
    meta = store.load_meta(sid)
    events = store.load_events(sid)
    lines = read_regular(root / sid / "events.ndjson").splitlines()
    previous = ""
    ids = set()
    for event, line in zip(events, lines, strict=True):
        if event.event_id in ids or event.prev_hash != previous:
            raise ValueError("invalid AgentTrace event chain")
        if event.parent_id and event.parent_id not in ids:
            raise ValueError("missing AgentTrace parent")
        ids.add(event.event_id)
        previous = digest(line)
    if meta.session_id != sid or meta.ended_at is None:
        raise ValueError("incomplete AgentTrace session")
    return len(events)


def verify_existing(root, sid, source_digest):
    target = root / sid
    if target.is_symlink() or not target.is_dir():
        raise ValueError("export destination is not a regular session directory")
    if {p.name for p in target.iterdir()} != FILES:
        raise ValueError("existing export has unexpected or missing files")
    manifest = json.loads(read_regular(target / "manifest.json"))
    if manifest.get("projection_sha256") != source_digest:
        raise ValueError("conflicting export; select a different output directory")
    expected = manifest.get("files", {})
    if set(expected) != FILES - {"manifest.json"}:
        raise ValueError("invalid export manifest")
    for name, checksum in expected.items():
        if digest(read_regular(target / name)) != checksum:
            raise ValueError("existing export artifact checksum mismatch")
    count = validate_session(root, sid)
    if count != manifest.get("native_events"):
        raise ValueError("existing export event count mismatch")
    return count


def publish(root, payload):
    check_dependency()
    from agent_trace.models import SessionMeta, TraceEvent
    from agent_trace.redact import redact_data_with_status
    from agent_trace.store import TraceStore

    if payload.get("schema") != SCHEMA or payload.get("agenttrace_revision") != REVISION:
        raise ValueError("unsupported projection version")
    sid = payload.get("session_id", "")
    if not re.fullmatch(r"[a-f0-9]{32}", sid):
        raise ValueError("invalid harness run ID")
    # Redact both the native records and sidecar before hashing or writing.
    payload, _ = redact_data_with_status(payload)
    source_digest = digest(canonical(payload))
    root = Path(root).absolute()
    if root.is_symlink():
        raise ValueError("refusing symlinked export root")
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    if root.stat().st_mode & 0o077:
        raise ValueError("export root must be private (chmod 700)")
    lock_fd = os.open(root / ".harness-export.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, "rb") as lock:
        if not stat.S_ISREG(os.fstat(lock.fileno()).st_mode):
            raise ValueError("invalid export lock")
        fcntl.flock(lock, fcntl.LOCK_EX)
        target = root / sid
        if target.exists() or target.is_symlink():
            count = verify_existing(root, sid, source_digest)
            return {"session_id": sid, "path": str(target), "reused": True, "native_events": count}
        with tempfile.TemporaryDirectory(prefix=".harness-stage-", dir=root) as temporary:
            stage = Path(temporary)
            store = TraceStore(stage, use_workspace_env=False, redact=True)
            meta = SessionMeta.from_json(json.dumps(payload["meta"]))
            if meta.session_id != sid:
                raise ValueError("metadata identity mismatch")
            session = store.create_session(meta)
            for item in payload["events"]:
                event = TraceEvent.from_json(json.dumps(item))
                store.append_event(sid, event)
            store.update_meta(meta)
            sidecar = {"schema": SCHEMA, "coverage": payload["coverage"], "journal": payload["journal"]}
            if "outcome" in payload:
                sidecar["outcome"] = payload["outcome"]
            write_json(session / "harness.json", sidecar)
            count = validate_session(stage, sid)
            hashes = {}
            for name in sorted(FILES - {"manifest.json"}):
                path = session / name
                os.chmod(path, 0o600)
                with path.open("rb") as stream:
                    os.fsync(stream.fileno())
                hashes[name] = digest(read_regular(path))
            write_json(session / "manifest.json", {
                "schema": SCHEMA, "agenttrace_revision": REVISION,
                "projection_sha256": source_digest, "native_events": count,
                "source_high_water_mark": payload["coverage"]["source_high_water_mark"],
                "files": hashes,
            })
            os.chmod(session, 0o700)
            sync_directory(session)
            os.rename(session, target)
            sync_directory(root)
        return {"session_id": sid, "path": str(target), "reused": False, "native_events": count}


if __name__ == "__main__":
    try:
        # Never inherit ambient tenant/workspace or redaction-disabling options.
        for name in list(os.environ):
            if name.startswith("AGENT_STRACE_"):
                del os.environ[name]
        os.umask(0o077)
        raw = sys.stdin.buffer.read((32 << 20) + 1)
        if len(raw) > 32 << 20:
            raise ValueError("projection exceeds 32 MiB")
        result = publish(sys.argv[1], json.loads(raw))
        print(json.dumps(result))
    except Exception as exc:
        # Errors describe validation failures, never the input projection.
        print(f"export failed ({type(exc).__name__}): {exc}", file=sys.stderr)
        sys.exit(1)
