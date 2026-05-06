package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"prismconductor/internal/types"
)

// CreateCollection inserts a new collection row and returns it.
func (s *Store) CreateCollection(name string) (types.Collection, error) {
	if s == nil || s.DB == nil {
		return types.Collection{}, errors.New("store unavailable")
	}
	if strings.TrimSpace(name) == "" {
		return types.Collection{}, errors.New("collection name required")
	}
	now := time.Now()
	c := types.Collection{
		ID:        uuid.New().String(),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.DB.Exec(
		`INSERT INTO collections (id, name, context_md, created_at, updated_at) VALUES (?, ?, '', ?, ?)`,
		c.ID, c.Name, c.CreatedAt.Unix(), c.UpdatedAt.Unix())
	if err != nil {
		return types.Collection{}, err
	}
	return c, nil
}

// ListCollections returns every collection with member workspace IDs ordered by position.
func (s *Store) ListCollections() ([]types.Collection, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(
		`SELECT id, name, context_md, created_at, updated_at FROM collections ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ids, err := s.memberIDs(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].WorkspaceIDs = ids
	}
	return out, nil
}

// GetCollection returns one collection by ID, with member workspace IDs.
func (s *Store) GetCollection(id string) (types.Collection, error) {
	if s == nil || s.DB == nil {
		return types.Collection{}, errors.New("store unavailable")
	}
	row := s.DB.QueryRow(
		`SELECT id, name, context_md, created_at, updated_at FROM collections WHERE id = ?`, id)
	c, err := scanCollection(row)
	if err != nil {
		return types.Collection{}, err
	}
	ids, err := s.memberIDs(c.ID)
	if err != nil {
		return types.Collection{}, err
	}
	c.WorkspaceIDs = ids
	return c, nil
}

// RenameCollection updates the collection's display name.
func (s *Store) RenameCollection(id, name string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("collection name required")
	}
	res, err := s.DB.Exec(
		`UPDATE collections SET name = ?, updated_at = ? WHERE id = ?`,
		name, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("collection %q not found", id)
	}
	return nil
}

// DeleteCollection removes the collection row; ON DELETE CASCADE drops members.
func (s *Store) DeleteCollection(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM collections WHERE id = ?`, id)
	return err
}

// AddWorkspaceToCollection adds a workspace to a collection. Returns
// types.ErrAlreadyInCollection if the workspace is already in any collection
// (v1 single-membership rule). The UNIQUE INDEX on workspace_id backstops
// concurrent calls that both pass the pre-check.
func (s *Store) AddWorkspaceToCollection(collectionID, workspaceID string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	// App-layer pre-check — fast path for the common (non-racy) case.
	var existing string
	err := s.DB.QueryRow(
		`SELECT collection_id FROM collection_members WHERE workspace_id = ?`, workspaceID).Scan(&existing)
	if err == nil {
		return types.ErrAlreadyInCollection
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Determine next position within this collection.
	var pos int
	_ = s.DB.QueryRow(
		`SELECT COALESCE(MAX(position), -1) + 1 FROM collection_members WHERE collection_id = ?`,
		collectionID).Scan(&pos)

	_, err = s.DB.Exec(
		`INSERT INTO collection_members (collection_id, workspace_id, position) VALUES (?, ?, ?)`,
		collectionID, workspaceID, pos)
	if err != nil {
		// UNIQUE INDEX violation — concurrent insert won the race.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return types.ErrAlreadyInCollection
		}
		return err
	}
	_, _ = s.DB.Exec(
		`UPDATE collections SET updated_at = ? WHERE id = ?`, time.Now().Unix(), collectionID)
	return nil
}

// RemoveWorkspaceFromCollection removes one workspace from the given collection.
func (s *Store) RemoveWorkspaceFromCollection(collectionID, workspaceID string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(
		`DELETE FROM collection_members WHERE collection_id = ? AND workspace_id = ?`,
		collectionID, workspaceID)
	if err != nil {
		return err
	}
	_, _ = s.DB.Exec(
		`UPDATE collections SET updated_at = ? WHERE id = ?`, time.Now().Unix(), collectionID)
	return nil
}

// RemoveWorkspaceFromAllCollections removes a workspace from every collection it
// belongs to. Called by DeleteWorkspace before the workspace row is removed so
// collection_members doesn't retain stale workspace_id rows.
func (s *Store) RemoveWorkspaceFromAllCollections(workspaceID string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(
		`DELETE FROM collection_members WHERE workspace_id = ?`, workspaceID)
	return err
}

// UpdateCollectionContext replaces the shared context markdown body and bumps updated_at.
func (s *Store) UpdateCollectionContext(collectionID, body string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	res, err := s.DB.Exec(
		`UPDATE collections SET context_md = ?, updated_at = ? WHERE id = ?`,
		body, time.Now().Unix(), collectionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("collection %q not found", collectionID)
	}
	return nil
}

// CollectionForWorkspace returns the collection the workspace belongs to, if any.
func (s *Store) CollectionForWorkspace(workspaceID string) (types.Collection, bool, error) {
	if s == nil || s.DB == nil {
		return types.Collection{}, false, errors.New("store unavailable")
	}
	var collID string
	err := s.DB.QueryRow(
		`SELECT collection_id FROM collection_members WHERE workspace_id = ?`, workspaceID).Scan(&collID)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Collection{}, false, nil
	}
	if err != nil {
		return types.Collection{}, false, err
	}
	c, err := s.GetCollection(collID)
	if err != nil {
		return types.Collection{}, false, err
	}
	return c, true, nil
}

// RelatedRepoPaths returns the RepoPath values of the other enabled members of the
// collection the workspace belongs to. Returns nil (no error) when the workspace is
// not in any collection.
//
// Workspace metadata (RepoPath, Enabled) is read from workspaces.json in the same
// config directory as conductor.db.
func (s *Store) RelatedRepoPaths(workspaceID string) ([]string, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	col, found, err := s.CollectionForWorkspace(workspaceID)
	if err != nil || !found {
		return nil, err
	}
	wsMap, err := s.loadWorkspaceMap()
	if err != nil {
		return nil, fmt.Errorf("RelatedRepoPaths: %w", err)
	}
	var paths []string
	for _, sibID := range col.WorkspaceIDs {
		if sibID == workspaceID {
			continue
		}
		ws, ok := wsMap[sibID]
		if !ok || !ws.Enabled {
			continue
		}
		paths = append(paths, ws.RepoPath)
	}
	return paths, nil
}

// loadWorkspaceMap reads workspaces.json from the same config directory as
// conductor.db and returns a map keyed by workspace ID.
func (s *Store) loadWorkspaceMap() (map[string]types.Workspace, error) {
	wsPath := filepath.Join(filepath.Dir(s.Path), "workspaces.json")
	b, err := os.ReadFile(wsPath)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]types.Workspace{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workspaces.json: %w", err)
	}
	var list []types.Workspace
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parse workspaces.json: %w", err)
	}
	m := make(map[string]types.Workspace, len(list))
	for _, ws := range list {
		m[ws.ID] = ws
	}
	return m, nil
}

// --- helpers ---

type collectionScanner interface {
	Scan(dest ...any) error
}

func scanCollection(r collectionScanner) (types.Collection, error) {
	var c types.Collection
	var createdAt, updatedAt int64
	if err := r.Scan(&c.ID, &c.Name, &c.ContextMD, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.Collection{}, fmt.Errorf("collection not found")
		}
		return types.Collection{}, err
	}
	c.CreatedAt = time.Unix(createdAt, 0)
	c.UpdatedAt = time.Unix(updatedAt, 0)
	return c, nil
}

func (s *Store) memberIDs(collectionID string) ([]string, error) {
	rows, err := s.DB.Query(
		`SELECT workspace_id FROM collection_members WHERE collection_id = ? ORDER BY position ASC, workspace_id ASC`,
		collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
