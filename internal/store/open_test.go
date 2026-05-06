package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpen_PlacesDBInDir verifies that Open creates conductor.db inside the
// given directory — the contract that PRISMCONDUCTOR_DATA_DIR relies on.
func TestOpen_PlacesDBInDir(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.DB.Close() })

	want := filepath.Join(dir, "conductor.db")
	if st.Path != want {
		t.Errorf("Store.Path = %q, want %q", st.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("conductor.db not found at %q: %v", want, err)
	}
}
