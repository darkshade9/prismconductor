// Package orchestrator wires goal/event-driven backlog ranking (PRISMCONDUCTOR_PLAN.md §8, §11).
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/goalfilter"
	"prismconductor/internal/types"
)

// Store is the slice of *store.Store we need.
type Store interface {
	ListIssues(workspaceID string) ([]types.Issue, error)
	SaveIssue(iss types.Issue) (bool, error)
	ListGoals() ([]types.Goal, error)
	GetGoal(id string) (types.Goal, error)
	DepCacheGet(workspaceID string, issueNumber int, goalID, bodyHash string) (string, bool, error)
	DepCachePut(workspaceID string, issueNumber int, goalID, bodyHash string, payload any) error
	MoveIssueColumn(workspaceID string, number int, column types.BoardColumn) error
	// Issue #40: pending pool queue.
	LoadIssue(workspaceID string, number int) (types.Issue, error)
	EnqueuePendingPool(workspaceID string, issueNumber int, role types.Role, action string) error
	ListPendingPools(limit int) ([]types.PendingPoolRequest, error)
	DequeuePendingPool(id int64) error
	DeletePendingForIssue(workspaceID string, issueNumber int) error
	SetIssueWaitingForPool(workspaceID string, issueNumber int, waiting bool) error
}

// PoolRouter is the slice of *workerpool.Registry we need (issue #27,
// role-keyed in #39). The orchestrator never compares against a specific
// Provider constant; routing eligibility is the registry's concern. autoPull
// only ever spawns plan-mode workers, so it asks for plan-role pools
// exclusively — no fallback to work pools.
type PoolRouter interface {
	AcquireForPlan(ws types.Workspace) (poolID string, ok bool)
	AcquireForWork(ws types.Workspace) (poolID string, ok bool) // issue #40 drain
	ReleaseByPool(poolID string)
	FreePlanSlots() int
}

// LLMResolver returns the orchestrator-LLM client to use for the next rank
// pass, or nil when no enabled role=orchestrator pool exists. Resolved on
// every runRank call so changes via the UI take effect on the next event tick
// without an app restart.
type LLMResolver func() LLM

// SpawnPlanFunc lets the orchestrator launch a plan worker without depending
// on the session package directly. poolID is the pool the registry already
// reserved a slot on; the spawn callback is responsible for resolving the row
// and threading the Pool through to the session manager.
type SpawnPlanFunc func(workspaceID string, issueNumber int, poolID string) error

// SpawnWorkFunc mirrors SpawnPlanFunc for execute-mode workers (issue #40
// pending pool drain for work role).
type SpawnWorkFunc func(workspaceID string, issueNumber int, poolID string) error

// WorkspaceLookup returns the full Workspace by ID. Optional — when nil, the
// orchestrator falls back to a bare-ID Workspace, which means PreferredPlanPoolID
// pinning is ignored (round-robin only).
type WorkspaceLookup func(id string) (types.Workspace, bool)

type Orchestrator struct {
	bus        *eventbus.Bus
	resolveLLM LLMResolver
	store      Store
	pool       PoolRouter
	spawn      SpawnPlanFunc
	spawnWork  SpawnWorkFunc // issue #40: work-role drain callback
	lookupWS   WorkspaceLookup

	mu      sync.Mutex
	running bool
	paused  atomic.Bool
}

// SetPaused suspends/resumes auto-pull. Manual drag-to-PLAN (which goes
// through App.MoveIssueColumn, not the orchestrator) is unaffected.
func (o *Orchestrator) SetPaused(b bool) { o.paused.Store(b) }

// IsPaused returns the current pause state.
func (o *Orchestrator) IsPaused() bool { return o.paused.Load() }

// KickAutoPull triggers an auto-pull pass from outside the bus (e.g., the
// resume path on SetAutoPullPaused(false)). Async to match the bus handlers.
func (o *Orchestrator) KickAutoPull(reason string) {
	go o.autoPull(reason)
}

// New builds an Orchestrator. resolver may be nil; runRank no-ops cleanly
// when no orchestrator pool is configured (matching today's "no Ollama"
// degraded behavior).
func New(bus *eventbus.Bus, resolver LLMResolver) *Orchestrator {
	o := &Orchestrator{bus: bus, resolveLLM: resolver}
	bus.Subscribe(o.handle)
	return o
}

// SetStore wires persistence after construction. (App.startup builds the
// orchestrator before the store is available.)
func (o *Orchestrator) SetStore(s Store) { o.store = s }

// SetLLM swaps the resolver function. The resolver is consulted on every
// runRank call, so swapping pool membership via the UI takes effect at the
// next event tick.
func (o *Orchestrator) SetLLM(resolver LLMResolver) { o.resolveLLM = resolver }

// SetAutoPull wires the pool router + plan-spawn callback. When called, the
// orchestrator will auto-pull unblocked TODO items into PLAN on slot-freed
// and agent-count-changed events (§14 Phase 5).
func (o *Orchestrator) SetAutoPull(pool PoolRouter, spawn SpawnPlanFunc) {
	o.pool = pool
	o.spawn = spawn
}

// SetWorkspaceLookup wires a workspace-by-id resolver so AcquireForPlan can
// honour Workspace.SkillProfile.PreferredPlanPoolID. Optional — auto-pull
// works without it (round-robin among plan pools).
func (o *Orchestrator) SetWorkspaceLookup(f WorkspaceLookup) { o.lookupWS = f }

// SetSpawnWork wires the work-role spawn callback used by the pending-pool
// drain when draining approve_execute requests (issue #40).
func (o *Orchestrator) SetSpawnWork(fn SpawnWorkFunc) { o.spawnWork = fn }

// KickDrain triggers a drain pass outside the normal bus flow (e.g. startup).
func (o *Orchestrator) KickDrain(reason string) { go o.drainPendingPools(reason) }

// handle is the per-event router from §8.
//
// Note: LLM-driven runRank is intentionally NOT auto-fired on issue_added /
// issue_closed / issue_label_changed. The GitHub poller (#2) emits a burst
// of these on every fetch, and the LLM round-trip is too slow to chase those
// events. The user invokes "Re-rank now" manually when they want a fresh
// priority pass. autoPull (slot management) still runs on every relevant
// event because it's purely local — no LLM call.
func (o *Orchestrator) handle(e eventbus.Event) {
	switch e.Type {
	case eventbus.EvtGoalActivated:
		// One LLM rank pass, then auto-pull.
		go func() {
			_ = o.runRank("goal_activated")
			o.autoPull("goal_activated")
		}()
	case eventbus.EvtIssueClosed:
		// No LLM. Just prune closed deps locally and try to pull next.
		go func() {
			_ = o.recomputeBlocked()
			o.autoPull("issue_closed")
		}()
	case eventbus.EvtWorkerSlotFreed:
		if o.pool != nil {
			if payload, ok := e.Payload.(eventbus.WorkerSlotFreed); ok {
				o.pool.ReleaseByPool(payload.PoolID)
			}
		}
		go func() {
			// Drain persisted pending requests first (FIFO: earlier approvals
			// get the freed slot before new auto-pull candidates).
			o.drainPendingPools("worker_slot_freed")
			o.autoPull("worker_slot_freed")
		}()
	case eventbus.EvtAgentCountChanged:
		go o.autoPull("agent_count_changed")
	case eventbus.EvtPlanRejected:
		go o.autoPull("plan_rejected")
	}
}

// autoPull pulls the highest-priority unblocked TODO into PLAN until the pool
// is full. Skips workspaces with no SkillProfile mode set (impossible state,
// but defensive). Respects manual moves implicitly: a card the user just
// dragged into PLAN is no longer in TODO so won't be re-pulled.
func (o *Orchestrator) autoPull(reason string) {
	if o.paused.Load() {
		return
	}
	if o.pool == nil || o.spawn == nil || o.store == nil {
		return
	}
	// Find the active goal's scope (workspace).
	goals, err := o.store.ListGoals()
	if err != nil {
		return
	}
	var active *types.Goal
	for i := range goals {
		if goals[i].Status == types.GoalActive {
			active = &goals[i]
			break
		}
	}
	if active == nil {
		// No active goal; auto-pull is goal-driven only (§8 wait-for-active-goal).
		return
	}

	scope := active.WorkspaceID
	all, err := o.store.ListIssues(scope)
	if err != nil {
		return
	}
	candidates := goalfilter.Apply(active.IssueFilter, openIssues(all))
	// Order by priority desc, with blocked items last.
	sortByOrchestratorPriority(candidates)

	// If all plan slots are taken, enqueue the next candidate so the drain can
	// retry as soon as a slot frees (issue #40). Skip if nothing is queued.
	if o.pool.FreePlanSlots() <= 0 {
		next := pickNextUnblocked(candidates, all, o.store)
		if next != nil {
			o.enqueuePending(next, types.RolePlan, "spawn:plan")
		}
		return
	}

	for o.pool.FreePlanSlots() > 0 {
		next := pickNextUnblocked(candidates, all, o.store)
		if next == nil {
			return
		}
		// Reserve a plan slot before spawning. Strict role=plan — no fallback
		// to role=work (issue #39 rev2). PreferredPlanPoolID comes from the
		// workspace's SkillProfile when a lookup is wired.
		ws := types.Workspace{ID: next.WorkspaceID}
		if o.lookupWS != nil {
			if got, ok := o.lookupWS(next.WorkspaceID); ok {
				ws = got
			}
		}
		poolID, ok := o.pool.AcquireForPlan(ws)
		if !ok {
			// Race: slot taken between FreePlanSlots() check and AcquireForPlan.
			// Enqueue and stop — the drain will retry on next slot-freed event.
			o.enqueuePending(next, types.RolePlan, "spawn:plan")
			return
		}
		// Clear any pre-existing waiting state before moving to PLAN.
		o.clearWaitingForPool(next.WorkspaceID, next.Number)
		if err := o.store.MoveIssueColumn(next.WorkspaceID, next.Number, types.ColPlan); err != nil {
			o.pool.ReleaseByPool(poolID)
			return
		}
		if err := o.spawn(next.WorkspaceID, next.Number, poolID); err != nil {
			o.pool.ReleaseByPool(poolID)
			log.Printf("orchestrator: auto-pull spawn failed (reason=%s, issue=#%d): %v", reason, next.Number, err)
			return
		}
		// Remove the just-pulled item from the candidate list so the next loop
		// iteration picks something else.
		for i, c := range candidates {
			if c.Number == next.Number && c.WorkspaceID == next.WorkspaceID {
				candidates = append(candidates[:i], candidates[i+1:]...)
				break
			}
		}
	}
}

// pickNextUnblocked returns the first candidate currently in TODO with no
// open dependencies, or nil. `all` is the full universe used to check whether
// same-workspace dependency issues are still open. `xdepStore` is used to
// resolve cross-workspace deps via CrossDeps (nil = skip cross-dep check).
func pickNextUnblocked(candidates []types.Issue, all []types.Issue, xdepStore CrossDepLoader) *types.Issue {
	openSet := map[int]bool{}
	for _, iss := range all {
		if iss.State == "" || iss.State == "open" {
			openSet[iss.Number] = true
		}
	}
	for i := range candidates {
		c := &candidates[i]
		if c.Column != types.ColTodo {
			continue
		}
		blocked := false
		for _, dep := range c.Dependencies {
			// Only check same-workspace deps here; cross-workspace deps are
			// tracked via CrossDeps and checked below (Phase B, issue #208).
			if dep.WorkspaceID == "" && openSet[dep.Number] {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		// Cross-workspace dep check (Phase B, issue #208).
		if xdepStore != nil && len(c.CrossDeps) > 0 {
			ok, err := AllCrossDepsResolved(xdepStore, c.CrossDeps)
			if err != nil || !ok {
				continue
			}
		}
		// Fan-out external dep gate (issue #297): an issue filed by the fan-out
		// flow holds at TODO until the source PR merges (Resolved=true).
		if c.DependsOnExternal != nil && !c.DependsOnExternal.Resolved {
			continue
		}
		return c
	}
	return nil
}

func sortByOrchestratorPriority(in []types.Issue) {
	// Stable sort by priority desc, then number asc.
	for i := 1; i < len(in); i++ {
		j := i
		for j > 0 {
			prev := in[j-1]
			cur := in[j]
			if cur.Priority > prev.Priority || (cur.Priority == prev.Priority && cur.Number < prev.Number) {
				in[j-1], in[j] = in[j], in[j-1]
				j--
			} else {
				break
			}
		}
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
	var llm LLM
	if o.resolveLLM != nil {
		llm = o.resolveLLM()
	}
	if len(fresh) > 0 && llm != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		res, err := RankDeps(ctx, llm, *active, fresh)
		if err != nil {
			log.Printf("orchestrator: rank+deps failed (reason=%s): %v", reason, err)
			return err
		}
		llmResult = res
	} else if len(fresh) > 0 && llm == nil {
		log.Printf("orchestrator: no role=orchestrator pool — skipping rank pass (reason=%s)", reason)
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
		// Preserve user-set cross-workspace deps; replace same-workspace deps
		// with the LLM's output (issue #210).
		var crossWsDeps types.IssueDepList
		for _, d := range iss.Dependencies {
			if d.WorkspaceID != "" {
				crossWsDeps = append(crossWsDeps, d)
			}
		}
		var sameWsDepsForCache []int
		if e, ok := depEdges[iss.Number]; ok {
			sameDeps := make(types.IssueDepList, len(e.DependsOn))
			for i, n := range e.DependsOn {
				sameDeps[i] = types.IssueDep{Number: n}
				sameWsDepsForCache = append(sameWsDepsForCache, n)
			}
			updated.Dependencies = append(crossWsDeps, sameDeps...)
			updated.DepRationale = e.Rationale
		} else {
			updated.Dependencies = crossWsDeps
			updated.DepRationale = ""
		}
		if _, err := o.store.SaveIssue(updated); err != nil {
			return err
		}

		hash := IssueBodyHash(iss)
		entry := depCacheEntry{
			IsPrimitive: primSet[iss.Number],
			DependsOn:   sameWsDepsForCache,
			Rationale:   updated.DepRationale,
		}
		_ = o.store.DepCachePut(iss.WorkspaceID, iss.Number, goal.ID, hash, entry)
	}
	return nil
}

// recomputeBlocked re-evaluates each candidate's "blocked-by" status when an
// issue closes — prunes same-workspace closed deps. Cross-workspace deps are
// managed by the GitHub poller via WaitingForDep (issue #210).
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
		var stillOpen types.IssueDepList
		for _, dep := range iss.Dependencies {
			if dep.WorkspaceID != "" {
				// Cross-workspace deps are never pruned here — the poller owns them.
				stillOpen = append(stillOpen, dep)
				continue
			}
			if openSet[dep.Number] {
				stillOpen = append(stillOpen, dep)
			}
		}
		if len(stillOpen) != len(iss.Dependencies) {
			updated := iss
			updated.Dependencies = stillOpen
			_, _ = o.store.SaveIssue(updated)
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
