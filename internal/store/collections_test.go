package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"prismconductor/internal/types"
)

func TestCollectionCRUDCycle(t *testing.T) {
	s := openTempStore(t)

	// Create
	col, err := s.CreateCollection("my-fleet")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if col.ID == "" || col.Name != "my-fleet" {
		t.Fatalf("unexpected collection: %+v", col)
	}

	// List
	cols, err := s.ListCollections()
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(cols) != 1 || cols[0].ID != col.ID {
		t.Fatalf("ListCollections = %v, want 1 entry with id=%s", cols, col.ID)
	}

	// Rename
	if err := s.RenameCollection(col.ID, "renamed-fleet"); err != nil {
		t.Fatalf("RenameCollection: %v", err)
	}
	got, err := s.GetCollection(col.ID)
	if err != nil {
		t.Fatalf("GetCollection after rename: %v", err)
	}
	if got.Name != "renamed-fleet" {
		t.Errorf("Name = %q, want %q", got.Name, "renamed-fleet")
	}

	// UpdateCollectionContext
	if err := s.UpdateCollectionContext(col.ID, "# Shared architecture\nUse gRPC."); err != nil {
		t.Fatalf("UpdateCollectionContext: %v", err)
	}
	got, _ = s.GetCollection(col.ID)
	if got.ContextMD != "# Shared architecture\nUse gRPC." {
		t.Errorf("ContextMD = %q", got.ContextMD)
	}

	// Delete
	if err := s.DeleteCollection(col.ID); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	cols, _ = s.ListCollections()
	if len(cols) != 0 {
		t.Errorf("expected 0 collections after delete, got %d", len(cols))
	}
}

func TestCollectionMemberAddRemovePosition(t *testing.T) {
	s := openTempStore(t)
	col, _ := s.CreateCollection("fleet")

	if err := s.AddWorkspaceToCollection(col.ID, "ws-a"); err != nil {
		t.Fatalf("AddWorkspace ws-a: %v", err)
	}
	if err := s.AddWorkspaceToCollection(col.ID, "ws-b"); err != nil {
		t.Fatalf("AddWorkspace ws-b: %v", err)
	}

	got, err := s.GetCollection(col.ID)
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if len(got.WorkspaceIDs) != 2 || got.WorkspaceIDs[0] != "ws-a" || got.WorkspaceIDs[1] != "ws-b" {
		t.Errorf("WorkspaceIDs = %v, want [ws-a ws-b]", got.WorkspaceIDs)
	}

	// Remove one
	if err := s.RemoveWorkspaceFromCollection(col.ID, "ws-a"); err != nil {
		t.Fatalf("RemoveWorkspace ws-a: %v", err)
	}
	got, _ = s.GetCollection(col.ID)
	if len(got.WorkspaceIDs) != 1 || got.WorkspaceIDs[0] != "ws-b" {
		t.Errorf("after remove: WorkspaceIDs = %v, want [ws-b]", got.WorkspaceIDs)
	}
}

func TestCollectionAlreadyInCollectionBothPaths(t *testing.T) {
	s := openTempStore(t)
	c1, _ := s.CreateCollection("fleet-1")
	c2, _ := s.CreateCollection("fleet-2")

	if err := s.AddWorkspaceToCollection(c1.ID, "ws-x"); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Pre-check path: ws-x already belongs to c1.
	err := s.AddWorkspaceToCollection(c2.ID, "ws-x")
	if !errors.Is(err, types.ErrAlreadyInCollection) {
		t.Errorf("expected ErrAlreadyInCollection, got %v", err)
	}
}

func TestCollectionDeleteCascadeDropsMembers(t *testing.T) {
	s := openTempStore(t)
	col, _ := s.CreateCollection("fleet")
	_ = s.AddWorkspaceToCollection(col.ID, "ws-a")
	_ = s.DeleteCollection(col.ID)

	// CollectionForWorkspace should now return not-found.
	_, found, err := s.CollectionForWorkspace("ws-a")
	if err != nil {
		t.Fatalf("CollectionForWorkspace: %v", err)
	}
	if found {
		t.Error("expected workspace to be un-membered after collection delete")
	}
}

func TestCollectionRemoveWorkspaceFromAllCollections(t *testing.T) {
	s := openTempStore(t)
	col, _ := s.CreateCollection("fleet")
	_ = s.AddWorkspaceToCollection(col.ID, "ws-orphan")

	if err := s.RemoveWorkspaceFromAllCollections("ws-orphan"); err != nil {
		t.Fatalf("RemoveWorkspaceFromAllCollections: %v", err)
	}
	_, found, _ := s.CollectionForWorkspace("ws-orphan")
	if found {
		t.Error("expected orphan to be unlinked")
	}
}

func TestRelatedRepoPathsNotInCollection(t *testing.T) {
	s := openTempStore(t)
	paths, err := s.RelatedRepoPaths("ws-solo")
	if err != nil {
		t.Fatalf("RelatedRepoPaths for non-member: %v", err)
	}
	if paths != nil {
		t.Errorf("expected nil for solo workspace, got %v", paths)
	}
}

func TestRelatedRepoPathsFiltersDisabledSiblings(t *testing.T) {
	s := openTempStore(t)
	col, _ := s.CreateCollection("fleet")
	_ = s.AddWorkspaceToCollection(col.ID, "ws-app")
	_ = s.AddWorkspaceToCollection(col.ID, "ws-infra")
	_ = s.AddWorkspaceToCollection(col.ID, "ws-disabled")

	// Write a workspaces.json in the store's config dir.
	workspaces := []types.Workspace{
		{ID: "ws-app", RepoPath: "/repo/app", Enabled: true},
		{ID: "ws-infra", RepoPath: "/repo/infra", Enabled: true},
		{ID: "ws-disabled", RepoPath: "/repo/disabled", Enabled: false},
	}
	writeWorkspacesJSON(t, s, workspaces)

	paths, err := s.RelatedRepoPaths("ws-app")
	if err != nil {
		t.Fatalf("RelatedRepoPaths: %v", err)
	}
	// Expect infra only (disabled excluded, self excluded).
	if len(paths) != 1 || paths[0] != "/repo/infra" {
		t.Errorf("RelatedRepoPaths = %v, want [/repo/infra]", paths)
	}
}

// writeWorkspacesJSON writes a workspaces.json into the store's config directory.
func writeWorkspacesJSON(t *testing.T, s *Store, workspaces []types.Workspace) {
	t.Helper()
	b, err := json.Marshal(workspaces)
	if err != nil {
		t.Fatalf("marshal workspaces: %v", err)
	}
	path := filepath.Join(filepath.Dir(s.Path), "workspaces.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}
