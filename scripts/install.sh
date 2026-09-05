#!/bin/sh
# Install only from an already downloaded/extracted, checksummed release bundle.
set -eu
bin_dir="$HOME/.local/bin"
force=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bin-dir) [ "$#" -ge 2 ] || exit 2; bin_dir=$2; shift 2 ;;
    --force) force=true; shift ;;
    --help|-h) echo 'Usage: sh install.sh [--bin-dir PATH] [--force]'; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done
bundle=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$bundle/harness" ] && [ -f "$bundle/harness.sha256" ] || {
  echo 'Run the install.sh inside an extracted release archive. See docs/getting-started.md.' >&2
  exit 1
}
case "$(uname -s)" in Darwin) platform=darwin ;; Linux) platform=linux ;; *) echo 'Only macOS/Linux are supported.' >&2; exit 1 ;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) echo 'Unsupported CPU architecture.' >&2; exit 1 ;; esac
[ "$(cat "$bundle/platform.txt")" = "$platform/$arch" ] || { echo 'This archive targets a different platform; download the matching archive.' >&2; exit 1; }
(
  cd "$bundle"
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c harness.sha256
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 -c harness.sha256
  else echo 'Install sha256sum or shasum before continuing.' >&2; exit 1; fi
)
mkdir -p "$bin_dir"
destination="$bin_dir/harness"
if [ -e "$destination" ] || [ -L "$destination" ]; then
  [ "$force" = true ] || { echo "Already installed at $destination; use --force to upgrade." >&2; exit 1; }
  [ ! -d "$destination" ] || { echo 'Destination is a directory.' >&2; exit 1; }
fi
staged=$(mktemp "$bin_dir/.harness-install.XXXXXX")
trap 'rm -f "$staged"' EXIT HUP INT TERM
cp "$bundle/harness" "$staged"
chmod 755 "$staged"
if [ "$force" = true ]; then mv -f "$staged" "$destination"; else ln "$staged" "$destination"; fi
echo "Installed $destination"
case ":$PATH:" in *":$bin_dir:"*) ;; *) echo "Add $bin_dir to PATH in your shell configuration." ;; esac
echo 'Next: harness init'
