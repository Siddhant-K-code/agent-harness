// Collect upstream notices for linked module dependencies in a binary archive.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: notices OUTPUT_FILE")
	}
	b, err := exec.Command("go", "list", "-deps", "-json", "./cmd/harness").Output()
	if err != nil {
		panic(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	modules := map[string]string{}
	for d.More() {
		var p struct {
			Module *struct {
				Path, Version, Dir string
				Main               bool
			}
		}
		if err := d.Decode(&p); err != nil {
			panic(err)
		}
		if m := p.Module; m != nil && !m.Main {
			modules[m.Path+"@"+m.Version] = m.Dir
		}
	}
	keys := make([]string, 0, len(modules))
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("Third-party notices for dependencies linked into harness.\nAgent-harness is licensed under MIT; see LICENSE. Dependencies retain their own licenses and notices below.\n\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "=== %s ===\n", key)
		entries, err := os.ReadDir(modules[key])
		if err != nil {
			panic(err)
		}
		found := false
		for _, entry := range entries {
			upper := strings.ToUpper(entry.Name())
			if !entry.IsDir() && (strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE") || upper == "AUTHORS") {
				b, err := os.ReadFile(filepath.Join(modules[key], entry.Name()))
				if err != nil {
					panic(err)
				}
				fmt.Fprintf(&out, "--- %s ---\n%s\n\n", entry.Name(), b)
				if strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") {
					found = true
				}
			}
		}
		if !found {
			panic("No license notice found for linked dependency " + key)
		}
	}
	if err := os.WriteFile(os.Args[1], []byte(out.String()), 0644); err != nil {
		panic(err)
	}
}
