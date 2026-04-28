// Package bundle ships the four universal skills with the binary (PRISMCONDUCTOR_PLAN.md §15.7).
// Phase 4.5: extracts to ~/.prismconductor/skills/ on first run.
package bundle

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed skills/*.md
var FS embed.FS

// Extract copies bundled skills to dst if they don't already exist.
// Returns the destination dir.
func Extract(dst string) (string, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	entries, err := fs.ReadDir(FS, "skills")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		out := filepath.Join(dst, e.Name())
		if _, err := os.Stat(out); err == nil {
			continue // user may have edited; never overwrite
		}
		b, err := FS.ReadFile("skills/" + e.Name())
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return "", err
		}
	}
	return dst, nil
}
