// Package learningdemo bundles real coding tasks for exercising the learning loop.
package learningdemo

import "embed"

//go:embed utilities.js words.verify.sh sum.verify.sh clamp.verify.sh SKILL.md
var Files embed.FS
