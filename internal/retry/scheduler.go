// Package retry implements the automatic-retry scheduler for transient execute
// session failures (issue #221, Phase 1: in-memory queue, lost on restart).
package retry

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"prismconductor/internal/cardstate"
	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// FireFunc is called by the Scheduler when a retry is due. The implementation
// in app.go re-spawns the execute session using the existing approved plan.
type FireFunc func(workspaceID string, issueNumber int, attempt int, priorSessionID string)

// PendingRetry is the scheduler's in-memory record for one queued retry.
type PendingRetry struct {
	WorkspaceID    string
	IssueNumber    int
	Attempt        int
	MaxAttempts    int
	NotBefore      time.Time
	LastCause      *types.FailureCause
	PriorSessionID string
}

// Scheduler maintains an in-memory queue of pending retries and fires them
// after their NotBefore time elapses. Safe for concurrent use.
type Scheduler struct {
	mu      sync.Mutex
	pending map[string]*PendingRetry // key: "wsID#issueNum"
	bus     *eventbus.Bus
	fire    FireFunc
	stop    chan struct{}
}

// NewScheduler creates a Scheduler and starts the background tick goroutine.
// Call Stop() to shut it down cleanly.
func NewScheduler(bus *eventbus.Bus, fire FireFunc) *Scheduler {
	s := &Scheduler{
		pending: make(map[string]*PendingRetry),
		bus:     bus,
		fire:    fire,
		stop:    make(chan struct{}),
	}
	go s.run()
	return s
}

// Schedule enqueues an automatic retry for a failed execute session. Returns
// false (no-op) when the cause is not retryable, the cap is reached, or a
// retry is already pending for this issue.
func (s *Scheduler) Schedule(ws types.Workspace, sess types.Session, cause *types.FailureCause) bool {
	if !cardstate.IsRetryable(cause) {
		return false
	}
	policy := effectivePolicy(ws)
	if policy.MaxAttempts == 0 {
		return false
	}

	nextAttempt := sess.RetryAttempt + 1
	maxAllowed := policy.MaxAttempts
	if maxAllowed > types.GlobalRetryMaxAttempts {
		maxAllowed = types.GlobalRetryMaxAttempts
	}
	if nextAttempt > maxAllowed {
		s.bus.Publish(eventbus.EvtRetryExhausted, eventbus.RetryExhausted{
			WorkspaceID: ws.ID,
			IssueNumber: sess.IssueNumber,
			Attempts:    sess.RetryAttempt,
		})
		return false
	}

	delay := backoffWithJitter(nextAttempt, policy)
	if delay > types.GlobalRetryMaxWindow {
		s.bus.Publish(eventbus.EvtRetryExhausted, eventbus.RetryExhausted{
			WorkspaceID: ws.ID,
			IssueNumber: sess.IssueNumber,
			Attempts:    sess.RetryAttempt,
		})
		return false
	}

	notBefore := time.Now().Add(delay)
	key := issueKey(ws.ID, sess.IssueNumber)

	s.mu.Lock()
	if _, exists := s.pending[key]; exists {
		s.mu.Unlock()
		return false // already queued
	}
	p := &PendingRetry{
		WorkspaceID:    ws.ID,
		IssueNumber:    sess.IssueNumber,
		Attempt:        nextAttempt,
		MaxAttempts:    maxAllowed,
		NotBefore:      notBefore,
		LastCause:      cause,
		PriorSessionID: sess.ID,
	}
	s.pending[key] = p
	s.mu.Unlock()

	log.Printf("retry: scheduled attempt %d/%d for #%d in %s (cause: %s)",
		nextAttempt, maxAllowed, sess.IssueNumber, delay.Round(time.Second), cause.Kind)

	s.bus.Publish(eventbus.EvtRetryScheduled, eventbus.RetryScheduled{
		WorkspaceID: ws.ID,
		IssueNumber: sess.IssueNumber,
		Attempt:     nextAttempt,
		MaxAttempts: maxAllowed,
		NotBefore:   notBefore.Unix(),
	})
	return true
}

// Cancel removes a pending retry for the given issue without firing it.
// Returns true if a retry was present and cancelled.
func (s *Scheduler) Cancel(workspaceID string, issueNumber int) bool {
	key := issueKey(workspaceID, issueNumber)
	s.mu.Lock()
	_, existed := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if existed {
		s.bus.Publish(eventbus.EvtRetryCancelled, eventbus.RetryCancelled{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
			Reason:      "user",
		})
	}
	return existed
}

// FireNow fires the pending retry immediately, ignoring the NotBefore time.
// Returns true if a retry was present and dispatched.
func (s *Scheduler) FireNow(workspaceID string, issueNumber int) bool {
	key := issueKey(workspaceID, issueNumber)
	s.mu.Lock()
	p, exists := s.pending[key]
	if exists {
		delete(s.pending, key)
	}
	s.mu.Unlock()
	if !exists {
		return false
	}
	s.dispatch(p)
	return true
}

// List returns a snapshot of the pending retries for a workspace.
func (s *Scheduler) List(workspaceID string) []PendingRetry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PendingRetry
	for _, p := range s.pending {
		if p.WorkspaceID == workspaceID {
			out = append(out, *p)
		}
	}
	return out
}

// PendingRetryInfoFor returns a *cardstate.PendingRetryInfo for a specific
// issue, or nil if no retry is queued for it.
func (s *Scheduler) PendingRetryInfoFor(workspaceID string, issueNumber int) *cardstate.PendingRetryInfo {
	key := issueKey(workspaceID, issueNumber)
	s.mu.Lock()
	p := s.pending[key]
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	return &cardstate.PendingRetryInfo{
		Attempt:     p.Attempt,
		MaxAttempts: p.MaxAttempts,
		NotBefore:   p.NotBefore.Unix(),
		LastCause:   p.LastCause,
	}
}

// Stop terminates the background tick goroutine.
func (s *Scheduler) Stop() {
	close(s.stop)
}

// run is the background goroutine that checks for due retries every second.
func (s *Scheduler) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	s.mu.Lock()
	var due []*PendingRetry
	for key, p := range s.pending {
		if !now.Before(p.NotBefore) {
			due = append(due, p)
			delete(s.pending, key)
		}
	}
	s.mu.Unlock()
	for _, p := range due {
		s.dispatch(p)
	}
}

func (s *Scheduler) dispatch(p *PendingRetry) {
	log.Printf("retry: firing attempt %d/%d for #%d", p.Attempt, p.MaxAttempts, p.IssueNumber)
	s.bus.Publish(eventbus.EvtRetryFired, eventbus.RetryFired{
		WorkspaceID: p.WorkspaceID,
		IssueNumber: p.IssueNumber,
		Attempt:     p.Attempt,
	})
	go s.fire(p.WorkspaceID, p.IssueNumber, p.Attempt, p.PriorSessionID)
}

// backoffWithJitter computes the delay for attempt n: base * 2^(n-1), capped
// at MaxBackoff, then applies symmetric ±JitterPct% jitter.
func backoffWithJitter(attempt int, policy types.RetryPolicy) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	d := policy.BaseBackoff * (1 << uint(shift))
	if d > policy.MaxBackoff || d < 0 {
		d = policy.MaxBackoff
	}
	if policy.JitterPct > 0 {
		jitterRange := float64(d) * float64(policy.JitterPct) / 100.0
		jitter := (rand.Float64()*2 - 1) * jitterRange //nolint:gosec
		d = time.Duration(float64(d) + jitter)
		if d < 0 {
			d = 0
		}
	}
	return d
}

func effectivePolicy(ws types.Workspace) types.RetryPolicy {
	if ws.RetryPolicy != nil {
		return *ws.RetryPolicy
	}
	return types.DefaultRetryPolicy
}

func issueKey(workspaceID string, issueNumber int) string {
	return fmt.Sprintf("%s#%d", workspaceID, issueNumber)
}
