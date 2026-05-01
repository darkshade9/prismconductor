package issueview

import (
	"encoding/json"
	"log"
	"strconv"
	"sync"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

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

	mu    sync.Mutex
	cache map[string]string // "wsID#num" → last-emitted JSON
}

// New wires the Assembler to the bus. Call once at startup after the store is ready.
func New(bus *eventbus.Bus, st issueStore) *Assembler {
	a := &Assembler{
		store: st,
		bus:   bus,
		cache: make(map[string]string),
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
}

func (a *Assembler) handleEvent(e eventbus.Event) {
	if !issueRelevantEvents[e.Type] {
		return
	}
	wsID, issueNum := extractIssueKey(e)
	if wsID == "" || issueNum == 0 {
		return
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

	active, paused, lastFail := selectSessions(iss, sessions)

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

	return IssueView{
		Issue:         iss,
		LatestPlan:    plan,
		ActiveSession: active,
		PausedSession: paused,
		LastFailure:   lastFail,
		PoolBadge:     poolBadge,
		DerivedColumn: derivedColumn(iss, plan, active),
	}, nil
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
// canonical (active, paused, lastFailure) tuple. Mirrors the selection logic
// from Card.tsx so backend and frontend derive identical state.
func selectSessions(iss types.Issue, sessions []types.Session) (active, paused, lastFail *types.Session) {
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

		if m.State == types.StateRunning || m.State == types.StateWaitingForInput ||
			(m.State == types.StateBlocked && notAck) {
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
			continue
		}

		if m.State == types.StateCompleted {
			if mostRecentCompleted == nil || m.StartedAt.After(mostRecentCompleted.StartedAt) {
				mostRecentCompleted = m
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

	return active, paused, lastFail
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
// Handles the three payload shapes used across the codebase:
//   - eventbus.SessionStateChanged (new, issue #98)
//   - eventbus.PendingPoolChange (#40)
//   - map[string]any with "workspace_id" + "issue_number" or "number" key
func extractIssueKey(e eventbus.Event) (wsID string, issueNum int) {
	switch p := e.Payload.(type) {
	case eventbus.SessionStateChanged:
		return p.WorkspaceID, p.IssueNumber
	case eventbus.PendingPoolChange:
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
