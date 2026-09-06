#!/usr/bin/env python3
"""Install a project-signed release; Python 3.9+, OpenSSL, no pip dependencies."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

REPOSITORY = "Siddhant-K-code/agent-harness"
PLATFORMS = ("darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64")
# Trust anchor: changes require a new bootstrap from reviewed source.
PUBLIC_KEY = b"""-----BEGIN PUBLIC KEY-----
MIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEAtHyWhx5hTsUaIto0TZuA
Ik+AuR/+MEFagMnLz1HIyVrajrjroc9+J3xJfyJMLpE+HJM0TGQFqJsGv2YPDR3v
DHWz5YtfHG2WUdLllwEV9qwCh22Wa/fwZGdAek8DQDRTs4TWjgJvUgNxKazz0Qax
QHGkRQqpyK09BcofHORGLkfGshQpOYB3bnh/eEUmhkq2s/VXLhDcS2VaYvshFN9D
aKLbdQvsmWJIGcQGdSYKJj1hAnAtxDgKhJu5Zui+AIZ4IMiwc5G7JeLwgF3rf755
W+d7WujeqEhrS3DayR1bMzKqa3WWNUgMPhWfRQvhCyhherk0x304dCEqmBCAoR7k
CRslm5ArR4Qjq6Jb1XVUD6jfr8vGJfzS/mpBgIYhJi+fv9aKELv6t8Y2YZ5vzteX
83Aa/o04beLpamjsAc5smJTYO/qmsEaFO96lYvdpBkSMHk6qILwzbyImHqCHUvdU
AP92hoA4cKFVRUYwRhwSIm3zleE4MOWi05QoSgxxswDbAgMBAAE=
-----END PUBLIC KEY-----
"""


def valid_version(value):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", value):
        raise ValueError("Use an explicit version such as v0.1.0-rc.6")
    return value


def archive_name(version, target):
    valid_version(version)
    if target not in PLATFORMS:
        raise ValueError("Unsupported platform")
    return f"agent-harness_{version}_{target}.tar.gz"


def expected_files(version):
    return {archive_name(version, target) for target in PLATFORMS} | {
        "install-release.py", "release.json"
    }


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def verify_release(directory, version, target, *, public_key=PUBLIC_KEY, all_files=False):
    """Authenticate metadata before reading names or extracting executable content."""
    directory = Path(directory)
    expected = expected_files(version)
    archive = archive_name(version, target)
    manifest = directory / "checksums.txt"
    signature = directory / "checksums.txt.sig"
    if manifest.stat().st_size > 8192 or signature.stat().st_size > 1024:
        raise ValueError("Oversized release signature or manifest")
    with tempfile.TemporaryDirectory(prefix="harness-key-") as temporary:
        key_path = Path(temporary) / "public.pem"
        key_path.write_bytes(public_key)
        result = subprocess.run(
            ["openssl", "dgst", "-sha256", "-verify", str(key_path),
             "-signature", str(signature), str(manifest)], capture_output=True, timeout=30
        )
    if result.returncode:
        raise ValueError("Release signature verification failed; nothing was installed")
    entries = {}
    for line in manifest.read_text(encoding="ascii").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.-]+)", line)
        if not match or match[2] in entries:
            raise ValueError("Malformed or duplicate checksum entry")
        entries[match[2]] = match[1]
    if set(entries) != expected:
        raise ValueError("Manifest does not match the requested release")
    selected = expected if all_files else {archive, "release.json"}
    for name in selected:
        path = directory / name
        if path.is_symlink() or not path.is_file() or digest(path) != entries[name]:
            raise ValueError(f"Release checksum mismatch: {name}")
    metadata = json.loads((directory / "release.json").read_text())
    if (metadata.get("version") != version or metadata.get("repository") != REPOSITORY
            or not re.fullmatch(r"[0-9a-f]{40}", metadata.get("commit", ""))):
        raise ValueError("Signed release metadata does not match the requested release")
    return directory / archive, metadata


def extract_bundle(archive, directory):
    # Packages contain only these flat regular files. Never accept links or paths.
    expected = {"harness", "harness.sha256", "install.sh", "platform.txt", "commit.txt",
                "go.mod", "LICENSE", "THIRD-PARTY-NOTICES.txt", "GETTING-STARTED.md",
                "COMPACTION-AND-LEARNING.md", "UI-AND-INTEGRATIONS.md"}
    with tarfile.open(archive, "r:gz") as bundle:
        members = []
        seen = set()
        total = 0
        for member in bundle.getmembers():
            if member.name in (".", "./") and member.isdir():
                continue
            name = member.name[2:] if member.name.startswith("./") else member.name
            if name not in expected or name in seen or not member.isfile():
                raise ValueError("Unexpected archive entry; refusing extraction")
            seen.add(name)
            total += member.size
            if total > 512 * 1024 * 1024:
                raise ValueError("Release archive exceeds extraction limit")
            members.append((member, name))
        if seen != expected:
            raise ValueError("Incomplete release archive")
        directory.mkdir()
        for member, name in members:
            with bundle.extractfile(member) as source, (directory / name).open("xb") as dest:
                shutil.copyfileobj(source, dest)


def fetch_release(directory, version, target, public=False):
    names = ["checksums.txt", "checksums.txt.sig", "release.json", archive_name(version, target)]
    if public:
        for name in names:
            url = f"https://github.com/{REPOSITORY}/releases/download/{version}/{name}"
            with urllib.request.urlopen(url, timeout=120) as response, (directory / name).open("xb") as output:
                shutil.copyfileobj(response, output)
    else:
        if not shutil.which("gh"):
            raise ValueError("Install GitHub CLI and run gh auth login for repository access")
        command = ["gh", "release", "download", version, "--repo", REPOSITORY, "--dir", str(directory)]
        for name in names:
            command.extend(["--pattern", name])
        subprocess.run(command, check=True, timeout=600)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, type=valid_version)
    parser.add_argument("--bin-dir", type=Path, default=Path.home() / ".local/bin")
    parser.add_argument("--force", action="store_true", help="explicitly replace an installed binary")
    parser.add_argument("--public", action="store_true", help="anonymous HTTPS download, only for a public repository")
    parser.add_argument("--from-dir", type=Path, help="verify and install previously downloaded release files")
    parser.add_argument("--verify-only", action="store_true", help="verify without extraction or installation")
    parser.add_argument("--setup", type=Path, metavar="NEW_DIRECTORY", help="create the bundled demo project")
    parser.add_argument("--with-docker", action="store_true", help="check existing Docker daemon and pull the demo image")
    parser.add_argument("--with-trace", action="store_true", help="install pinned AgentTrace into the new project")
    parser.add_argument("--python", default=sys.executable, help="Python 3.12+ interpreter for AgentTrace")
    parser.add_argument("--login", action="store_true", help="prompt locally for an OpenAI key if none is configured")
    parser.add_argument("--serve", action="store_true", help="start the local web app after setup")
    args = parser.parse_args()
    if any((args.with_docker, args.with_trace, args.login, args.serve)) and not args.setup:
        parser.error("setup options require --setup NEW_DIRECTORY")
    if args.verify_only and args.setup:
        parser.error("--verify-only cannot run setup")
    machine = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    target = f"{platform.system().lower()}_{machine}"
    archive_name(args.version, target)
    if not shutil.which("openssl"):
        raise ValueError("OpenSSL is required to verify release signatures")
    task = args.setup.expanduser().absolute() if args.setup else None
    if task:
        if task.exists():
            raise ValueError("Setup directory already exists; choose a new directory")
        if not shutil.which("git"):
            raise ValueError("Git is required for project setup")
        if args.with_trace:
            subprocess.run([args.python, "-c", "import sys; sys.exit(sys.version_info < (3, 12))"], check=True)
        if args.with_docker:
            subprocess.run(["docker", "info"], check=True, stdout=subprocess.DEVNULL)
    with tempfile.TemporaryDirectory(prefix="harness-release-") as temporary:
        working = Path(temporary)
        downloads = working / "downloads"
        downloads.mkdir()
        if args.from_dir:
            # Copy to private staging so verification and extraction use the same bytes.
            for name in ("checksums.txt", "checksums.txt.sig", "release.json", archive_name(args.version, target)):
                source = args.from_dir / name
                if source.is_symlink() or not source.is_file():
                    raise ValueError(f"Missing regular release file: {name}")
                shutil.copyfile(source, downloads / name)
        else:
            fetch_release(downloads, args.version, target, public=args.public)
        archive, metadata = verify_release(downloads, args.version, target)
        print(f"Verified project signature: {args.version}, source {metadata['commit']}", flush=True)
        if args.verify_only:
            return
        bundle = working / "bundle"
        extract_bundle(archive, bundle)
        if (bundle / "commit.txt").read_text().strip() != metadata["commit"]:
            raise ValueError("Archive source commit does not match signed metadata")
        binary_dir = args.bin_dir.expanduser().absolute()
        command = ["sh", str(bundle / "install.sh"), "--bin-dir", str(binary_dir)]
        if args.force:
            command.append("--force")
        subprocess.run(command, check=True)
    harness = str(binary_dir / "harness")
    subprocess.run([harness, "version"], check=True)
    if not task:
        return
    subprocess.run([harness, "init", str(task)], check=True)
    if args.with_docker:
        subprocess.run(["docker", "pull", "node:22-alpine"], check=True)
    if args.with_trace:
        subprocess.run([harness, "trace", "setup", "--python", args.python], cwd=task, check=True)
    if args.login:
        status = subprocess.run([harness, "auth", "status"], cwd=task, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if status.returncode:
            subprocess.run([harness, "auth", "login"], cwd=task, check=True)
    if args.with_docker:
        subprocess.run([harness, "doctor"], cwd=task, check=True)
    print(f"Setup finished in {task}. No paid model requests were submitted.", flush=True)
    if args.serve:
        subprocess.run([harness, "serve", "--task", "harness.task.json"], cwd=task, check=True)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(130)
    except (ValueError, OSError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(f"Install failed: {error}", file=sys.stderr)
        sys.exit(1)
