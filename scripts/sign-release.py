#!/usr/bin/env python3
"""Sign an exact set of tested release archives with an explicitly provided key file."""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("installer", ROOT / "scripts/install-release.py")
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", type=installer.valid_version)
    parser.add_argument("commit")
    parser.add_argument("--directory", type=Path, default=Path("dist"))
    parser.add_argument("--key-file", required=True, type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit):
        parser.error("commit must be a full Git SHA")
    directory = args.directory
    archives = {installer.archive_name(args.version, target) for target in installer.PLATFORMS}
    if {path.name for path in directory.iterdir()} != archives:
        parser.error("release directory must contain exactly the four tested platform archives")
    for name in archives:
        path = directory / name
        if path.is_symlink() or not path.is_file():
            parser.error("archives must be regular files")
    public = (ROOT / "security/release-signing.pub.pem").read_bytes()
    if public != installer.PUBLIC_KEY:
        parser.error("installer trust anchor differs from the committed public key")
    shutil.copyfile(ROOT / "scripts/install-release.py", directory / "install-release.py")
    shutil.copyfile(ROOT / "security/release-signing.pub.pem", directory / "release-signing.pub.pem")
    metadata = {"version": args.version, "commit": args.commit, "repository": installer.REPOSITORY,
                "signature": "RSA-3072/SHA-256 project signature; not Apple notarization"}
    (directory / "release.json").write_text(json.dumps(metadata, indent=2) + "\n")
    manifest = directory / "checksums.txt"
    manifest.write_text("".join(f"{installer.digest(directory / name)}  {name}\n"
                                for name in sorted(installer.expected_files(args.version))))
    subprocess.run(["openssl", "dgst", "-sha256", "-sign", str(args.key_file), "-out",
                    str(directory / "checksums.txt.sig"), str(manifest)], check=True)
    installer.verify_release(directory, args.version, "linux_amd64", all_files=True)
    print(f"Signed and verified all artifacts for {args.version}")


if __name__ == "__main__":
    main()
