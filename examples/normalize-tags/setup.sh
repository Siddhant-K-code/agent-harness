#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
repo=.harness/example-repo
if [ -e "$repo" ]; then
  echo "Example repository already exists: $repo" >&2
  exit 1
fi
mkdir -p "$repo"
cp examples/normalize-tags/tags.js "$repo/"
git -c core.hooksPath=/dev/null init "$repo"
git -C "$repo" -c core.hooksPath=/dev/null add tags.js
git -C "$repo" -c core.hooksPath=/dev/null -c user.name=Harness -c user.email=harness@example.invalid commit -m 'Add tag normalization bug fixture'
