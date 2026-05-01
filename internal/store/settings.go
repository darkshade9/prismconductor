package store

import (
	"encoding/json"
	"errors"
)

// GetSetting returns the value for a key, or "" if missing.
func (s *Store) GetSetting(key string) (string, error) {
	if s == nil || s.DB == nil {
		return "", errors.New("store unavailable")
	}
	var v string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return "", nil
	}
	return v, nil
}

// SetSetting upserts a kv pair.
func (s *Store) SetSetting(key, value string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// DeleteSetting removes a kv row. No error if absent.
func (s *Store) DeleteSetting(key string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return err
}

// GetLabelFilter returns the persisted label filter selection for a workspace.
// Returns empty labels slice and "or" mode when the key is absent.
func (s *Store) GetLabelFilter(workspaceID string) (labels []string, mode string, err error) {
	labelsStr, err := s.GetSetting("label_filter:" + workspaceID + ":labels")
	if err != nil {
		return []string{}, "or", err
	}
	mode, _ = s.GetSetting("label_filter:" + workspaceID + ":mode")
	if labelsStr != "" {
		if jerr := json.Unmarshal([]byte(labelsStr), &labels); jerr != nil {
			labels = []string{}
		}
	}
	if labels == nil {
		labels = []string{}
	}
	if mode == "" {
		mode = "or"
	}
	return labels, mode, nil
}

// SetLabelFilter persists the label filter selection for a workspace.
func (s *Store) SetLabelFilter(workspaceID string, labels []string, mode string) error {
	if labels == nil {
		labels = []string{}
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	if err := s.SetSetting("label_filter:"+workspaceID+":labels", string(b)); err != nil {
		return err
	}
	return s.SetSetting("label_filter:"+workspaceID+":mode", mode)
}

// AllSettings returns the full kv table as a map.
func (s *Store) AllSettings() (map[string]string, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
