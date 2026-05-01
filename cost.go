package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"prismconductor/internal/llm"
	"prismconductor/internal/types"
)

// CostEstimate is the estimated execute-cost payload returned by PlanCostEstimate
// (issue #47). HasRate is false when no rate is configured for the model; in
// that case CostUSD is 0 and the UI should display '-'.
type CostEstimate struct {
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
	HasRate bool    `json:"has_rate"`
	Model   string  `json:"model"`
}

// IssueCost returns the cumulative persisted LLM cost_usd for an issue.
func (a *App) IssueCost(workspaceID string, issueNumber int) (float64, error) {
	if a.store == nil {
		return 0, nil
	}
	iss, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return 0, err
	}
	return iss.CostUSD, nil
}

// PlanCostEstimate returns an estimated execute cost for the latest plan of an
// issue. The estimate uses the Q1=A heuristic: 5000 base tokens + 80 tokens per
// LOC across files_to_modify + 5 tokens per plan_markdown word.
func (a *App) PlanCostEstimate(workspaceID string, issueNumber int) (CostEstimate, error) {
	if a.store == nil {
		return CostEstimate{}, nil
	}
	plan, err := a.store.LatestPlan(workspaceID, issueNumber)
	if err != nil || plan == nil {
		return CostEstimate{}, err
	}

	model := resolveWorkPoolModel(a, workspaceID)

	// Count lines across all files_to_modify. Files that can't be read
	// (not yet created, outside the repo) fall back to 100 lines each.
	var ws types.Workspace
	if a.wsReg != nil {
		ws, _ = a.wsReg.Get(workspaceID)
	}
	totalLines := 0
	for _, fi := range plan.FilesToModify {
		path := filepath.Join(ws.RepoPath, fi.Path)
		if b, err := os.ReadFile(path); err == nil {
			totalLines += bytes.Count(b, []byte("\n"))
		} else {
			totalLines += 100
		}
	}

	planWords := len(strings.Fields(plan.PlanMarkdown))
	tokens := llm.EstimateTokens(totalLines, planWords)

	rates, hasRate := llm.LookupRates(model)
	var costUSD float64
	if hasRate {
		costUSD = llm.EstimateCostUSD(tokens, rates)
	}

	return CostEstimate{
		Tokens:  tokens,
		CostUSD: costUSD,
		HasRate: hasRate,
		Model:   model,
	}, nil
}

// resolveWorkPoolModel finds the model string for the workspace's preferred
// work pool, or the first enabled work pool if no preference is set.
func resolveWorkPoolModel(a *App, workspaceID string) string {
	if a.store == nil {
		return ""
	}
	pools, err := a.store.ListPools()
	if err != nil {
		return ""
	}
	var ws types.Workspace
	if a.wsReg != nil {
		ws, _ = a.wsReg.Get(workspaceID)
	}
	preferredID := ws.SkillProfile.PreferredWorkPoolID
	first := ""
	for _, p := range pools {
		if !p.Enabled {
			continue
		}
		if preferredID != "" && p.ID == preferredID {
			return p.Model
		}
		if p.Role == types.RoleWork && first == "" {
			first = p.Model
		}
	}
	return first
}
