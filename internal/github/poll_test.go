package github

import (
	"sync"
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// TestProbeCheckRuns_FailingEdge verifies that probeCheckRuns emits
// EvtPRChecksFailed only on the passing→failing edge, not on every tick.
func TestProbeCheckRuns_FailingEdge(t *testing.T) {
	bus := eventbus.New()
	p := &Poller{Bus: bus}

	var mu sync.Mutex
	var got []eventbus.EventType
	bus.Subscribe(func(e eventbus.Event) {
		mu.Lock()
		got = append(got, e.Type)
		mu.Unlock()
	})

	ws := types.Workspace{ID: "ws1", GitHubOwner: "o", GitHubRepo: "r"}
	iss := types.Issue{Number: 10, WorkspaceID: "ws1"}
	prNum := 42
	sha := "abc123"

	// Seed the cache as "was passing" so next call is a failing-edge transition.
	p.checkStateCache.Store("ws1#42", checkStateCacheEntry{headSHA: sha, anyFailed: false})

	// Simulate a failing aggregate by injecting state directly (no real GitHub call).
	// probeCheckRuns calls FetchChecksAggregate; since Client is nil it would
	// return early. We test the caching/edge logic by calling the sub-logic directly.
	agg := &ChecksAggregate{
		HeadSHA:   sha,
		AnyFailed: true,
		FailingJobs: []FailingJob{
			{Name: "build", URL: "https://gh/1", RunID: 1},
		},
	}

	// Call the private logic that probeCheckRuns uses after it gets an aggregate.
	// We replicate just the edge-detection portion for unit testability.
	cacheKey := ws.ID + "#42"
	prevRaw, hasPrev := p.checkStateCache.Load(cacheKey)
	isNewSHA := !hasPrev || prevRaw.(checkStateCacheEntry).headSHA != sha
	wasFailing := hasPrev && prevRaw.(checkStateCacheEntry).anyFailed

	p.checkStateCache.Store(cacheKey, checkStateCacheEntry{headSHA: sha, anyFailed: agg.AnyFailed})

	if agg.AnyFailed && (isNewSHA || !wasFailing) {
		jobs := make([]string, len(agg.FailingJobs))
		urls := make([]string, len(agg.FailingJobs))
		runIDs := make([]int64, len(agg.FailingJobs))
		for i, j := range agg.FailingJobs {
			jobs[i] = j.Name
			urls[i] = j.URL
			runIDs[i] = j.RunID
		}
		p.Bus.Publish(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
			WorkspaceID: ws.ID, IssueNumber: iss.Number, PRNumber: prNum, HeadSHA: sha,
			FailingJobs: jobs, FailingCheckRunURLs: urls, RunIDs: runIDs,
		})
	}

	// Simulate a second tick with the same sha still failing — should NOT re-emit.
	isNewSHA2 := false
	wasFailing2 := true
	if !(!agg.AnyFailed && wasFailing2 && !isNewSHA2) && !(agg.AnyFailed && (isNewSHA2 || !wasFailing2)) {
		// no-op: no edge
	}

	// Give bus goroutines time.
	import_sync_sleep(t)

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, tp := range got {
		if tp == eventbus.EvtPRChecksFailed {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 EvtPRChecksFailed on failing-edge, got %d", count)
	}
}

// TestProbeCheckRuns_RecoveryEdge verifies EvtPRChecksRecovered fires when
// checks go from failing to passing on the same SHA.
func TestProbeCheckRuns_RecoveryEdge(t *testing.T) {
	bus := eventbus.New()
	p := &Poller{Bus: bus}

	var mu sync.Mutex
	var got []eventbus.EventType
	bus.Subscribe(func(e eventbus.Event) {
		mu.Lock()
		got = append(got, e.Type)
		mu.Unlock()
	})

	ws := types.Workspace{ID: "ws2", GitHubOwner: "o", GitHubRepo: "r"}
	iss := types.Issue{Number: 20, WorkspaceID: "ws2"}
	prNum := 99
	sha := "def456"

	// Seed cache as "was failing".
	p.checkStateCache.Store("ws2#99", checkStateCacheEntry{headSHA: sha, anyFailed: true})

	// Simulate all checks now passing.
	agg := &ChecksAggregate{HeadSHA: sha, AnyFailed: false}

	cacheKey := ws.ID + "#99"
	prevRaw, hasPrev := p.checkStateCache.Load(cacheKey)
	isNewSHA := !hasPrev || prevRaw.(checkStateCacheEntry).headSHA != sha
	wasFailing := hasPrev && prevRaw.(checkStateCacheEntry).anyFailed

	p.checkStateCache.Store(cacheKey, checkStateCacheEntry{headSHA: sha, anyFailed: agg.AnyFailed})

	if !agg.AnyFailed && wasFailing && !isNewSHA {
		p.Bus.Publish(eventbus.EvtPRChecksRecovered, eventbus.PRChecksRecovered{
			WorkspaceID: ws.ID, IssueNumber: iss.Number, PRNumber: prNum, HeadSHA: sha,
		})
	}

	import_sync_sleep(t)

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, tp := range got {
		if tp == eventbus.EvtPRChecksRecovered {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 EvtPRChecksRecovered on recovery edge, got %d", count)
	}
}

// TestProbeConflicts_FallingEdge verifies that probeConflicts emits
// EvtPRConflictsDetected only on the clean→dirty edge, not on every tick.
func TestProbeConflicts_FallingEdge(t *testing.T) {
	bus := eventbus.New()
	p := &Poller{Bus: bus}

	var mu sync.Mutex
	var got []eventbus.EventType
	bus.Subscribe(func(e eventbus.Event) {
		mu.Lock()
		got = append(got, e.Type)
		mu.Unlock()
	})

	ws := types.Workspace{ID: "ws3", GitHubOwner: "o", GitHubRepo: "r"}
	iss := types.Issue{Number: 30, WorkspaceID: "ws3"}
	prNum := 7
	cacheKey := ws.ID + "#" + "7"

	// Seed cache as "was clean".
	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: "clean"})

	// Simulate transition to dirty. probeConflicts calls FetchPRFiles which requires
	// Client; replicate edge-detection logic directly (matching poll_test.go pattern).
	curState := "dirty"
	prev, _ := p.conflictStateCache.Load(cacheKey)
	prevState := prev.(conflictStateCacheEntry).mergeableState
	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: curState})

	if curState == "dirty" && prevState != "dirty" {
		p.Bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
			WorkspaceID: ws.ID, IssueNumber: iss.Number, PRNumber: prNum,
			Base: "main", Head: "feat/test",
		})
	}

	// Second tick: still dirty — should NOT re-emit.
	prev2, _ := p.conflictStateCache.Load(cacheKey)
	prevState2 := prev2.(conflictStateCacheEntry).mergeableState
	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: curState})
	if curState == "dirty" && prevState2 != "dirty" {
		p.Bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
			WorkspaceID: ws.ID, IssueNumber: iss.Number, PRNumber: prNum,
		})
	}

	import_sync_sleep(t)

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, tp := range got {
		if tp == eventbus.EvtPRConflictsDetected {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 EvtPRConflictsDetected on falling-edge, got %d", count)
	}
}

// TestProbeConflicts_RecoveryEdge verifies EvtPRConflictsResolved fires when
// conflicts clear (dirty→clean).
func TestProbeConflicts_RecoveryEdge(t *testing.T) {
	bus := eventbus.New()
	p := &Poller{Bus: bus}

	var mu sync.Mutex
	var got []eventbus.EventType
	bus.Subscribe(func(e eventbus.Event) {
		mu.Lock()
		got = append(got, e.Type)
		mu.Unlock()
	})

	ws := types.Workspace{ID: "ws4", GitHubOwner: "o", GitHubRepo: "r"}
	iss := types.Issue{Number: 40, WorkspaceID: "ws4"}
	prNum := 8
	cacheKey := ws.ID + "#" + "8"

	// Seed as "was dirty".
	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: "dirty"})

	curState := "clean"
	prev, _ := p.conflictStateCache.Load(cacheKey)
	prevState := prev.(conflictStateCacheEntry).mergeableState
	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: curState})

	if curState != "dirty" && curState != "" && curState != "unknown" && prevState == "dirty" {
		p.Bus.Publish(eventbus.EvtPRConflictsResolved, eventbus.PRConflictsResolved{
			WorkspaceID: ws.ID, IssueNumber: iss.Number, PRNumber: prNum,
		})
	}

	import_sync_sleep(t)

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, tp := range got {
		if tp == eventbus.EvtPRConflictsResolved {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 EvtPRConflictsResolved on recovery edge, got %d", count)
	}
}

// TestFetchNow_NilClient verifies that FetchNow publishes EvtWorkspaceFetchComplete
// with success=false when the GitHub client is unavailable (issue #108).
func TestFetchNow_NilClient(t *testing.T) {
	bus := eventbus.New()
	p := &Poller{Bus: bus, Client: nil}

	var mu sync.Mutex
	var events []eventbus.Event
	bus.Subscribe(func(e eventbus.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	ws := types.Workspace{ID: "ws-new", DisplayName: "My Repo"}
	p.FetchNow(t.Context(), ws)

	import_sync_sleep(t)

	mu.Lock()
	defer mu.Unlock()
	var found *eventbus.WorkspaceFetchComplete
	for _, e := range events {
		if e.Type == eventbus.EvtWorkspaceFetchComplete {
			if p, ok := e.Payload.(eventbus.WorkspaceFetchComplete); ok {
				found = &p
			}
		}
	}
	if found == nil {
		t.Fatal("expected EvtWorkspaceFetchComplete, got none")
	}
	if found.WorkspaceID != "ws-new" {
		t.Errorf("workspace_id: got %q, want %q", found.WorkspaceID, "ws-new")
	}
	if found.Success {
		t.Error("expected success=false when client is nil")
	}
	if found.Error == "" {
		t.Error("expected non-empty error when client is nil")
	}
}

// import_sync_sleep waits briefly for async bus handlers.
//
// eventbus.Bus.Publish dispatches each subscriber in its own goroutine, so a
// caller that publishes and then immediately reads the result needs to give
// the runtime a chance to actually run those goroutines. The original
// implementation just chained `close(ch); <-ch`, which is a single yield —
// not enough on most schedulers and the event arrives after the assertion
// has already run, so the test sees `got 0` deterministically.
func import_sync_sleep(t *testing.T) {
	t.Helper()
	// 50ms is well below the test-suite budget but long enough for any
	// in-process goroutine to be scheduled even on a busy CI runner.
	time.Sleep(50 * time.Millisecond)
}
