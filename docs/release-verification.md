# Verify release artifacts

Signed releases include four native archives, `release.json`, `install-release.py`, `checksums.txt`, `checksums.txt.sig`, and `release-signing.pub.pem`. The RSA-3072/SHA-256 signature authenticates the checksum manifest, which binds every archive, the installer, the version, and the source commit.

The bootstrap in [scripts/install-release.py](../scripts/install-release.py) embeds the project's public key. Obtain it from the immutable source commit in the [installation instructions](getting-started.md#install-a-binary). This initial download trusts GitHub HTTPS and the reviewed source commit. The bootstrap then verifies the signature, selected archive hash, requested version, repository and source metadata **before extraction**. There is no unsigned fallback or runtime flag to replace the trust root. Use `--verify-only` to stop before extraction and installation.

The public key's SHA-256 fingerprint (DER encoding) is:

```text
59af5840cf9d91db384c4cd673f0ffc4f45bb250029f270e98f12882e37bd5c9
```

For manual verification, obtain `security/release-signing.pub.pem` from that same trusted source commit. Do not accept a replacement key solely because it appears beside the downloaded archive. In the download directory:

```sh
openssl pkey -pubin -in /path/to/trusted/release-signing.pub.pem -outform DER | openssl dgst -sha256
openssl dgst -sha256 -verify /path/to/trusted/release-signing.pub.pem \
  -signature checksums.txt.sig checksums.txt
```

Only after `Verified OK`, check the exact requested archive and `release.json` against the authenticated manifest. The bootstrap performs these checks and rejects missing or duplicate entries, mismatched versions, and unexpected archive paths or links. Its `--from-dir PATH` option supports previously downloaded release files. [OpenSSL signing and verification reference](https://docs.openssl.org/3.6/man1/openssl-dgst/).

These are project signatures. They establish that the artifacts were authorized by the holder of this project key; they are not an independent security audit, reproducible-build proof, Apple Developer ID signature, or Apple notarization. macOS platform approval can still be required. Releases do not change repository visibility or grant repository access.

## Maintainer workflow

The `Package and signed release` workflow tests each native archive on its matching OS and architecture. It also uses ephemeral test keys to verify the real installer and corruption rejection; test keys never authorize a production release. Actions are pinned to commit SHAs, checkout credentials are not persisted, and PR/push package jobs receive no release-signing key.

The manually requested `main` release job needs the encrypted repository secret `HARNESS_RELEASE_SIGNING_KEY`, holding the private PEM corresponding to the committed public key. Only the signing step references it, through a temporary owner-only file removed at step exit. Repository administrators and trusted workflow changes can access repository secrets; protect those permissions accordingly. The public key belongs in source control; the private key does not.

After checking the intended commit's CI results, trigger a signed prerelease:

```sh
gh workflow run package.yml --repo Siddhant-K-code/agent-harness --ref main \
  -f version=v0.1.0-rc.6 -f draft_release=true -f publish_release=true
```

The workflow signs exactly the four tested archives plus metadata and installer, creates a draft, downloads all uploaded assets, and verifies them against the committed key before publication. Use `publish_release=false` to retain the verified draft. Existing release versions are not overwritten; choose a new version. If an upload or verification fails, publication stops and any created draft stays available for investigation.

Normal test/package CI needs no signing, OpenAI, or AWS secrets. Signing and setup make no paid model requests. Signing-key rotation requires updating the public key and embedded bootstrap together, distributing a newly trusted bootstrap, and replacing the encrypted secret. Older bootstraps intentionally reject a newly signed key; keep a protected backup if old-key releases must remain maintainable.
