package slack

import (
	"fmt"
	"sync"
)

// Permission levels for Slack users within a conductor workspace.
const (
	PermNone     = ""          // no access (not mapped)
	PermReadOnly = "read_only" // may run read-only commands
	PermFull     = "full"      // may run mutating commands
)

// AuthRegistry maps Slack user IDs to conductor permission levels per workspace.
// It is safe for concurrent use. Entries are loaded from the workspace's
// SlackConfig.UserMap at startup and refreshed via UpdateWorkspace.
type AuthRegistry struct {
	mu      sync.RWMutex
	entries map[string]string // key: wsID+":"+slackUserID → perm
}

// NewAuthRegistry returns an empty AuthRegistry.
func NewAuthRegistry() *AuthRegistry {
	return &AuthRegistry{entries: make(map[string]string)}
}

func registryKey(workspaceID, slackUserID string) string {
	return workspaceID + ":" + slackUserID
}

// Set stores (or updates) the permission level for a Slack user in a workspace.
func (r *AuthRegistry) Set(workspaceID, slackUserID, perm string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[registryKey(workspaceID, slackUserID)] = perm
}

// Delete removes the mapping for a Slack user in a workspace.
func (r *AuthRegistry) Delete(workspaceID, slackUserID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, registryKey(workspaceID, slackUserID))
}

// LoadWorkspace replaces all mappings for a workspace with the provided map.
// userMap is slackUserID → perm string.
func (r *AuthRegistry) LoadWorkspace(workspaceID string, userMap map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := workspaceID + ":"
	// Remove stale entries for this workspace.
	for k := range r.entries {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(r.entries, k)
		}
	}
	for slackUID, perm := range userMap {
		r.entries[registryKey(workspaceID, slackUID)] = perm
	}
}

// Perm returns the permission level for slackUserID in workspaceID.
// Returns PermNone when unmapped.
func (r *AuthRegistry) Perm(workspaceID, slackUserID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[registryKey(workspaceID, slackUserID)]
}

// CanMutate returns true when the user is authorized for mutating commands.
func (r *AuthRegistry) CanMutate(workspaceID, slackUserID string) bool {
	return r.Perm(workspaceID, slackUserID) == PermFull
}

// CanRead returns true when the user may run read-only commands.
func (r *AuthRegistry) CanRead(workspaceID, slackUserID string) bool {
	p := r.Perm(workspaceID, slackUserID)
	return p == PermFull || p == PermReadOnly
}

// ErrUnauthorized is returned by command handlers when the caller lacks
// sufficient permission.
type ErrUnauthorized struct {
	SlackUserID string
	Required    string
}

func (e *ErrUnauthorized) Error() string {
	return fmt.Sprintf("slack user %s lacks %s permission", e.SlackUserID, e.Required)
}
