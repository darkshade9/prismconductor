package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// createBackup copies src to <dir>/conductor.db.pre-<ts>.bak and rotates
// old backup files so that at most keep copies are retained.
// src must be an existing regular file; if not, this is a no-op.
func createBackup(src string, keep int) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	dir := filepath.Dir(src)
	ts := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, fmt.Sprintf("conductor.db.pre-%s.bak", ts))

	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("backup %s → %s: %w", src, dst, err)
	}

	return rotateBackups(dir, keep)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.CreateTemp(filepath.Dir(dst), ".backup-*")
	if err != nil {
		return err
	}
	tmp := out.Name()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// rotateBackups removes the oldest conductor.db.pre-*.bak files beyond keep.
func rotateBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "conductor.db.pre-") && strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(backups) // lexicographic = chronological for our timestamp format
	for len(backups) > keep {
		if err := os.Remove(backups[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		backups = backups[1:]
	}
	return nil
}
