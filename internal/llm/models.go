package llm

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// ToolSupport describes the level of tool/function-calling support a model offers.
type ToolSupport string

const (
	ToolFull    ToolSupport = "full"
	ToolPartial ToolSupport = "partial"
	ToolNone    ToolSupport = "none"
)

// Fit describes how well a model suits a given conductor role.
type Fit string

const (
	FitExcellent  Fit = "excellent"
	FitGood       Fit = "good"
	FitFair       Fit = "fair"
	FitPoor       Fit = "poor"
	FitUnsuitable Fit = "unsuitable"
)

// CostTier is a rough relative cost category (not a precise number).
type CostTier string

const (
	CostFree    CostTier = "free"
	CostLow     CostTier = "low"
	CostMedium  CostTier = "medium"
	CostHigh    CostTier = "high"
	CostPremium CostTier = "premium"
)

// ModelHint carries capability metadata for one model.
// Wails serializes this to the frontend via App.GetModelHint.
type ModelHint struct {
	Provider      string      `json:"provider"`
	ModelID       string      `json:"model_id"`
	PlanFit       Fit         `json:"plan_fit"`
	WorkFit       Fit         `json:"work_fit"`
	OrchFit       Fit         `json:"orch_fit"`
	ArchitectFit  Fit         `json:"architect_fit"`
	ToolSupport   ToolSupport `json:"tool_support"`
	ContextWindow int         `json:"context_window"`
	CostTier      CostTier    `json:"cost_tier"`
	Notes         string      `json:"notes"`
	UpdatedAt     string      `json:"updated_at"`
	Source        string      `json:"source"`
}

// externalSnapshot mirrors the JSON structure of external_models.json.
type externalSnapshot struct {
	FetchedAt string      `json:"fetched_at"`
	Models    []ModelHint `json:"models"`
}

//go:embed external_models.json
var externalModelsJSON []byte

// bundledHints is the hand-curated, authoritative model matrix (~30 models).
// Keys are matched by exact provider+model_id, then by prefix (allowing
// version-suffix variants such as claude-sonnet-4-6-20251001 to match
// the bundled claude-sonnet-4-6 entry).
var bundledHints = []ModelHint{
	// ── Anthropic / Claude ────────────────────────────────────────────────
	{
		Provider: "claude", ModelID: "claude-opus-4-7",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitExcellent, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostPremium,
		Notes: "Highest-capability Claude model; ideal for all conductor roles.", UpdatedAt: "2025-05-01", Source: "bundled",
	},
	{
		Provider: "claude", ModelID: "claude-opus-4-6",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitExcellent, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostPremium,
		Notes: "Previous-gen Opus; strong across all roles.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	{
		Provider: "claude", ModelID: "claude-sonnet-4-6",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitExcellent, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostHigh,
		Notes: "Recommended default: excellent quality/cost balance for all roles.", UpdatedAt: "2025-05-01", Source: "bundled",
	},
	{
		Provider: "claude", ModelID: "claude-haiku-4-5",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitFair,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostMedium,
		Notes: "Fast and cheap; adequate for plan/work, weaker at orchestration.", UpdatedAt: "2025-05-01", Source: "bundled",
	},
	{
		Provider: "claude", ModelID: "claude-3-5-sonnet",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitGood, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostHigh,
		Notes: "Solid all-rounder; superseded by Claude 4.x for most use cases.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	// ── OpenAI ────────────────────────────────────────────────────────────
	{
		Provider: "openai", ModelID: "gpt-4o",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitGood, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 128000, CostTier: CostHigh,
		Notes: "OpenAI flagship; strong tool use and long context.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "gpt-4o-mini",
		PlanFit: FitGood, WorkFit: FitFair, OrchFit: FitFair, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 128000, CostTier: CostLow,
		Notes: "Budget-friendly; adequate for plan, weaker for heavy work/orchestrator.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "gpt-4-turbo",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitGood, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 128000, CostTier: CostHigh,
		Notes: "Reliable older model; superseded by gpt-4o.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "gpt-3.5-turbo",
		PlanFit: FitFair, WorkFit: FitPoor, OrchFit: FitPoor, ArchitectFit: FitPoor,
		ToolSupport: ToolPartial, ContextWindow: 16000, CostTier: CostLow,
		Notes: "Legacy model; insufficient reasoning for conductor roles.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "o1",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitGood, ArchitectFit: FitExcellent,
		ToolSupport: ToolNone, ContextWindow: 200000, CostTier: CostPremium,
		Notes: "Strong reasoning; no tool support — execute workers that need tool calls should avoid.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "o1-mini",
		PlanFit: FitGood, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolNone, ContextWindow: 128000, CostTier: CostMedium,
		Notes: "Reasoning model without tool support; limited conductor utility.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "o3",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitExcellent, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostPremium,
		Notes: "Latest OpenAI frontier model; excellent across all roles.", UpdatedAt: "2025-04-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "o3-mini",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 128000, CostTier: CostMedium,
		Notes: "Cost-effective reasoning with tool support.", UpdatedAt: "2025-04-01", Source: "bundled",
	},
	{
		Provider: "openai", ModelID: "o4-mini",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitGood, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 200000, CostTier: CostHigh,
		Notes: "Latest mini reasoning model; strong tool use.", UpdatedAt: "2025-04-01", Source: "bundled",
	},
	// ── Google Gemini ─────────────────────────────────────────────────────
	{
		Provider: "gemini", ModelID: "gemini-2.5-pro",
		PlanFit: FitExcellent, WorkFit: FitExcellent, OrchFit: FitGood, ArchitectFit: FitExcellent,
		ToolSupport: ToolFull, ContextWindow: 1000000, CostTier: CostHigh,
		Notes: "Largest Gemini 2.5; 1M context; great for long-horizon planning.", UpdatedAt: "2025-04-01", Source: "bundled",
	},
	{
		Provider: "gemini", ModelID: "gemini-2.5-flash",
		PlanFit: FitGood, WorkFit: FitExcellent, OrchFit: FitGood, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 1000000, CostTier: CostMedium,
		Notes: "Fast, cost-effective Gemini with 1M context; strong for work pools.", UpdatedAt: "2025-04-01", Source: "bundled",
	},
	{
		Provider: "gemini", ModelID: "gemini-2.5-flash-lite",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 1000000, CostTier: CostLow,
		Notes: "Cheapest 2.5 variant; partial tool support limits agentic use.", UpdatedAt: "2026-05-13", Source: "bundled",
	},
	{
		Provider: "gemini", ModelID: "gemini-2.0-flash",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 1000000, CostTier: CostLow,
		Notes: "Solid all-around; cheaper than 2.5 with slightly lower capability.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "gemini", ModelID: "gemini-1.5-pro",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 1000000, CostTier: CostMedium,
		Notes: "Previous generation; still capable but superseded by 2.x.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	{
		Provider: "gemini", ModelID: "gemini-1.5-flash",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 1000000, CostTier: CostLow,
		Notes: "Lightweight Gemini; partial tool support limits conductor utility.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	// ── LiteLLM / OpenAI-compat proxies ──────────────────────────────────
	{
		Provider: "litellm", ModelID: "deepseek/deepseek-r1",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitGood,
		ToolSupport: ToolPartial, ContextWindow: 64000, CostTier: CostLow,
		Notes: "Strong reasoning; partial tool support.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "litellm", ModelID: "deepseek/deepseek-chat",
		PlanFit: FitGood, WorkFit: FitGood, OrchFit: FitFair, ArchitectFit: FitGood,
		ToolSupport: ToolFull, ContextWindow: 64000, CostTier: CostLow,
		Notes: "DeepSeek V3; full tool support at low cost.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "litellm", ModelID: "mistral/mistral-large-latest",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 128000, CostTier: CostMedium,
		Notes: "Capable but trails frontier models for agentic tasks.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "litellm", ModelID: "codestral/codestral-latest",
		PlanFit: FitFair, WorkFit: FitGood, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 32000, CostTier: CostMedium,
		Notes: "Code-specialized Mistral; good for work pools, weak at orchestration.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	// ── Ollama (local / free tier) ────────────────────────────────────────
	{
		Provider: "ollama", ModelID: "llama3.3:70b",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 128000, CostTier: CostFree,
		Notes: "Best local Llama option; partial tool support limits orchestration.", UpdatedAt: "2025-02-01", Source: "bundled",
	},
	{
		Provider: "ollama", ModelID: "qwen2.5-coder:32b",
		PlanFit: FitFair, WorkFit: FitGood, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 32000, CostTier: CostFree,
		Notes: "Strong local code model; good for work pools on capable hardware.", UpdatedAt: "2025-02-01", Source: "bundled",
	},
	{
		Provider: "ollama", ModelID: "deepseek-r1:70b",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 64000, CostTier: CostFree,
		Notes: "Local reasoning variant; slow on consumer hardware.", UpdatedAt: "2025-03-01", Source: "bundled",
	},
	{
		Provider: "ollama", ModelID: "mistral:7b",
		PlanFit: FitPoor, WorkFit: FitPoor, OrchFit: FitUnsuitable, ArchitectFit: FitPoor,
		ToolSupport: ToolNone, ContextWindow: 32000, CostTier: CostFree,
		Notes: "Small local model; inadequate for conductor roles.", UpdatedAt: "2025-01-01", Source: "bundled",
	},
	{
		Provider: "ollama", ModelID: "phi4:14b",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 16000, CostTier: CostFree,
		Notes: "Compact Phi-4; limited context window.", UpdatedAt: "2025-02-01", Source: "bundled",
	},
	{
		Provider: "ollama", ModelID: "gemma3:27b",
		PlanFit: FitFair, WorkFit: FitFair, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolNone, ContextWindow: 128000, CostTier: CostFree,
		Notes: "Google Gemma local; no tool support.", UpdatedAt: "2025-02-01", Source: "bundled",
	},
	// ── LM Studio ────────────────────────────────────────────────────────
	{
		Provider: "lmstudio", ModelID: "qwen2.5-coder-32b-instruct",
		PlanFit: FitFair, WorkFit: FitGood, OrchFit: FitPoor, ArchitectFit: FitFair,
		ToolSupport: ToolPartial, ContextWindow: 32000, CostTier: CostFree,
		Notes: "Popular LM Studio code model; prefer qwen2.5-coder:32b on ollama for better support.", UpdatedAt: "2025-02-01", Source: "bundled",
	},
}

// hintIndex caches the bundled entries keyed by "provider:model_id" (lowercase).
var (
	hintIndex     map[string]*ModelHint
	externalIndex map[string]*ModelHint
	externalAt    string
	hintOnce      sync.Once
)

func buildIndexes() {
	hintIndex = make(map[string]*ModelHint, len(bundledHints))
	for i := range bundledHints {
		h := &bundledHints[i]
		key := strings.ToLower(h.Provider) + ":" + strings.ToLower(h.ModelID)
		hintIndex[key] = h
	}

	// Load the committed external snapshot (non-fatal if malformed).
	externalIndex = make(map[string]*ModelHint)
	var snap externalSnapshot
	if err := json.Unmarshal(externalModelsJSON, &snap); err == nil {
		externalAt = snap.FetchedAt
		for i := range snap.Models {
			m := &snap.Models[i]
			if m.Source == "" {
				m.Source = "hermesguide.xyz"
			}
			if externalAt != "" {
				m.Source = "hermesguide.xyz (refreshed " + externalAt + ")"
			}
			key := strings.ToLower(m.Provider) + ":" + strings.ToLower(m.ModelID)
			externalIndex[key] = m
		}
	}
}

// LookupHint returns the capability hint for the given provider and modelID.
// Bundled entries always take precedence over the external snapshot.
// Returns nil if no entry matches (the UI should treat nil as "unknown").
func LookupHint(provider, modelID string) *ModelHint {
	hintOnce.Do(buildIndexes)

	prov := strings.ToLower(strings.TrimSpace(provider))
	mid := strings.ToLower(strings.TrimSpace(modelID))

	// 1. Exact bundled match.
	if h := hintIndex[prov+":"+mid]; h != nil {
		return h
	}

	// 2. Prefix bundled match — allows version-suffix variants
	//    (e.g. "claude-sonnet-4-6-20251001" matches "claude-sonnet-4-6").
	for key, h := range hintIndex {
		prefix := strings.TrimPrefix(key, prov+":")
		if prefix != key && strings.HasPrefix(mid, prefix) {
			// Clone so callers can't mutate the indexed entry.
			copy := *h
			return &copy
		}
	}

	// 3. Exact external match.
	if h := externalIndex[prov+":"+mid]; h != nil {
		return h
	}

	// 4. Prefix external match.
	for key, h := range externalIndex {
		prefix := strings.TrimPrefix(key, prov+":")
		if prefix != key && strings.HasPrefix(mid, prefix) {
			copy := *h
			return &copy
		}
	}

	return nil
}

// RoleFit extracts the Fit value for a given role name from a ModelHint.
// Role names match the conductor pool roles: "plan", "work", "orchestrator", "architect".
func RoleFit(h *ModelHint, role string) Fit {
	if h == nil {
		return ""
	}
	switch strings.ToLower(role) {
	case "plan":
		return h.PlanFit
	case "work":
		return h.WorkFit
	case "orchestrator":
		return h.OrchFit
	case "architect":
		return h.ArchitectFit
	default:
		return h.WorkFit
	}
}
