// Package archiver implements per-workspace automatic archiving of DONE cards
// whose GitHub issue has been closed for at least N days (issue #107).
package archiver

import (
	"context"
	"fmt"
	"log"

	"prismconductor/internal/types"
)

// Store is the subset of store.Store consumed by this package.
type Store interface {
	ArchiveClosedByAge(workspaceID string, daysClosed int) (int, error)
}

// RunAutoArchive archives DONE cards in ws that are older than the configured
// threshold. Returns the count archived, or 0 if AutoArchive is disabled.
func RunAutoArchive(ctx context.Context, s Store, ws types.Workspace) (int, error) {
	cfg := ws.AutoArchive
	if !cfg.Enabled {
		return 0, nil
	}
	days := cfg.DaysClosed
	if days < 1 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	n, err := s.ArchiveClosedByAge(ws.ID, days)
	if err != nil {
		return 0, fmt.Errorf("auto-archive workspace %q: %w", ws.ID, err)
	}
	if n > 0 {
		log.Printf("auto-archive: workspace %q archived %d card(s) closed >%d day(s) ago", ws.ID, n, days)
	}
	return n, nil
}
