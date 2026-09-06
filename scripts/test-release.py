#!/usr/bin/env python3
"""Real signature/tampering tests; ephemeral test keys never authorize a release."""
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("installer", ROOT / "scripts/install-release.py")
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)
NATIVE_ARCHIVE = None
if len(sys.argv) == 3 and sys.argv[1] == "--archive":
    NATIVE_ARCHIVE = Path(sys.argv[2]).resolve()
    sys.argv = sys.argv[:1]


class SignedReleaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.keys = tempfile.TemporaryDirectory()
        cls.private = Path(cls.keys.name) / "test-private.pem"
        subprocess.run(["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072",
                        "-out", str(cls.private)], check=True, capture_output=True)
        cls.public = subprocess.check_output(["openssl", "pkey", "-in", str(cls.private), "-pubout"])

    @classmethod
    def tearDownClass(cls):
        cls.keys.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.version = NATIVE_ARCHIVE.name.split("_")[1] if NATIVE_ARCHIVE else "v0.1.0-rc.6"
        self.commit = "a" * 40
        for name in installer.expected_files(self.version):
            (self.root / name).write_bytes(b"non-executable archive fixture")
        (self.root / "release.json").write_text(json.dumps({"version": self.version, "commit": self.commit,
                                                           "repository": installer.REPOSITORY}))
        self.manifest()

    def manifest(self):
        (self.root / "checksums.txt").write_text("".join(
            f"{installer.digest(self.root / name)}  {name}\n"
            for name in sorted(installer.expected_files(self.version))))
        self.sign()

    def sign(self):
        subprocess.run(["openssl", "dgst", "-sha256", "-sign", str(self.private), "-out",
                        str(self.root / "checksums.txt.sig"), str(self.root / "checksums.txt")], check=True)

    def verify(self, **kwargs):
        return installer.verify_release(self.root, self.version, "linux_amd64", public_key=self.public, **kwargs)

    def test_authentic_manifest(self):
        _, metadata = self.verify(all_files=True)
        self.assertEqual(metadata["commit"], self.commit)

    def test_production_trust_anchor_matches_source(self):
        self.assertEqual(installer.PUBLIC_KEY, (ROOT / "security/release-signing.pub.pem").read_bytes())

    def test_wrong_key_rejected(self):
        with self.assertRaisesRegex(ValueError, "signature"):
            installer.verify_release(self.root, self.version, "linux_amd64")

    def test_tampered_manifest_rejected(self):
        with (self.root / "checksums.txt").open("ab") as stream:
            stream.write(b"\n")
        with self.assertRaisesRegex(ValueError, "signature"):
            self.verify()

    def test_tampered_archive_rejected(self):
        (self.root / installer.archive_name(self.version, "linux_amd64")).write_bytes(b"tampered")
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.verify()

    def test_missing_archive_rejected(self):
        (self.root / installer.archive_name(self.version, "linux_amd64")).unlink()
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.verify()

    def test_wrong_requested_version_rejected(self):
        with self.assertRaisesRegex(ValueError, "requested release"):
            installer.verify_release(self.root, "v0.1.0-rc.7", "linux_amd64", public_key=self.public)

    def test_signed_wrong_metadata_rejected(self):
        (self.root / "release.json").write_text(json.dumps({"version": "v0.1.0-rc.1",
                                                           "commit": self.commit, "repository": installer.REPOSITORY}))
        self.manifest()
        with self.assertRaisesRegex(ValueError, "metadata"):
            self.verify()

    def test_duplicate_and_unsafe_signed_entries_rejected(self):
        manifest = self.root / "checksums.txt"
        original = manifest.read_text()
        for extra in (original.splitlines()[0] + "\n", "0" * 64 + "  ../escape\n"):
            with self.subTest(extra=extra):
                manifest.write_text(original + extra)
                self.sign()
                with self.assertRaisesRegex(ValueError, "Malformed|duplicate"):
                    self.verify()

    def test_unsafe_archives_rejected_before_any_extraction(self):
        for name, kind in (("../escape", tarfile.REGTYPE), ("harness", tarfile.SYMTYPE),
                           ("harness", tarfile.LNKTYPE)):
            with self.subTest(name=name, kind=kind):
                archive = self.root / "unsafe.tar.gz"
                with tarfile.open(archive, "w:gz") as output:
                    member = tarfile.TarInfo(name)
                    member.type = kind
                    member.linkname = "/tmp/escape" if kind != tarfile.REGTYPE else ""
                    output.addfile(member, io.BytesIO(b""))
                destination = self.root / "extracted"
                with self.assertRaisesRegex(ValueError, "Unexpected"):
                    installer.extract_bundle(archive, destination)
                self.assertFalse(destination.exists())

    @unittest.skipUnless(NATIVE_ARCHIVE, "pass --archive for a real native installation")
    def test_signer_and_native_bootstrap(self):
        # Exercise the production scripts in a disposable copy with a TEST trust root.
        source = self.root / "source"
        (source / "scripts").mkdir(parents=True)
        (source / "security").mkdir()
        (source / "security/release-signing.pub.pem").write_bytes(self.public)
        script = (ROOT / "scripts/install-release.py").read_bytes().replace(installer.PUBLIC_KEY, self.public)
        (source / "scripts/install-release.py").write_bytes(script)
        shutil.copyfile(ROOT / "scripts/sign-release.py", source / "scripts/sign-release.py")
        release = self.root / "release"
        release.mkdir()
        with tarfile.open(NATIVE_ARCHIVE) as archive:
            self.commit = archive.extractfile("./commit.txt").read().decode().strip()
        for target in installer.PLATFORMS:
            # Only the host's archive is installed; other entries exercise manifest completeness.
            shutil.copyfile(NATIVE_ARCHIVE, release / installer.archive_name(self.version, target))
        subprocess.run([sys.executable, str(source / "scripts/sign-release.py"), self.version,
                        self.commit, "--directory", str(release), "--key-file", str(self.private)], check=True)
        binary_dir = self.root / "installed bin"
        command = [sys.executable, str(source / "scripts/install-release.py"), "--version", self.version,
                   "--from-dir", str(release), "--bin-dir", str(binary_dir)]
        subprocess.run(command + ["--verify-only"], check=True)
        self.assertFalse(binary_dir.exists())
        subprocess.run(command + ["--setup", str(self.root / "demo project")], check=True)
        self.assertTrue((self.root / "demo project/harness.task.json").is_file())
        before = installer.digest(binary_dir / "harness")
        self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)
        subprocess.run(command + ["--force"], check=True)
        # An authenticated but changed archive must not replace a working installation.
        for archive in release.glob("*.tar.gz"):
            archive.write_bytes(b"tampered")
        self.assertNotEqual(subprocess.run(command + ["--force"], capture_output=True).returncode, 0)
        self.assertEqual(installer.digest(binary_dir / "harness"), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
