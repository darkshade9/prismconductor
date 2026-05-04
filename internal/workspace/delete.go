package workspace

import (
	"log"

	"prismconductor/internal/secretstore"
	"prismconductor/internal/types"
)

// RemoteCleanup removes the OS keyring (or file fallback) entry for a
// workspace's CF API token, if one was stored. Errors are logged but not
// returned — a missing or inaccessible keyring must not block workspace
// deletion. The workspace metadata update is the caller's responsibility.
func RemoteCleanup(ws types.Workspace, store secretstore.Store) {
	if ws.RemoteConfig == nil {
		return
	}
	ref := ws.RemoteConfig.SecretRefs.CFAPITokenRef
	if ref == "" {
		return
	}
	if err := secretstore.DeleteFromRef(ref); err != nil {
		log.Printf("workspace.RemoteCleanup: delete CF token ref %q for %s: %v (non-fatal)", ref, ws.ID, err)
	}
}
