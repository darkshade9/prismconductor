package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"prismconductor/internal/types"
)

func writeRegistry(t *testing.T, dir string, items []types.Workspace) string {
	t.Helper()
	path := filepath.Join(dir, "workspaces.json")
	b, _ := json.MarshalIndent(items, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("writeRegistry: %v", err)
	}
	return path
}

func TestReconcileProvisioning_removesStaleRows(t *testing.T) {
	dir := t.TempDir()
	staleAt := time.Now().Add(-20 * time.Minute)
	recentAt := time.Now().Add(-5 * time.Minute)
	items := []types.Workspace{
		{ID: "stale1", Provisioning: true, ProvisioningAt: &staleAt},
		{ID: "stale2", Provisioning: true, ProvisioningAt: nil}, // nil treated as stale
		{ID: "recent", Provisioning: true, ProvisioningAt: &recentAt},
		{ID: "good", Provisioning: false},
	}
	writeRegistry(t, dir, items)

	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	removed := r.ReconcileProvisioning()
	if len(removed) != 2 {
		t.Errorf("removed %d workspaces, want 2; got %v", len(removed), removed)
	}
	for _, id := range removed {
		if id != "stale1" && id != "stale2" {
			t.Errorf("unexpected removed ID: %s", id)
		}
	}

	// Verify the surviving rows.
	list := r.List()
	if len(list) != 2 {
		t.Errorf("len(list) = %d, want 2", len(list))
	}
	ids := map[string]bool{}
	for _, w := range list {
		ids[w.ID] = true
	}
	if !ids["recent"] {
		t.Error("recent provisioning row should survive (not yet stale)")
	}
	if !ids["good"] {
		t.Error("non-provisioning row should survive")
	}
}

func TestReconcileProvisioning_noOp(t *testing.T) {
	dir := t.TempDir()
	items := []types.Workspace{
		{ID: "ws1", Provisioning: false},
		{ID: "ws2"},
	}
	writeRegistry(t, dir, items)

	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	removed := r.ReconcileProvisioning()
	if len(removed) != 0 {
		t.Errorf("expected no removals, got %v", removed)
	}
	if len(r.List()) != 2 {
		t.Errorf("list length changed unexpectedly")
	}
}

func TestReconcileProvisioning_persists(t *testing.T) {
	dir := t.TempDir()
	staleAt := time.Now().Add(-20 * time.Minute)
	items := []types.Workspace{
		{ID: "zombie", Provisioning: true, ProvisioningAt: &staleAt},
		{ID: "live"},
	}
	writeRegistry(t, dir, items)

	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.ReconcileProvisioning()

	// Re-open the registry from disk to confirm the save happened.
	r2, err := New(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	list := r2.List()
	if len(list) != 1 || list[0].ID != "live" {
		t.Errorf("persisted list = %v, want [{ID:live}]", list)
	}
}
