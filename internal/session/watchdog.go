package session

import (
	"context"
	"log"
	"time"

	"prismconductor/internal/types"
)

// WatchdogTimeout is the maximum time a running session may go without writing
// any output to its transcript before the watchdog force-kills it. Fifteen
// minutes is long enough to tolerate slow providers while catching deadlocked
// tool dispatches and sessions whose HTTP timeout did not fire.
const WatchdogTimeout = 15 * time.Minute

// watchdogInterval is how often the watchdog scans active sessions.
const watchdogInterval = time.Minute

// StartWatchdog launches a background goroutine that scans running sessions
// once per minute and kills any that have not written a transcript line for
// WatchdogTimeout. The goroutine exits when ctx is cancelled.
//
// Call this from app startup (e.g. after Manager.Configure) to enable the
// belt-and-suspenders liveness guarantee.
func (m *Manager) StartWatchdog(ctx context.Context) {
	go func() {
		t := time.NewTicker(watchdogInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.runWatchdogScan()
			}
		}
	}()
}

// runWatchdogScan checks all running sessions and kills any that have been
// silent for longer than WatchdogTimeout. Separated from StartWatchdog so it
// can be called directly in tests.
func (m *Manager) runWatchdogScan() {
	now := time.Now()
	m.mu.RLock()
	type staleEntry struct {
		id   string
		last time.Time
	}
	var stale []staleEntry
	for id, rs := range m.sessions {
		if rs == nil || rs.sess == nil {
			continue
		}
		if rs.sess.State != types.StateRunning {
			continue
		}
		rs.actMu.Lock()
		last := rs.lastLineAt
		rs.actMu.Unlock()
		if last.IsZero() {
			continue
		}
		if now.Sub(last) > WatchdogTimeout {
			stale = append(stale, staleEntry{id: id, last: last})
		}
	}
	m.mu.RUnlock()

	for _, e := range stale {
		log.Printf("watchdog: killing stale session %s (last output %v ago)", e.id, now.Sub(e.last).Round(time.Second))
		if err := m.Kill(e.id); err != nil {
			log.Printf("watchdog: Kill %s: %v", e.id, err)
		}
	}
}
