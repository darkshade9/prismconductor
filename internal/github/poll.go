package github

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// Store is the slice of *store.Store the poller needs.
type Store interface {
	ListIssues(workspaceID string) ([]types.Issue, error)
	SaveIssue(iss types.Issue) (bool, error)
	MarkIssueClosed(workspaceID string, number int) error
	MarkPROpened(workspaceID string, number int, prNumber int, prURL string) error
	MarkPRMerged(workspaceID string, number int) error
	MarkPRClosedUnmerged(workspaceID string, number int) error
	SaveLabels(workspaceID string, labels []types.Label) error
}

// WorkspaceSource lets the poller pick up newly-added workspaces between ticks.
type WorkspaceSource interface {
	List() []types.Workspace
}

// checkStateCacheEntry is stored per (wsID, prNumber) to detect failing-edge
// transitions across poller ticks (issue #116).
type checkStateCacheEntry struct {
	headSHA   string
	anyFailed bool
}

// conflictStateCacheEntry is stored per (wsID, prNumber) to detect conflict
// falling-edge transitions across poller ticks (issue #124).
type conflictStateCacheEntry struct {
	mergeableState string // "clean", "dirty", "blocked", "unknown", or ""
}

// Poller fans out per-workspace fetches every Interval. Each tick:
//   - Pulls open issues from GitHub for every enabled workspace (parallel,
//     bounded by maxConcurrency).
//   - Diffs against the local mirror.
//   - Saves new + changed rows.
//   - Marks anything previously open and now missing as closed (and moves to DONE).
//   - Publishes EvtIssueAdded / EvtIssueClosed / EvtIssueLabelChanged on the bus.
//
// Run() blocks; call from a goroutine.
type Poller struct {
	Bus        *eventbus.Bus
	Client     *Client
	Store      Store
	Source     WorkspaceSource
	Interval   time.Duration

	maxConcurrency int

	mu      sync.Mutex
	running bool
	pokeCh  chan struct{}

	// checkStateCache tracks the previous aggregate check-run state per PR so
	// the poller can detect failing-edge transitions (issue #116).
	// Key: "wsID#prNumber". Value: checkStateCacheEntry.
	checkStateCache sync.Map

	// conflictStateCache tracks the previous mergeable_state per PR to detect
	// conflict falling-edge transitions (issue #124).
	// Key: "wsID#prNumber". Value: conflictStateCacheEntry.
	conflictStateCache sync.Map
}

// NewPoller constructs a Poller with sensible defaults.
func NewPoller(bus *eventbus.Bus, client *Client, store Store, src WorkspaceSource, interval time.Duration) *Poller {
	if interval == 0 {
		interval = 5 * time.Minute
	}
	return &Poller{
		Bus:            bus,
		Client:         client,
		Store:          store,
		Source:         src,
		Interval:       interval,
		maxConcurrency: 4,
		pokeCh:         make(chan struct{}, 1),
	}
}

// FetchNow runs an immediate fetch for a single workspace (used after AddWorkspace).
// It blocks until the fetch completes and then publishes EvtWorkspaceFetchComplete.
// Safe to call from a goroutine.
func (p *Poller) FetchNow(ctx context.Context, ws types.Workspace) {
	var fetchErr error
	if p.Client == nil {
		fetchErr = fmt.Errorf("github client unavailable")
	} else {
		fetchErr = p.pollOne(ctx, ws)
	}
	var issueCount int
	if fetchErr == nil {
		if issues, e := p.Store.ListIssues(ws.ID); e == nil {
			issueCount = len(issues)
		}
	}
	if p.Bus == nil {
		return
	}
	payload := eventbus.WorkspaceFetchComplete{
		WorkspaceID:   ws.ID,
		WorkspaceName: ws.DisplayName,
		Success:       fetchErr == nil,
		IssueCount:    issueCount,
	}
	if fetchErr != nil {
		payload.Error = fetchErr.Error()
	}
	p.Bus.Publish(eventbus.EvtWorkspaceFetchComplete, payload)
}

// PokeNow asks the poll loop to fan out immediately (e.g. user clicked Refresh).
// Safe to call concurrently; if a poke is already pending it's dropped.
func (p *Poller) PokeNow() {
	select {
	case p.pokeCh <- struct{}{}:
	default:
	}
}

// Run loops until ctx is canceled.
func (p *Poller) Run(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	// Initial fetch.
	p.fanOut(ctx)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.fanOut(ctx)
		case <-p.pokeCh:
			p.fanOut(ctx)
			t.Reset(p.Interval)
		}
	}
}

func (p *Poller) fanOut(ctx context.Context) {
	if p.Client == nil {
		return
	}
	workspaces := p.Source.List()
	sem := make(chan struct{}, p.maxConcurrency)
	var wg sync.WaitGroup
	for _, ws := range workspaces {
		if !ws.Enabled {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ws types.Workspace) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := p.pollOne(ctx, ws); err != nil {
				log.Printf("github poll %s: %v", ws.ID, err)
			}
		}(ws)
	}
	wg.Wait()
	if p.Bus != nil {
		p.Bus.Publish(eventbus.EventType("github_poll_done"), map[string]any{
			"at": time.Now(),
		})
	}
}

func (p *Poller) pollOne(ctx context.Context, ws types.Workspace) error {
	fresh, err := p.Client.FetchOpenIssues(ctx, ws)
	if err != nil {
		return err
	}
	prev, err := p.Store.ListIssues(ws.ID)
	if err != nil {
		return err
	}

	prevByNum := make(map[int]types.Issue, len(prev))
	for _, i := range prev {
		prevByNum[i.Number] = i
	}
	freshByNum := make(map[int]types.Issue, len(fresh))
	for _, i := range fresh {
		freshByNum[i.Number] = i
	}

	for _, fr := range fresh {
		old, hadOld := prevByNum[fr.Number]
		// SaveIssue preserves existing column_name + manual_order — see
		// internal/store/issues.go. So a poll won't trample a user's drag.
		// unarchived=true when an archived row was just cleared because the
		// fresh row's state == "open" (#34 q2 auto-unarchive on reopen).
		unarchived, err := p.Store.SaveIssue(fr)
		if err != nil {
			log.Printf("save issue %s#%d: %v", ws.ID, fr.Number, err)
			continue
		}
		if unarchived {
			p.publish(eventbus.EvtIssuesArchived, ws, fr)
		}
		if !hadOld {
			p.publish(eventbus.EvtIssueAdded, ws, fr)
			continue
		}
		if !sameLabels(old.Labels, fr.Labels) {
			p.publish(eventbus.EvtIssueLabelChanged, ws, fr)
		}
	}

	// Anything previously known and now missing from the open list: closed.
	// Use MarkIssueClosed (not SaveIssue) — SaveIssue's column-preservation
	// would otherwise stick the closed issue in whatever column it was in.
	// Gate on column (not state) so issues left in a half-migrated state by
	// an earlier buggy poll get re-routed to DONE on the next tick.
	for _, old := range prev {
		if _, stillOpen := freshByNum[old.Number]; stillOpen {
			continue
		}
		if old.Column == types.ColDone {
			continue
		}
		if err := p.Store.MarkIssueClosed(ws.ID, old.Number); err != nil {
			log.Printf("close issue %s#%d: %v", ws.ID, old.Number, err)
			continue
		}
		p.publish(eventbus.EvtIssueClosed, ws, old)
	}

	// PR-state probe (#33). For every REVIEW-column issue with a PR, ask
	// GitHub whether it merged or closed-without-merge. Bounded by the size
	// of the REVIEW column (typically ≤10), so well below the 5000/h
	// authenticated rate limit. Walk `prev` (not `fresh`) since fresh only
	// contains open issues — a merged PR's issue is already closed and
	// missing from the open list.
	for _, iss := range prev {
		if iss.PRNumber == nil {
			continue
		}
		if iss.Column != types.ColReview {
			continue
		}
		pr, err := p.Client.FetchPRState(ctx, ws, *iss.PRNumber)
		if err != nil {
			log.Printf("pr state %s#%d: %v", ws.ID, iss.Number, err)
			continue
		}
		switch {
		case pr.MergedAt != nil:
			if err := p.Store.MarkPRMerged(ws.ID, iss.Number); err != nil {
				log.Printf("mark pr merged %s#%d: %v", ws.ID, iss.Number, err)
				continue
			}
			p.publishPR(eventbus.EvtPRMerged, ws, iss)
			// Clear caches on merge — no longer relevant.
			cacheKey := ws.ID + "#" + strconv.Itoa(*iss.PRNumber)
			p.checkStateCache.Delete(cacheKey)
			p.conflictStateCache.Delete(cacheKey)
			continue
		case pr.ClosedAt != nil:
			if err := p.Store.MarkPRClosedUnmerged(ws.ID, iss.Number); err != nil {
				log.Printf("mark pr closed-unmerged %s#%d: %v", ws.ID, iss.Number, err)
				continue
			}
			p.publishPR(eventbus.EvtPRClosedUnmerged, ws, iss)
			cacheKey := ws.ID + "#" + strconv.Itoa(*iss.PRNumber)
			p.checkStateCache.Delete(cacheKey)
			p.conflictStateCache.Delete(cacheKey)
			continue
		}
		// PR is still open — probe check runs for CI failure detection (#116)
		// and conflict detection (#124).
		if pr.HeadSHA != "" {
			p.probeCheckRuns(ctx, ws, iss, *iss.PRNumber, pr.HeadSHA)
		}
		p.probeConflicts(ctx, ws, iss, *iss.PRNumber, pr)
	}

	// NEEDS_PR PR detection (#157): for every issue with NeedsPRInfo but no PR
	// yet, ask GitHub whether any open PR targets the conductor's branch. If one
	// is found, attach it via MarkPROpened (same path as the sentinel handler).
	// Bounded by the number of NEEDS_PR cards (typically 0–2), so the extra
	// API calls are negligible relative to the per-PR state probes above.
	for _, iss := range prev {
		if iss.NeedsPRInfo == nil {
			continue
		}
		if iss.PRNumber != nil {
			continue // PR already attached; MarkPROpened cleared NeedsPRInfo on next save
		}
		prNum, prURL, err := p.Client.FetchOpenPRsForBranch(ctx, ws, iss.NeedsPRInfo.Branch)
		if err != nil {
			log.Printf("needs_pr poll %s#%d: %v", ws.ID, iss.Number, err)
			continue
		}
		if prNum == 0 {
			continue // no PR yet
		}
		if err := p.Store.MarkPROpened(ws.ID, iss.Number, prNum, prURL); err != nil {
			log.Printf("needs_pr: mark pr opened %s#%d: %v", ws.ID, iss.Number, err)
			continue
		}
		p.publishPR(eventbus.EvtPROpened, ws, iss)
	}

	// Piggy-back label fetch on the same tick. Failures are logged and don't
	// abort the issue cycle (the cache stays stale until the next tick).
	if labels, err := p.Client.ListLabels(ctx, ws); err != nil {
		log.Printf("github labels %s: %v", ws.ID, err)
	} else if err := p.Store.SaveLabels(ws.ID, labels); err != nil {
		log.Printf("save labels %s: %v", ws.ID, err)
	} else if p.Bus != nil {
		p.Bus.Publish(eventbus.EvtLabelsUpdated, map[string]any{"workspace_id": ws.ID})
	}
	return nil
}

func (p *Poller) publish(t eventbus.EventType, ws types.Workspace, iss types.Issue) {
	if p.Bus == nil {
		return
	}
	p.Bus.Publish(t, map[string]any{
		"workspace_id": ws.ID,
		"number":       iss.Number,
		"title":        iss.Title,
	})
}

// publishPR emits a PR-related event with the same payload shape as
// app.go's handlePROpened so frontend subscribers can read pr_number / pr_url
// uniformly across pr_opened / pr_merged / pr_closed_unmerged.
func (p *Poller) publishPR(t eventbus.EventType, ws types.Workspace, iss types.Issue) {
	if p.Bus == nil {
		return
	}
	payload := map[string]any{
		"workspace_id": ws.ID,
		"issue_number": iss.Number,
		"title":        iss.Title,
		"pr_url":       iss.PRURL,
	}
	if iss.PRNumber != nil {
		payload["pr_number"] = *iss.PRNumber
	}
	p.Bus.Publish(t, payload)
}

// probeCheckRuns fetches GitHub Actions check runs for the PR's HEAD SHA,
// computes an aggregate, and emits EvtPRChecksFailed on a failing-edge
// transition (passing→failing or new-SHA-with-failures). Emits
// EvtPRChecksRecovered when a previously-failing PR passes. Issue #116.
func (p *Poller) probeCheckRuns(ctx context.Context, ws types.Workspace, iss types.Issue, prNumber int, headSHA string) {
	if p.Bus == nil || p.Client == nil {
		return
	}
	agg, err := p.Client.FetchChecksAggregate(ctx, ws, headSHA)
	if err != nil {
		log.Printf("check runs %s#%d (pr %d): %v", ws.ID, iss.Number, prNumber, err)
		return
	}

	cacheKey := ws.ID + "#" + strconv.Itoa(prNumber)
	prev, hasPrev := p.checkStateCache.Load(cacheKey)

	// Compute whether this is a new-SHA (force-push) or a state transition.
	isNewSHA := !hasPrev || prev.(checkStateCacheEntry).headSHA != headSHA
	wasFailing := hasPrev && prev.(checkStateCacheEntry).anyFailed

	// Update cache.
	p.checkStateCache.Store(cacheKey, checkStateCacheEntry{
		headSHA:   headSHA,
		anyFailed: agg.AnyFailed,
	})

	switch {
	case agg.AnyFailed && (isNewSHA || !wasFailing):
		// Failing edge: emit the failure event.
		jobs := make([]string, len(agg.FailingJobs))
		urls := make([]string, len(agg.FailingJobs))
		runIDs := make([]int64, len(agg.FailingJobs))
		for i, j := range agg.FailingJobs {
			jobs[i] = j.Name
			urls[i] = j.URL
			runIDs[i] = j.RunID
		}
		p.Bus.Publish(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
			WorkspaceID:         ws.ID,
			IssueNumber:         iss.Number,
			PRNumber:            prNumber,
			HeadSHA:             headSHA,
			FailingJobs:         jobs,
			FailingCheckRunURLs: urls,
			RunIDs:              runIDs,
		})
	case !agg.AnyFailed && wasFailing && !isNewSHA:
		// Recovery edge: all checks now pass on the same SHA.
		p.Bus.Publish(eventbus.EvtPRChecksRecovered, eventbus.PRChecksRecovered{
			WorkspaceID: ws.ID,
			IssueNumber: iss.Number,
			PRNumber:    prNumber,
			HeadSHA:     headSHA,
		})
	}
}

// probeConflicts checks the PR's mergeable_state and emits EvtPRConflictsDetected
// on a falling-edge transition to "dirty" (not-dirty → dirty). Emits
// EvtPRConflictsResolved when the state transitions back to a non-dirty value.
// Issue #124.
func (p *Poller) probeConflicts(ctx context.Context, ws types.Workspace, iss types.Issue, prNumber int, pr *PRState) {
	if p.Bus == nil {
		return
	}
	cacheKey := ws.ID + "#" + strconv.Itoa(prNumber)
	prev, hasPrev := p.conflictStateCache.Load(cacheKey)
	prevState := ""
	if hasPrev {
		prevState = prev.(conflictStateCacheEntry).mergeableState
	}

	p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{mergeableState: pr.MergeableState})

	switch {
	case pr.MergeableState == "dirty" && prevState != "dirty":
		// Falling edge: PR became conflicted. Fetch changed files for context.
		files, err := p.Client.FetchPRFiles(ctx, ws, prNumber)
		if err != nil {
			log.Printf("pr files %s#%d (pr %d): %v", ws.ID, iss.Number, prNumber, err)
		}
		p.Bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
			WorkspaceID:      ws.ID,
			IssueNumber:      iss.Number,
			PRNumber:         prNumber,
			Base:             pr.BaseBranch,
			Head:             pr.HeadRef,
			ConflictingFiles: files,
		})
	case pr.MergeableState != "dirty" && pr.MergeableState != "" && pr.MergeableState != "unknown" && prevState == "dirty":
		// Recovery edge: conflicts cleared (clean, blocked, etc.).
		p.Bus.Publish(eventbus.EvtPRConflictsResolved, eventbus.PRConflictsResolved{
			WorkspaceID: ws.ID,
			IssueNumber: iss.Number,
			PRNumber:    prNumber,
		})
	}
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
