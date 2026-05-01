package issueview

import (
	"encoding/json"
	"log"
	"strconv"
	"sync"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// checkFailEntry is the in-memory state the Assembler keeps per issue for
// CI check-run failures. Updated by handleEvent (bus) and SetSelfHealAttempts
// (called from app.go after spawning a self-heal worker).
type checkFailEntry struct {
	info eventbus.PRChecksFailed
	// attempts and cap are set by SetSelfHealAttempts; zero values mean
	// no attempt has been recorded yet.
	attempts   int
	cap        int
	maxReached bool
}

// conflictEntry is the in-memory state the Assembler keeps per issue for
// PR merge-conflict state. Set on EvtPRConflictsDetected, cleared on
// EvtPRConflictsResolved / EvtPRMerged / EvtPRClosedUnmerged (#124).
type conflictEntry struct {
	info eventbus.PRConflictsDetected
}

// issueStore is the subset of *store.Store that the Assembler needs.
type issueStore interface {
	LoadIssue(workspaceID string, number int) (types.Issue, error)
	ListIssues(workspaceID string) ([]types.Issue, error)
	LatestPlan(workspaceID string, issueNumber int) (*types.Plan, error)
	ListSessionsForIssue(workspaceID string, issueNumber int) ([]types.Session, error)
	GetPool(id string) (types.Pool, error)
}

// Assembler assembles canonical IssueView structs and re-emits
// EvtIssueViewUpdated whenever any contributing source changes.
type Assembler struct {
	store issueStore
	bus   *eventbus.Bus

	mu            sync.Mutex
	cache         map[string]string           // "wsID#num" → last-emitted JSON
	checkFails    map[string]*checkFailEntry  // "wsID#num" → latest check failure
	conflictFails map[string]*conflictEntry   // "wsID#num" → latest conflict state
}

// New wires the Assembler to the bus. Call once at startup after the store is ready.
func New(bus *eventbus.Bus, st issueStore) *Assembler {
	a := &Assembler{
		store:         st,
		bus:           bus,
		cache:         make(map[string]string),
		checkFails:    make(map[string]*checkFailEntry),
		conflictFails: make(map[string]*conflictEntry),
	}
	bus.Subscribe(a.handleEvent)
	return a
}

// issueRelevantEvents is the set of bus events that can change an IssueView.
var issueRelevantEvents = map[eventbus.EventType]bool{
	eventbus.EvtSessionStateChanged: true,
	eventbus.EvtPlanReady:           true,
	eventbus.EvtPlanApproved:        true,
	eventbus.EvtPlanRejected:        true,
	eventbus.EvtPlanRevised:         true,
	eventbus.EvtPROpened:            true,
	eventbus.EvtPRMerged:            true,
	eventbus.EvtPRClosedUnmerged:    true,
	eventbus.EvtCardMovedManually:   true,
	eventbus.EvtIssueAdded:          true,
	eventbus.EvtIssueClosed:         true,
	eventbus.EvtIssueLabelChanged:   true,
	eventbus.EvtPendingPoolEnqueued: true,
	eventbus.EvtPendingPoolDequeued: true,
	eventbus.EvtPRChecksFailed:      true,
	eventbus.EvtPRChecksRecovered:   true,
	eventbus.EvtPRConflictsDetected: true,
	eventbus.EvtPRConflictsResolved: true,
}

func (a *Assembler) handleEvent(e eventbus.Event) {
	if !issueRelevantEvents[e.Type] {
		return
	}
	wsID, issueNum := extractIssueKey(e)
	if wsID == "" || issueNum == 0 {
		return
	}
	// Update in-memory check-failure state before reassembling so Assemble()
	// reads the latest value.
	switch e.Type {
	case eventbus.EvtPRChecksFailed:
		if p, ok := e.Payload.(eventbus.PRChecksFailed); ok {
			key := p.WorkspaceID + "#" + strconv.Itoa(p.IssueNumber)
			a.mu.Lock()
			existing := a.checkFails[key]
			if existing == nil || existing.info.HeadSHA != p.HeadSHA {
				// New SHA or first failure — reset attempt counter.
				a.checkFails[key] = &checkFailEntry{info: p}
			} else {
				existing.info = p
			}
			a.mu.Unlock()
		}
	case eventbus.EvtPRChecksRecovered, eventbus.EvtPRMerged, eventbus.EvtPRClosedUnmerged:
		if wsID != "" && issueNum != 0 {
			key := wsID + "#" + strconv.Itoa(issueNum)
			a.mu.Lock()
			delete(a.checkFails, key)
			delete(a.conflictFails, key)
			a.mu.Unlock()
		}
	case eventbus.EvtPRConflictsDetected:
		if p, ok := e.Payload.(eventbus.PRConflictsDetected); ok {
			key := p.WorkspaceID + "#" + strconv.Itoa(p.IssueNumber)
			a.mu.Lock()
			a.conflictFails[key] = &conflictEntry{info: p}
			a.mu.Unlock()
		}
	case eventbus.EvtPRConflictsResolved:
		if wsID != "" && issueNum != 0 {
			key := wsID + "#" + strconv.Itoa(issueNum)
			a.mu.Lock()
			delete(a.conflictFails, key)
			a.mu.Unlock()
		}
	}
	a.reassembleAndEmit(wsID, issueNum)
}

func (a *Assembler) reassembleAndEmit(wsID string, issueNum int) {
	view, err := a.Assemble(wsID, issueNum)
	if err != nil {
		log.Printf("issueview: assemble %s#%d: %v", wsID, issueNum, err)
		return
	}
	b, _ := json.Marshal(view)
	key := wsID + "#" + strconv.Itoa(issueNum)

	a.mu.Lock()
	changed := a.cache[key] != string(b)
	if changed {
		a.cache[key] = string(b)
	}
	a.mu.Unlock()

	if changed {
		a.bus.Publish(eventbus.EvtIssueViewUpdated, view)
	}
}

// Assemble builds a fresh IssueView from the canonical sources in the store.
func (a *Assembler) Assemble(workspaceID string, issueNumber int) (IssueView, error) {
	iss, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return IssueView{}, err
	}

	plan, _ := a.store.LatestPlan(workspaceID, issueNumber)
	sessions, _ := a.store.ListSessionsForIssue(workspaceID, issueNumber)

	active, paused, lastFail, lastSession := selectSessions(iss, sessions)

	var poolBadge *PoolBadge
	// Resolve pool badge from most-recent session that has a pool_id.
	for i := range sessions {
		pid := sessions[i].PoolID
		if pid == "" {
			continue
		}
		if p, err := a.store.GetPool(pid); err == nil {
			poolBadge = &PoolBadge{
				PoolID:   p.ID,
				Name:     p.Name,
				Provider: string(p.Provider),
			}
		}
		// Sessions are ordered newest-first; first hit is most-recent.
		break
	}

	key := workspaceID + "#" + strconv.Itoa(issueNumber)
	a.mu.Lock()
	cfEntry := a.checkFails[key]
	conflEntry := a.conflictFails[key]
	a.mu.Unlock()

	var testsFailingInfo *TestsFailingInfo
	if cfEntry != nil {
		testsFailingInfo = &TestsFailingInfo{
			FailingJobs:         cfEntry.info.FailingJobs,
			FailingCheckRunURLs: cfEntry.info.FailingCheckRunURLs,
			HeadSHA:             cfEntry.info.HeadSHA,
			SelfHealAttempts:    cfEntry.attempts,
			AttemptCap:          cfEntry.cap,
			MaxAttemptsReached:  cfEntry.maxReached,
		}
	}

	var conflictsInfo *ConflictsInfo
	if conflEntry != nil {
		conflictsInfo = &ConflictsInfo{
			PRNumber:         conflEntry.info.PRNumber,
			Base:             conflEntry.info.Base,
			Head:             conflEntry.info.Head,
			ConflictingFiles: conflEntry.info.ConflictingFiles,
		}
	}

	return IssueView{
		Issue:            iss,
		LatestPlan:       plan,
		ActiveSession:    active,
		PausedSession:    paused,
		LastFailure:      lastFail,
		LastSession:      lastSession,
		PoolBadge:        poolBadge,
		DerivedColumn:    derivedColumn(iss, plan, active),
		TestsFailingInfo: testsFailingInfo,
		ConflictsInfo:    conflictsInfo,
	}, nil
}

// SetSelfHealAttempts updates the in-memory self-heal attempt counter for the
// given issue and triggers a re-emit of its IssueView. Called from app.go
// after spawning or attempting a self-heal worker (#116).
func (a *Assembler) SetSelfHealAttempts(workspaceID string, issueNumber, attempts, cap int, maxReached bool) {
	key := workspaceID + "#" + strconv.Itoa(issueNumber)
	a.mu.Lock()
	entry := a.checkFails[key]
	if entry != nil {
		entry.attempts = attempts
		entry.cap = cap
		entry.maxReached = maxReached
	}
	a.mu.Unlock()
	if entry != nil {
		a.reassembleAndEmit(workspaceID, issueNumber)
	}
}

// ListForWorkspace assembles an IssueView for every non-archived issue in the workspace.
func (a *Assembler) ListForWorkspace(workspaceID string) ([]IssueView, error) {
	issues, err := a.store.ListIssues(workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]IssueView, 0, len(issues))
	for _, iss := range issues {
		v, err := a.Assemble(workspaceID, iss.Number)
		if err != nil {
			log.Printf("issueview: list %s#%d: %v", workspaceID, iss.Number, err)
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// selectSessions walks the session list (newest-first) and returns the
// canonical (active, paused, lastFailure, lastSession) tuple. Mirrors the
// selection logic from Card.tsx so backend and frontend derive identical state.
// lastSession is the most recent terminal session (any outcome) for use by the
// CostChip token-breakdown tooltip (issue #101).
func selectSessions(iss types.Issue, sessions []types.Session) (active, paused, lastFail, lastSession *types.Session) {
	var mostRecentCompleted *types.Session

	for i := range sessions {
		cp := sessions[i] // deep copy: each returned pointer owns its struct
		m := &cp
		notAck := m.AcknowledgedAt == nil || *m.AcknowledgedAt == 0

		if m.State == types.StatePausedForQuestion {
			if paused == nil || m.StartedAt.After(paused.StartedAt) {
				paused = m
			}
			continue
		}

		// `state=blocked` is a TERMINAL state: the worker emitted the BLOCKED
		// sentinel and exited (or got killed). Routing it as `active` was the
		// historical behavior from when "blocked" briefly meant "worker still
		// alive, just emitted BLOCKED, mid-shutdown" — but in practice every
		// `blocked` row in the DB is a stale failure. Treating them as active
		// causes DONE cards (whose old plan/execute sessions ended in
		// `blocked`) to glow purple/blue forever as if work were ongoing.
		// Route blocked → lastFail only. The suppression rules below then
		// hide it from REVIEW/DONE cards naturally.
		if m.State == types.StateRunning || m.State == types.StateWaitingForInput {
			if active == nil {
				active = m
			}
			continue
		}

		if (m.State == types.StateFailed || m.State == types.StateBlocked) &&
			m.BlockedReason != "" && notAck {
			if lastFail == nil || m.StartedAt.After(lastFail.StartedAt) {
				lastFail = m
			}
		}

		// Blocked-without-reason: still terminal, just no captured reason.
		// Don't surface as active (would falsely glow); skip silently.
		if m.State == types.StateBlocked {
			if lastSession == nil || m.StartedAt.After(lastSession.StartedAt) {
				lastSession = m
			}
			continue
		}

		if m.State == types.StateCompleted {
			if mostRecentCompleted == nil || m.StartedAt.After(mostRecentCompleted.StartedAt) {
				mostRecentCompleted = m
			}
		}

		// Track the most recent terminal session (completed, failed, blocked)
		// for the cost-chip token breakdown tooltip.
		if m.State == types.StateCompleted || m.State == types.StateFailed {
			if lastSession == nil || m.StartedAt.After(lastSession.StartedAt) {
				lastSession = m
			}
		}
	}

	// Suppress lastFail when card is in REVIEW or DONE (work demonstrably succeeded).
	if lastFail != nil && (iss.Column == types.ColReview || iss.Column == types.ColDone) {
		lastFail = nil
	}
	// Suppress lastFail when a more recent successful session followed the failure.
	if lastFail != nil && mostRecentCompleted != nil &&
		mostRecentCompleted.StartedAt.After(lastFail.StartedAt) {
		lastFail = nil
	}

	return active, paused, lastFail, lastSession
}

// derivedColumn computes the canonical column from issue state and session state.
//
// Precedence (mirrors plan §98 DerivedColumn rules):
//  1. PR open/merged → trust the persisted column (already "review" or "done")
//  2. Active session running → "in_progress"
//  3. Plan ready (not yet approved) → "plan"
//  4. Fall back to Issue.Column or "todo"
func derivedColumn(iss types.Issue, plan *types.Plan, active *types.Session) types.BoardColumn {
	if iss.PRNumber != nil {
		if iss.Column == "" {
			return types.ColReview
		}
		return iss.Column
	}
	if active != nil {
		return types.ColInProgress
	}
	if plan != nil && plan.ReadyToExecute && plan.ApprovedAt == nil {
		return types.ColPlan
	}
	if iss.Column == "" {
		return types.ColTodo
	}
	return iss.Column
}

// extractIssueKey extracts (workspaceID, issueNumber) from the event payload.
// Handles payload shapes used across the codebase:
//   - eventbus.SessionStateChanged (#98)
//   - eventbus.PendingPoolChange (#40)
//   - eventbus.PRChecksFailed / eventbus.PRChecksRecovered (#116)
//   - map[string]any with "workspace_id" + "issue_number" or "number" key
func extractIssueKey(e eventbus.Event) (wsID string, issueNum int) {
	switch p := e.Payload.(type) {
	case eventbus.SessionStateChanged:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PendingPoolChange:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PRChecksFailed:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PRChecksRecovered:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PRConflictsDetected:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PRConflictsResolved:
		return p.WorkspaceID, p.IssueNumber
	case map[string]any:
		wsID, _ = p["workspace_id"].(string)
		// Some callers use "issue_number", others use "number".
		if n, ok := p["issue_number"].(float64); ok {
			issueNum = int(n)
		} else if n, ok := p["issue_number"].(int); ok {
			issueNum = n
		} else if n, ok := p["number"].(float64); ok {
			issueNum = int(n)
		} else if n, ok := p["number"].(int); ok {
			issueNum = n
		}
		return wsID, issueNum
	}
	return "", 0
}
