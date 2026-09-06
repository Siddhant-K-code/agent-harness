#!/bin/sh
# Build one platform archive. The explicit file list excludes keys/run artifacts.
set -eu
version=${1:?usage: package.sh VERSION OS ARCH [OUTPUT_DIRECTORY]}
target_os=${2:?target OS required}
target_arch=${3:?target architecture required}
output=${4:-dist}
case "$version" in v[0-9]*) ;; *) echo 'Version must start with v and a digit.' >&2; exit 2 ;; esac
case "$version" in *[!a-zA-Z0-9.+-]*) echo 'Invalid version characters.' >&2; exit 2 ;; esac
case "$target_os/$target_arch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'Unsupported target.' >&2; exit 2 ;; esac
cd "$(dirname "$0")/.."
mkdir -p "$output"
output=$(CDPATH= cd "$output" && pwd)
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM
source_commit=$(git rev-parse HEAD)
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags "-s -w -X main.version=$version -X main.commit=$source_commit" -o "$staging/harness" ./cmd/harness
cp scripts/install.sh "$staging/install.sh"
cp docs/getting-started.md "$staging/GETTING-STARTED.md"
cp docs/compaction-and-learning.md "$staging/COMPACTION-AND-LEARNING.md"
cp DISTRIBUTION.md "$staging/DISTRIBUTION.md"
cp go.mod "$staging/go.mod"
printf '%s/%s\n' "$target_os" "$target_arch" > "$staging/platform.txt"
printf '%s\n' "$source_commit" > "$staging/commit.txt"
# Include license notices for all Go dependencies actually linked into the CLI.
go run ./scripts/notices "$staging/THIRD-PARTY-NOTICES.txt"
(
  cd "$staging"
  if command -v sha256sum >/dev/null 2>&1; then sha256sum harness > harness.sha256
  else shasum -a 256 harness > harness.sha256; fi
  tar -czf "$output/agent-harness_${version}_${target_os}_${target_arch}.tar.gz" .
)
echo "$output/agent-harness_${version}_${target_os}_${target_arch}.tar.gz"
