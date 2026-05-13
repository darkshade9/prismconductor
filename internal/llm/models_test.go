package llm

import (
	"sync"
	"testing"
)

// resetIndexes wipes the once guard so tests can re-initialise cleanly.
func resetIndexes() {
	hintOnce = sync.Once{}
	hintIndex = nil
	externalIndex = nil
}

func TestLookupHint_ExactBundled(t *testing.T) {
	resetIndexes()
	h := LookupHint("claude", "claude-sonnet-4-6")
	if h == nil {
		t.Fatal("expected hint for claude-sonnet-4-6, got nil")
	}
	if h.Source != "bundled" {
		t.Errorf("expected source=bundled, got %q", h.Source)
	}
	if h.ToolSupport != ToolFull {
		t.Errorf("expected tool_support=full, got %q", h.ToolSupport)
	}
	if h.PlanFit != FitExcellent {
		t.Errorf("expected plan_fit=excellent, got %q", h.PlanFit)
	}
}

func TestLookupHint_PrefixBundled(t *testing.T) {
	resetIndexes()
	// Version-suffixed ID should match the prefix-stripped bundled entry.
	h := LookupHint("claude", "claude-sonnet-4-6-20251001")
	if h == nil {
		t.Fatal("expected prefix match for claude-sonnet-4-6-20251001, got nil")
	}
	if h.Source != "bundled" {
		t.Errorf("expected source=bundled for prefix match, got %q", h.Source)
	}
}

func TestLookupHint_Unknown(t *testing.T) {
	resetIndexes()
	h := LookupHint("openai", "gpt-99-turbo-ultra-mega")
	if h != nil {
		t.Errorf("expected nil for unknown model, got %+v", h)
	}
}

func TestGeminiFlashLiteInBundledMatrix(t *testing.T) {
	resetIndexes()
	h := LookupHint("gemini", "gemini-2.5-flash-lite")
	if h == nil {
		t.Fatal("gemini-2.5-flash-lite missing from bundled capability matrix")
	}
	if h.Source != "bundled" {
		t.Errorf("source = %q, want bundled", h.Source)
	}
	if h.ToolSupport != ToolPartial {
		t.Errorf("tool_support = %q, want partial", h.ToolSupport)
	}
	if h.CostTier != CostLow {
		t.Errorf("cost_tier = %q, want low", h.CostTier)
	}
	if h.OrchFit != FitPoor {
		t.Errorf("orch_fit = %q, want poor (limits agentic use)", h.OrchFit)
	}
}

func TestLookupHint_CaseInsensitive(t *testing.T) {
	resetIndexes()
	h := LookupHint("CLAUDE", "Claude-Opus-4-7")
	if h == nil {
		t.Fatal("expected case-insensitive match, got nil")
	}
}

func TestLookupHint_BundledOverridesExternal(t *testing.T) {
	resetIndexes()
	// Load the bundled index first.
	hintOnce.Do(buildIndexes)

	// Inject a fake external entry for an existing bundled model.
	externalIndex["claude:claude-sonnet-4-6"] = &ModelHint{
		Provider: "claude",
		ModelID:  "claude-sonnet-4-6",
		Source:   "hermesguide.xyz",
		WorkFit:  FitPoor, // intentionally wrong, should be overridden
	}

	h := LookupHint("claude", "claude-sonnet-4-6")
	if h == nil {
		t.Fatal("expected hint, got nil")
	}
	if h.Source != "bundled" {
		t.Errorf("bundled should override external; got source=%q, work_fit=%q", h.Source, h.WorkFit)
	}
	if h.WorkFit == FitPoor {
		t.Error("external entry should not have overridden bundled entry")
	}
}

func TestLookupHint_ExternalFallback(t *testing.T) {
	resetIndexes()
	hintOnce.Do(buildIndexes)

	// Inject a fake external-only model.
	externalIndex["openai:gpt-99-ultra"] = &ModelHint{
		Provider: "openai",
		ModelID:  "gpt-99-ultra",
		Source:   "hermesguide.xyz (refreshed 2025-01-01)",
		WorkFit:  FitGood,
	}

	h := LookupHint("openai", "gpt-99-ultra")
	if h == nil {
		t.Fatal("expected external fallback, got nil")
	}
	if h.Source == "bundled" {
		t.Error("external-only model should not report source=bundled")
	}
}

func TestRoleFit(t *testing.T) {
	h := &ModelHint{
		PlanFit: FitExcellent, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitPoor,
	}
	cases := []struct {
		role string
		want Fit
	}{
		{"plan", FitExcellent},
		{"work", FitGood},
		{"orchestrator", FitFair},
		{"architect", FitPoor},
		{"unknown", FitGood}, // fallback to WorkFit
	}
	for _, c := range cases {
		got := RoleFit(h, c.role)
		if got != c.want {
			t.Errorf("RoleFit(%q) = %q, want %q", c.role, got, c.want)
		}
	}
}

func TestRoleFit_Nil(t *testing.T) {
	if got := RoleFit(nil, "work"); got != "" {
		t.Errorf("RoleFit(nil) = %q, want empty string", got)
	}
}

func TestBundledMatrix_Size(t *testing.T) {
	if len(bundledHints) < 30 {
		t.Errorf("expected at least 30 bundled entries, got %d", len(bundledHints))
	}
}

func TestBundledMatrix_NoEmptyFields(t *testing.T) {
	for _, h := range bundledHints {
		if h.Provider == "" {
			t.Errorf("entry %q has empty Provider", h.ModelID)
		}
		if h.ModelID == "" {
			t.Errorf("entry with provider=%q has empty ModelID", h.Provider)
		}
		if h.WorkFit == "" {
			t.Errorf("%s:%s has empty WorkFit", h.Provider, h.ModelID)
		}
		if h.ToolSupport == "" {
			t.Errorf("%s:%s has empty ToolSupport", h.Provider, h.ModelID)
		}
	}
}
