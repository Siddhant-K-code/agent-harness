// Package normalizetags bundles the real first-run task in release binaries.
package normalizetags

import "embed"

//go:embed tags.js verify.sh task.json
var Files embed.FS
