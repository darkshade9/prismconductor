package session

import (
	"strings"
	"testing"

	"prismconductor/internal/types"
)

// collectionPersister is a fakePersister extension that injects collection data
// for the prompt-generation tests. It embeds fakePersister so all other
// Persister methods are satisfied.
type collectionPersister struct {
	fakePersister
	col   types.Collection
	found bool
	paths []string
}

func (p *collectionPersister) CollectionForWorkspace(_ string) (types.Collection, bool, error) {
	return p.col, p.found, nil
}
func (p *collectionPersister) RelatedRepoPaths(_ string) ([]string, error) {
	return p.paths, nil
}

// bundledWS returns a workspace wired to use the default bundled skill mode
// (no PerStage override — exercises the legacy Mode path).
func bundledWS(id, repoPath string) types.Workspace {
	return types.Workspace{
		ID:       id,
		RepoPath: repoPath,
		SkillProfile: types.SkillProfile{
			Mode: types.SkillModeBundled,
		},
	}
}

func hybridWS(id, repoPath, nativeCmd string) types.Workspace {
	return types.Workspace{
		ID:       id,
		RepoPath: repoPath,
		SkillProfile: types.SkillProfile{
			Mode:              types.SkillModeHybrid,
			NativePlanCommand: nativeCmd,
		},
	}
}

// TestPlanPromptNoCollectionIsUnchanged guards the regression: a workspace not
// in any collection must produce a byte-identical prompt to what the system
// emits today.
func TestPlanPromptNoCollectionIsUnchanged(t *testing.T) {
	ws := bundledWS("ws-1", "/repo/app")
	issue := types.Issue{Number: 42}

	// Expected: unchanged bundled plan prompt (no collection fields)
	want := "/conductor-plan --issue 42 --repo /repo/app"
	got := planPrompt(ws, issue, nil, "")
	if got != want {
		t.Errorf("planPrompt without collection =\n  %q\nwant\n  %q", got, want)
	}
}

// TestPlanPromptBundledCollectionAppendsFlags verifies that Bundled mode gets
// the shared-context prefix and repeated --related-repos flags.
func TestPlanPromptBundledCollectionAppendsFlags(t *testing.T) {
	ws := bundledWS("ws-app", "/repo/app")
	issue := types.Issue{Number: 7}
	related := []string{"/repo/infra", "/repo/helm"}
	contextMD := "# Shared architecture\nUse gRPC between services."

	got := planPrompt(ws, issue, related, contextMD)

	if !strings.HasPrefix(got, "## Shared collection context\n\n"+contextMD+"\n\n---\n\n") {
		t.Errorf("prompt missing shared-context prefix:\n%s", got)
	}
	if !strings.Contains(got, "--related-repos /repo/infra") {
		t.Errorf("prompt missing --related-repos for infra:\n%s", got)
	}
	if !strings.Contains(got, "--related-repos /repo/helm") {
		t.Errorf("prompt missing --related-repos for helm:\n%s", got)
	}
}

// TestPlanPromptBundledCollectionDisabledSiblingAbsent verifies that disabled
// siblings are excluded from --related-repos (store already filters them, so
// this tests that a nil/empty related list produces no flag).
func TestPlanPromptBundledCollectionDisabledSiblingAbsent(t *testing.T) {
	ws := bundledWS("ws-app", "/repo/app")
	issue := types.Issue{Number: 9}
	// Store returns empty because the only sibling is disabled.
	got := planPrompt(ws, issue, nil, "# Context")

	if strings.Contains(got, "--related-repos") {
		t.Errorf("disabled sibling should not produce --related-repos flag:\n%s", got)
	}
	if !strings.Contains(got, "# Context") {
		t.Errorf("contextMD prefix missing:\n%s", got)
	}
}

// TestPlanPromptHybridGetsContextButNoFlags verifies that Hybrid mode receives
// the shared contextMD prefix but no --related-repos flag.
func TestPlanPromptHybridGetsContextButNoFlags(t *testing.T) {
	ws := hybridWS("ws-app", "/repo/app", "my-planner")
	issue := types.Issue{Number: 3}
	related := []string{"/repo/infra"}
	contextMD := "API contracts"

	got := planPrompt(ws, issue, related, contextMD)

	if strings.Contains(got, "--related-repos") {
		t.Errorf("Hybrid mode must not emit --related-repos:\n%s", got)
	}
	if !strings.Contains(got, contextMD) {
		t.Errorf("Hybrid mode must include contextMD prefix:\n%s", got)
	}
}

// TestCollectionContextNoStore ensures collectionContext is safe when store is nil.
func TestCollectionContextNoStore(t *testing.T) {
	m := &Manager{}
	siblings, contextMD := m.collectionContext("ws-1")
	if siblings != nil || contextMD != "" {
		t.Errorf("collectionContext with nil store should return empty, got siblings=%v contextMD=%q", siblings, contextMD)
	}
}

// TestCollectionContextNotInCollection ensures collectionContext returns empty
// when the workspace is not in any collection.
func TestCollectionContextNotInCollection(t *testing.T) {
	store := &collectionPersister{found: false}
	m := &Manager{store: store}
	siblings, contextMD := m.collectionContext("ws-1")
	if siblings != nil || contextMD != "" {
		t.Errorf("collectionContext for non-member should return empty, got siblings=%v contextMD=%q", siblings, contextMD)
	}
}

// TestCollectionContextInCollection ensures collectionContext returns siblings
// and contextMD when the workspace is a collection member.
func TestCollectionContextInCollection(t *testing.T) {
	store := &collectionPersister{
		found: true,
		col:   types.Collection{Name: "my-col", ContextMD: "# Shared doc"},
		paths: []string{"/repo/infra", "/repo/helm"},
	}
	m := &Manager{store: store}
	siblings, contextMD := m.collectionContext("ws-app")
	if contextMD != "# Shared doc" {
		t.Errorf("contextMD = %q, want %q", contextMD, "# Shared doc")
	}
	if len(siblings) != 2 || siblings[0] != "/repo/infra" || siblings[1] != "/repo/helm" {
		t.Errorf("siblings = %v, want [/repo/infra /repo/helm]", siblings)
	}
}

// TestShellQuoteNoSpecial passes through paths without shell-special chars.
func TestShellQuoteNoSpecial(t *testing.T) {
	if got := shellQuote("/repo/app"); got != "/repo/app" {
		t.Errorf("shellQuote(%q) = %q, want unchanged", "/repo/app", got)
	}
}

// TestShellQuoteWithSpace wraps space-containing paths in single quotes.
func TestShellQuoteWithSpace(t *testing.T) {
	got := shellQuote("/my repos/app")
	if got != "'/my repos/app'" {
		t.Errorf("shellQuote with space = %q, want %q", got, "'/my repos/app'")
	}
}
