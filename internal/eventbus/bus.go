// Package eventbus is the in-process pub/sub from PRISMCONDUCTOR_PLAN.md §7.
package eventbus

import (
	"sync"
	"time"
)

type EventType string

const (
	EvtGoalActivated     EventType = "goal_activated"
	EvtGoalUpdated       EventType = "goal_updated"
	EvtIssueAdded        EventType = "issue_added"
	EvtIssueClosed       EventType = "issue_closed"
	EvtIssueLabelChanged EventType = "issue_label_changed"
	EvtPlanReady         EventType = "plan_ready"
	EvtPROpened          EventType = "pr_opened"
	EvtPRMerged          EventType = "pr_merged"
	EvtPRClosedUnmerged  EventType = "pr_closed_unmerged"
	EvtPlanApproved      EventType = "plan_approved"
	EvtPlanRejected      EventType = "plan_rejected"
	EvtPlanRevised       EventType = "plan_revised"
	EvtWorkerSlotFreed   EventType = "worker_slot_freed"
	EvtWorkerBlocked     EventType = "worker_blocked"
	EvtCardMovedManually EventType = "card_moved_manually"
	EvtAgentCountChanged EventType = "agent_count_changed"
	EvtLabelsUpdated     EventType = "labels_updated"
	EvtAutoPullPausedChanged EventType = "auto_pull_paused_changed"
	EvtIssuesArchived        EventType = "issues_archived"
	// Issue #40: pending pool queue events.
	EvtPendingPoolEnqueued EventType = "pending_pool_enqueued"
	EvtPendingPoolDequeued EventType = "pending_pool_dequeued"

	// Issue #98: canonical IssueView assembler events.
	EvtIssueViewUpdated    EventType = "issue_view_updated"
	EvtSessionStateChanged EventType = "session_state_changed"
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	Payload   any
}

// WorkerSlotFreed is the payload shape for EvtWorkerSlotFreed (issue #27). The
// orchestrator uses PoolID to release the slot on the right pool registry
// entry; the prior bare-string-sessionID payload is replaced.
type WorkerSlotFreed struct {
	SessionID string `json:"session_id"`
	PoolID    string `json:"pool_id"`
}

// PendingPoolChange is the payload for EvtPendingPoolEnqueued / Dequeued (#40).
type PendingPoolChange struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Role        string `json:"role"`
}

// SessionStateChanged is the payload for EvtSessionStateChanged (#98). Published
// from handleSessionStateChange so the IssueView assembler can react to every
// session transition without subscribing to Wails-only events.
type SessionStateChanged struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	SessionID   string `json:"session_id"`
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
}

func New() *Bus { return &Bus{} }

func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	b.handlers = append(b.handlers, h)
	b.mu.Unlock()
}

func (b *Bus) Publish(t EventType, payload any) {
	evt := Event{Type: t, Timestamp: time.Now(), Payload: payload}
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, h := range hs {
		go h(evt)
	}
}
