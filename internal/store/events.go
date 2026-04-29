package store

import (
	"encoding/json"
	"errors"
)

// LogEvent appends an event row. Used by the EventBus subscriber for §13's
// append-only event log.
func (s *Store) LogEvent(eventType string, payload any) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	var raw string
	if payload != nil {
		b, err := json.Marshal(payload)
		if err == nil {
			raw = string(b)
		}
	}
	_, err := s.DB.Exec(`INSERT INTO events (type, ts, payload) VALUES (?, strftime('%s', 'now'), ?)`, eventType, raw)
	return err
}

// RecentEvents returns the last n event rows, newest first.
type EventRow struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	TS      int64  `json:"ts"`
	Payload string `json:"payload"`
}

func (s *Store) RecentEvents(n int) ([]EventRow, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(`SELECT id, type, ts, IFNULL(payload, '') FROM events ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.ID, &r.Type, &r.TS, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
