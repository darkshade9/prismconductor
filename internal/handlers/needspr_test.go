package handlers

import (
	"strings"
	"testing"

	"prismconductor/internal/types"
)

func TestBuildManualPushCommand_NilInfo(t *testing.T) {
	if got := BuildManualPushCommand(nil); got != "" {
		t.Errorf("expected empty string for nil info, got %q", got)
	}
}

func TestBuildManualPushCommand_PushKind(t *testing.T) {
	info := &types.NeedsPRInfo{
		Branch:      "feat/my-branch",
		WorktreeDir: "/tmp/worktrees/prismconductor-42",
		Kind:        "push",
	}
	cmd := BuildManualPushCommand(info)
	if !strings.Contains(cmd, "git push -u origin feat/my-branch") {
		t.Errorf("push command missing push step: %q", cmd)
	}
	if strings.Contains(cmd, "git commit") {
		t.Errorf("push command should not contain commit step: %q", cmd)
	}
}

func TestBuildManualPushCommand_CommitSigningWithFile(t *testing.T) {
	info := &types.NeedsPRInfo{
		Branch:        "feat/signing-branch",
		WorktreeDir:   "/tmp/worktrees/prismconductor-99",
		Kind:          "commit_signing",
		CommitMsgFile: "/tmp/worktrees/prismconductor-99/.prismconductor/commit-msg/99.txt",
	}
	cmd := BuildManualPushCommand(info)
	if !strings.Contains(cmd, "git commit -S -F") {
		t.Errorf("commit+push command missing signed commit step: %q", cmd)
	}
	if !strings.Contains(cmd, "git push -u origin feat/signing-branch") {
		t.Errorf("commit+push command missing push step: %q", cmd)
	}
	if !strings.Contains(cmd, "99.txt") {
		t.Errorf("commit+push command should reference commit msg file: %q", cmd)
	}
}

func TestBuildManualPushCommand_CommitSigningNoFile(t *testing.T) {
	info := &types.NeedsPRInfo{
		Branch:      "feat/signing-branch",
		WorktreeDir: "/tmp/worktrees/prismconductor-99",
		Kind:        "commit_signing",
		// CommitMsgFile intentionally empty
	}
	cmd := BuildManualPushCommand(info)
	if !strings.Contains(cmd, "git commit -S -m") {
		t.Errorf("fallback command should use -m flag: %q", cmd)
	}
	if !strings.Contains(cmd, "git push -u origin feat/signing-branch") {
		t.Errorf("fallback command missing push step: %q", cmd)
	}
}

// TestBuildManualPushCommand_TierTransitions verifies that the three signing
// strategies map to the correct command shape (simulates Tier 1/2/3 outcomes).
func TestBuildManualPushCommand_TierTransitions(t *testing.T) {
	tests := []struct {
		name          string
		info          *types.NeedsPRInfo
		wantContains  []string
		wantAbsent    []string
	}{
		{
			name: "tier3 commit signing with msg file",
			info: &types.NeedsPRInfo{
				Branch:        "feat/tier3",
				WorktreeDir:   "/w",
				Kind:          "commit_signing",
				CommitMsgFile: "/w/.prismconductor/commit-msg/1.txt",
			},
			wantContains: []string{"git add -A", "git commit -S -F", "git push -u origin feat/tier3"},
		},
		{
			name: "tier2 push-only after local commit succeeded",
			info: &types.NeedsPRInfo{
				Branch:      "feat/tier2",
				WorktreeDir: "/w",
				Kind:        "push",
			},
			wantContains: []string{"git push -u origin feat/tier2"},
			wantAbsent:   []string{"git commit"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := BuildManualPushCommand(tc.info)
			for _, want := range tc.wantContains {
				if !strings.Contains(cmd, want) {
					t.Errorf("expected %q in command %q", want, cmd)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(cmd, absent) {
					t.Errorf("unexpected %q in command %q", absent, cmd)
				}
			}
		})
	}
}
