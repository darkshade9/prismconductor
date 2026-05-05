package workspace

import (
	"log"
	"time"
)

// provisioning rows older than this are treated as abandoned and removed.
const provisioningStaleAfter = 10 * time.Minute

// ReconcileProvisioning removes workspace rows that are stuck with
// Provisioning=true and whose ProvisioningAt is older than provisioningStaleAfter.
// This cleans up rows left by older code (pre-issue-192) or by a rare process
// crash between CF worker creation and registry.Add. Safe to call repeatedly;
// each call is idempotent.
//
// Returns the IDs of workspaces that were removed.
func (r *Registry) ReconcileProvisioning() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var stale []string
	var kept []any

	for _, ws := range r.items {
		if ws.Provisioning && isStale(ws.ProvisioningAt, now) {
			stale = append(stale, ws.ID)
		} else {
			kept = append(kept, ws)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	// Rebuild items list without the stale rows.
	filtered := r.items[:0]
	for _, ws := range r.items {
		remove := false
		for _, id := range stale {
			if ws.ID == id {
				remove = true
				break
			}
		}
		if !remove {
			filtered = append(filtered, ws)
		}
	}
	_ = kept
	r.items = filtered

	if err := r.save(); err != nil {
		log.Printf("workspace.ReconcileProvisioning: save after cleanup: %v", err)
	}
	for _, id := range stale {
		log.Printf("workspace.ReconcileProvisioning: removed stale provisioning workspace %s", id)
	}
	return stale
}

func isStale(provisioningAt *time.Time, now time.Time) bool {
	if provisioningAt == nil {
		// Row has no timestamp — treat as stale if Provisioning is set.
		return true
	}
	return now.Sub(*provisioningAt) > provisioningStaleAfter
}
