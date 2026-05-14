package slack_test

import (
	"testing"

	"prismconductor/internal/slack"
)

func TestAuthRegistry(t *testing.T) {
	r := slack.NewAuthRegistry()

	// Unmapped user has no permissions.
	if r.CanRead("ws1", "USTRANGER") {
		t.Error("unmapped user should not be able to read")
	}
	if r.CanMutate("ws1", "USTRANGER") {
		t.Error("unmapped user should not be able to mutate")
	}

	// Map a read-only user.
	r.Set("ws1", "UREADONLY", slack.PermReadOnly)
	if !r.CanRead("ws1", "UREADONLY") {
		t.Error("read_only user should be able to read")
	}
	if r.CanMutate("ws1", "UREADONLY") {
		t.Error("read_only user should not be able to mutate")
	}

	// Map a full user.
	r.Set("ws1", "UFULL", slack.PermFull)
	if !r.CanRead("ws1", "UFULL") {
		t.Error("full user should be able to read")
	}
	if !r.CanMutate("ws1", "UFULL") {
		t.Error("full user should be able to mutate")
	}

	// Workspace isolation: UFULL in ws1 has no perms in ws2.
	if r.CanMutate("ws2", "UFULL") {
		t.Error("full user in ws1 should not have perms in ws2")
	}
}

func TestAuthRegistryLoadWorkspace(t *testing.T) {
	r := slack.NewAuthRegistry()

	r.Set("ws1", "UOLD", slack.PermFull)

	// LoadWorkspace replaces all ws1 entries.
	r.LoadWorkspace("ws1", map[string]string{
		"UNEW": slack.PermReadOnly,
	})

	if r.CanMutate("ws1", "UOLD") {
		t.Error("UOLD should have been evicted by LoadWorkspace")
	}
	if !r.CanRead("ws1", "UNEW") {
		t.Error("UNEW should be in the registry after LoadWorkspace")
	}
}

func TestAuthRegistryDelete(t *testing.T) {
	r := slack.NewAuthRegistry()
	r.Set("ws1", "UUSER", slack.PermFull)
	r.Delete("ws1", "UUSER")
	if r.CanMutate("ws1", "UUSER") {
		t.Error("deleted user should not be able to mutate")
	}
}
