// Package orchestrator wires goal/event-driven backlog ranking (PRISMCONDUCTOR_PLAN.md §8, §11).
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/goalfilter"
	"prismconductor/internal/types"
)

// Store is the slice of *store.Store we need.
type Store interface {
	ListIssues(workspaceID string) ([]types.Issue, error)
	SaveIssue(iss types.Issue) error
	ListGoals() ([]types.Goal, error)
	GetGoal(id string) (types.Goal, error)
	DepCacheGet(workspaceID string, issueNumber int, goalID, bodyHash string) (string, bool, error)
	DepCachePut(workspaceID string, issueNumber int, goalID, bodyHash string, payload any) error
}

type Orchestrator struct {
	bus   *eventbus.Bus
	llm   LLM
	store Store

	mu      sync.Mutex
	running bool
}

func New(bus *eventbus.Bus, llm LLM) *Orchestrator {
	o := &Orchestrator{bus: bus, llm: llm}
	bus.Subscribe(o.handle)
	return o
}

// SetStore wires persistence after construction. (App.startup builds the
// orchestrator before the store is available.)
func (o *Orchestrator) SetStore(s Store) { o.store = s }

// SetLLM swaps the LLM client. Used when the user changes the Ollama endpoint.
func (o *Orchestrator) SetLLM(llm LLM) { o.llm = llm }

// handle is the per-event router from §8.
func (o *Orchestrator) handle(e eventbus.Event) {
	switch e.Type {
	case eventbus.EvtGoalActivated:
		go o.runRank("goal_activated")
	case eventbus.EvtGoalUpdated:
		go o.runRank("goal_updated")
	case eventbus.EvtIssueAdded:
		go o.runRank("issue_added")
	case eventbus.EvtIssueClosed:
		go o.recomputeBlocked()
	case eventbus.EvtIssueLabelChanged:
		go o.runRank("issue_label_changed")
	}
}

// RunNow runs a rank pass immediately. Exposed for the "Re-rank now" button.
func (o *Orchestrator) RunNow() error { return o.runRank("manual") }

// runRank ranks the active goal's candidate issues and writes priority +
// dependencies onto each Issue row.
func (o *Orchestrator) runRank(reason string) error {
	if o.store == nil {
		return fmt.Errorf("orchestrator: store unavailable")
	}
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return nil
	}
	o.running = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	goals, err := o.store.ListGoals()
	if err != nil {
		return err
	}
	var active *types.Goal
	for i := range goals {
		if goals[i].Status == types.GoalActive {
			active = &goals[i]
			break
		}
	}
	if active == nil {
		log.Printf("orchestrator: no active goal (reason=%s)", reason)
		return nil
	}

	scope := ""
	if active.WorkspaceID != "" {
		scope = active.WorkspaceID
	}
	all, err := o.store.ListIssues(scope)
	if err != nil {
		return err
	}
	candidates := goalfilter.Apply(active.IssueFilter, openIssues(all))
	if len(candidates) == 0 {
		log.Printf("orchestrator: no candidate issues for goal %q", active.Title)
		return nil
	}

	cached, fresh := o.partitionByCache(*active, candidates)

	var llmResult *RankDepsResult
	if len(fresh) > 0 && o.llm != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		res, err := RankDeps(ctx, o.llm, *active, fresh)
		if err != nil {
			log.Printf("orchestrator: rank+deps failed (reason=%s): %v", reason, err)
			return err
		}
		llmResult = res
	}

	merged := mergeResults(cached, llmResult, candidates)
	if err := o.applyResult(*active, candidates, merged); err != nil {
		return err
	}

	o.bus.Publish(eventbus.EventType("orchestrator_ran"), map[string]any{
		"reason":    reason,
		"goal_id":   active.ID,
		"candidates": len(candidates),
		"primitives": merged.Primitives,
	})
	return nil
}

// partitionByCache splits candidates into (cached results merged in, issues
// still needing an LLM call).
func (o *Orchestrator) partitionByCache(goal types.Goal, candidates []types.Issue) (RankDepsResult, []types.Issue) {
	var merged RankDepsResult
	primSet := map[int]bool{}
	depMap := map[int][]int{}
	rationaleMap := map[int]string{}

	var fresh []types.Issue
	for _, iss := range candidates {
		hash := IssueBodyHash(iss)
		raw, hit, err := o.store.DepCacheGet(iss.WorkspaceID, iss.Number, goal.ID, hash)
		if err != nil || !hit {
			fresh = append(fresh, iss)
			continue
		}
		var entry depCacheEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			fresh = append(fresh, iss)
			continue
		}
		merged.Ordering = append(merged.Ordering, iss.Number)
		if entry.IsPrimitive {
			primSet[iss.Number] = true
		}
		if len(entry.DependsOn) > 0 {
			depMap[iss.Number] = entry.DependsOn
			rationaleMap[iss.Number] = entry.Rationale
		}
	}
	for n := range primSet {
		merged.Primitives = append(merged.Primitives, n)
	}
	for issue, deps := range depMap {
		merged.Dependencies = append(merged.Dependencies, DependencyEdge{
			Issue:     issue,
			DependsOn: deps,
			Rationale: rationaleMap[issue],
		})
	}
	return merged, fresh
}

type depCacheEntry struct {
	IsPrimitive bool   `json:"is_primitive"`
	DependsOn   []int  `json:"depends_on"`
	Rationale   string `json:"rationale"`
}

// mergeResults overlays LLM output onto cached partial results, drops issues
// not in the candidate set, and falls back to "candidate order" when the LLM
// didn't rank some entry.
func mergeResults(cached RankDepsResult, llm *RankDepsResult, candidates []types.Issue) RankDepsResult {
	out := cached
	if llm != nil {
		// LLM-supplied ordering goes first, then cached, then anything missing.
		seen := map[int]bool{}
		var ordering []int
		for _, n := range llm.Ordering {
			if seen[n] {
				continue
			}
			seen[n] = true
			ordering = append(ordering, n)
		}
		for _, n := range out.Ordering {
			if seen[n] {
				continue
			}
			seen[n] = true
			ordering = append(ordering, n)
		}
		for _, c := range candidates {
			if seen[c.Number] {
				continue
			}
			seen[c.Number] = true
			ordering = append(ordering, c.Number)
		}
		out.Ordering = ordering

		primSet := map[int]bool{}
		for _, n := range out.Primitives {
			primSet[n] = true
		}
		for _, n := range llm.Primitives {
			primSet[n] = true
		}
		out.Primitives = nil
		for n := range primSet {
			out.Primitives = append(out.Primitives, n)
		}

		// Replace dep edges per-issue with LLM's view if provided.
		edgeMap := map[int]DependencyEdge{}
		for _, e := range out.Dependencies {
			edgeMap[e.Issue] = e
		}
		for _, e := range llm.Dependencies {
			edgeMap[e.Issue] = e
		}
		out.Dependencies = nil
		for _, e := range edgeMap {
			out.Dependencies = append(out.Dependencies, e)
		}
		if llm.Rationale != "" {
			out.Rationale = llm.Rationale
		}
	}
	if len(out.Ordering) == 0 {
		// No LLM, no cache — fall back to current order.
		for _, c := range candidates {
			out.Ordering = append(out.Ordering, c.Number)
		}
	}
	return out
}

// applyResult writes the orchestrator's output back onto the issues table:
// priority (derived from ordering), dependencies, dep_rationale; and persists
// per-issue cache rows.
func (o *Orchestrator) applyResult(goal types.Goal, candidates []types.Issue, res RankDepsResult) error {
	priority := map[int]float64{}
	N := len(res.Ordering)
	for i, n := range res.Ordering {
		priority[n] = float64(N-i) / float64(N) // 1.0 for top, descending
	}
	primSet := map[int]bool{}
	for _, n := range res.Primitives {
		primSet[n] = true
	}
	depEdges := map[int]DependencyEdge{}
	for _, e := range res.Dependencies {
		depEdges[e.Issue] = e
	}

	for _, iss := range candidates {
		updated := iss
		updated.GoalID = goalIDPtr(goal.ID)
		if p, ok := priority[iss.Number]; ok {
			updated.Priority = p
		}
		if e, ok := depEdges[iss.Number]; ok {
			updated.Dependencies = e.DependsOn
			updated.DepRationale = e.Rationale
		} else {
			updated.Dependencies = nil
			updated.DepRationale = ""
		}
		if err := o.store.SaveIssue(updated); err != nil {
			return err
		}

		hash := IssueBodyHash(iss)
		entry := depCacheEntry{
			IsPrimitive: primSet[iss.Number],
			DependsOn:   updated.Dependencies,
			Rationale:   updated.DepRationale,
		}
		_ = o.store.DepCachePut(iss.WorkspaceID, iss.Number, goal.ID, hash, entry)
	}
	return nil
}

// recomputeBlocked re-evaluates each candidate's "blocked-by" status when an
// issue closes — open dep numbers stay, closed dep numbers are pruned.
func (o *Orchestrator) recomputeBlocked() error {
	if o.store == nil {
		return nil
	}
	all, err := o.store.ListIssues("")
	if err != nil {
		return err
	}
	openSet := map[int]bool{}
	for _, iss := range all {
		if iss.State == "open" {
			openSet[iss.Number] = true
		}
	}
	for _, iss := range all {
		if len(iss.Dependencies) == 0 {
			continue
		}
		var stillOpen []int
		for _, n := range iss.Dependencies {
			if openSet[n] {
				stillOpen = append(stillOpen, n)
			}
		}
		if len(stillOpen) != len(iss.Dependencies) {
			updated := iss
			updated.Dependencies = stillOpen
			_ = o.store.SaveIssue(updated)
		}
	}
	return nil
}

func openIssues(all []types.Issue) []types.Issue {
	out := make([]types.Issue, 0, len(all))
	for _, iss := range all {
		if iss.State == "" || iss.State == "open" {
			out = append(out, iss)
		}
	}
	return out
}

func goalIDPtr(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
