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
	pcgit "prismconductor/internal/git"
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
	UpsertPRComment(c types.PRComment) (bool, error)
	MostRecentFailedExecuteSession(workspaceID string, issueNumber int) (*types.Session, error)
	// SetIssueWaitingForDep writes or clears the waiting_for_dep field
	// on an issue JSON blob (issue #210).
	SetIssueWaitingForDep(workspaceID string, issueNumber int, dep *types.IssueDep) error
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
// falling-edge transitions across poller ticks (issue #124) AND to hold the
// last conflict-file list so we can detect when the set of conflicting
// files CHANGES while the PR remains in dirty state (e.g. main moves and
// new files start conflicting). Without that diff, a PR locked in
// `dirty` keeps the original ConflictingFiles list forever even if the
// list is now wrong — and a binary upgrade that fixes the file-detection
// algorithm (#167) doesn't take effect on already-dirty PRs.
type conflictStateCacheEntry struct {
	mergeableState string   // "clean", "dirty", "blocked", "unknown", or ""
	files          []string // last-emitted conflicting files, sorted
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

	// commentSeenCache tracks the timestamp of the most-recently-seen comment
	// per (wsID, prNumber) so the poller only emits EvtPRCommentReceived for
	// new comments (#159). Key: "wsID#prNumber". Value: time.Time.
	commentSeenCache sync.Map

	// ghUserOnce / ghUser cache the authenticated GitHub login so the poller
	// can filter out the conductor's own comments from the unread-badge signal.
	ghUserOnce sync.Once
	ghUser     string

	// lastPollAt records the time the most recent fanOut completed. The watchdog
	// goroutine uses it to detect a stalled loop (issue #247).
	lastPollMu sync.Mutex
	lastPollAt time.Time
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

	// Watchdog: restarts a stalled poll loop (issue #247).
	go p.runWatchdog(ctx)

	// Initial fetch.
	p.fanOut(ctx)

	// Delayed second fetch ~15s after startup. Reason: GitHub's
	// mergeable_state field is computed lazily — for ~10-60 seconds
	// after the latest push to a PR, the API returns "unknown" instead
	// of "clean"/"dirty"/"blocked". If the conductor opens during that
	// settling window, the conflict-detection probe sees "unknown",
	// doesn't fire EvtPRConflictsDetected, and the user has to wait
	// for the next 5min tick (or click Refresh manually) before
	// conflict badges appear. The 15s wake-up catches the common case
	// where mergeable_state has settled by then.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
			p.fanOut(ctx)
		}
	}()

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
	now := time.Now()
	p.lastPollMu.Lock()
	p.lastPollAt = now
	p.lastPollMu.Unlock()
	if p.Bus != nil {
		p.Bus.Publish(eventbus.EventType("github_poll_done"), map[string]any{
			"at": now,
		})
	}
}

// LastPollAt returns the time the most recent fanOut completed, or zero if
// no poll has completed yet. Safe for concurrent use.
func (p *Poller) LastPollAt() time.Time {
	p.lastPollMu.Lock()
	defer p.lastPollMu.Unlock()
	return p.lastPollAt
}

// runWatchdog monitors lastPollAt and pokes the poll loop if it goes quiet
// for more than 3× the configured interval. Emits EvtPollerWatchdogFired
// on the bus so the app layer can surface a toast (issue #247).
// Returns when ctx is cancelled.
func (p *Poller) runWatchdog(ctx context.Context) {
	threshold := 3 * p.Interval
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.lastPollMu.Lock()
			last := p.lastPollAt
			p.lastPollMu.Unlock()
			if last.IsZero() {
				continue // no poll has completed yet; watchdog not armed
			}
			if time.Since(last) < threshold {
				continue
			}
			log.Printf("github poller watchdog: no poll completed in %v — poking loop", time.Since(last).Round(time.Second))
			p.PokeNow()
			if p.Bus != nil {
				p.Bus.Publish(eventbus.EvtPollerWatchdogFired, map[string]any{
					"stale_seconds": int(time.Since(last).Seconds()),
				})
			}
		}
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
		// PR is still open — probe check runs for CI failure detection (#116),
		// conflict detection (#124), and comment polling (#159).
		if pr.HeadSHA != "" {
			p.probeCheckRuns(ctx, ws, iss, *iss.PRNumber, pr.HeadSHA)
		}
		p.probeConflicts(ctx, ws, iss, *iss.PRNumber, pr)
		p.probeComments(ctx, ws, iss, *iss.PRNumber)
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

	// Orphan-branch detection (#194): for issues with a failed execute session
	// that have a branch but no open PR, check GitHub for a PR targeting that
	// branch. Bounded by issues with failed execute sessions (typically 0–2).
	for _, iss := range prev {
		if iss.PRNumber != nil {
			continue // PR already attached
		}
		if iss.NeedsPRInfo != nil {
			continue // already handled by NEEDS_PR path
		}
		sess, err := p.Store.MostRecentFailedExecuteSession(ws.ID, iss.Number)
		if err != nil || sess == nil || sess.Branch == "" {
			continue
		}
		prNum, prURL, err := p.Client.FetchOpenPRsForBranch(ctx, ws, sess.Branch)
		if err != nil {
			log.Printf("orphan-pr poll %s#%d: %v", ws.ID, iss.Number, err)
			continue
		}
		if prNum != 0 {
			// PR was opened manually; auto-attach it.
			if err := p.Store.MarkPROpened(ws.ID, iss.Number, prNum, prURL); err != nil {
				log.Printf("orphan-pr: mark pr opened %s#%d: %v", ws.ID, iss.Number, err)
				continue
			}
			p.publishPR(eventbus.EvtPROpened, ws, iss)
		} else {
			// Branch pushed but no PR yet — surface recovery button.
			if p.Bus != nil {
				p.Bus.Publish(eventbus.EvtOrphanPRDetected, eventbus.OrphanPRDetected{
					WorkspaceID: ws.ID,
					IssueNumber: iss.Number,
					Branch:      sess.Branch,
				})
			}
		}
	}

	// Cross-workspace dep check (issue #210): for issues with IssueDep entries
	// referencing other workspaces, check if those deps are still open. If any
	// are, set WaitingForDep on the issue to surface a UI badge. When all
	// cross-workspace deps are satisfied, clear WaitingForDep. Bounded by the
	// number of issues with cross-workspace deps (typically few), so API impact
	// is negligible. Uses the current prev list since it includes all columns.
	p.probeDepStatus(ws, prev)

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

// probeDepStatus checks cross-workspace dependencies for issues in prev and
// sets or clears WaitingForDep on each issue accordingly (issue #210).
// Only examines issues that have at least one cross-workspace dep. Loads each
// sibling workspace's issue list at most once per tick.
func (p *Poller) probeDepStatus(ws types.Workspace, prev []types.Issue) {
	siblingCache := map[string]map[int]bool{} // wsID → open issue numbers

	for _, iss := range prev {
		var firstBlocking *types.IssueDep
		for i := range iss.Dependencies {
			dep := &iss.Dependencies[i]
			if dep.WorkspaceID == "" {
				continue // same-workspace deps managed by orchestrator
			}
			// Load and cache sibling workspace open-issue set.
			openNums, ok := siblingCache[dep.WorkspaceID]
			if !ok {
				openNums = map[int]bool{}
				siblingIssues, err := p.Store.ListIssues(dep.WorkspaceID)
				if err != nil {
					log.Printf("dep probe: list issues %s: %v", dep.WorkspaceID, err)
					siblingCache[dep.WorkspaceID] = openNums
					continue
				}
				for _, si := range siblingIssues {
					if si.State == "" || si.State == "open" {
						openNums[si.Number] = true
					}
				}
				siblingCache[dep.WorkspaceID] = openNums
			}
			if openNums[dep.Number] {
				firstBlocking = dep
				break
			}
		}

		// Determine whether current WaitingForDep matches desired state.
		switch {
		case firstBlocking != nil && (iss.WaitingForDep == nil ||
			iss.WaitingForDep.WorkspaceID != firstBlocking.WorkspaceID ||
			iss.WaitingForDep.Number != firstBlocking.Number):
			// Set or update WaitingForDep.
			dep := *firstBlocking
			if err := p.Store.SetIssueWaitingForDep(ws.ID, iss.Number, &dep); err != nil {
				log.Printf("dep probe: set waiting_for_dep %s#%d: %v", ws.ID, iss.Number, err)
			} else if p.Bus != nil {
				p.Bus.Publish(eventbus.EvtIssueLabelChanged, map[string]any{
					"workspace_id": ws.ID,
					"number":       iss.Number,
				})
			}
		case firstBlocking == nil && iss.WaitingForDep != nil:
			// Clear WaitingForDep — all cross-workspace deps satisfied.
			if err := p.Store.SetIssueWaitingForDep(ws.ID, iss.Number, nil); err != nil {
				log.Printf("dep probe: clear waiting_for_dep %s#%d: %v", ws.ID, iss.Number, err)
			} else if p.Bus != nil {
				p.Bus.Publish(eventbus.EvtIssueLabelChanged, map[string]any{
					"workspace_id": ws.ID,
					"number":       iss.Number,
				})
			}
		}
	}
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

// probeConflicts checks the PR's mergeable_state and emits
// EvtPRConflictsDetected when:
//   - falling edge: PR transitions into dirty state, OR
//   - PR is already dirty AND the precise conflicting-files list has
//     changed since the last emit (main moved, conductor was upgraded,
//     etc.)
//
// Emits EvtPRConflictsResolved on the recovery edge. Issue #124, plus
// the freshness fix that prevents a stale conflict-file list from
// being pinned forever.
func (p *Poller) probeConflicts(ctx context.Context, ws types.Workspace, iss types.Issue, prNumber int, pr *PRState) {
	if p.Bus == nil {
		return
	}
	cacheKey := ws.ID + "#" + strconv.Itoa(prNumber)
	prev, hasPrev := p.conflictStateCache.Load(cacheKey)
	var prevState string
	var prevFiles []string
	if hasPrev {
		prevEntry := prev.(conflictStateCacheEntry)
		prevState = prevEntry.mergeableState
		prevFiles = prevEntry.files
	}

	switch {
	case pr.MergeableState == "dirty":
		// Compute the current conflict-file list every poll. This makes
		// the file detection RE-RUN even when the PR has been dirty for
		// a while — needed because (a) the conflict set can change as
		// main moves, and (b) a binary upgrade that fixes the detection
		// algorithm (#167's switch from FetchPRFiles to git merge-tree)
		// only takes effect on dirty PRs if the post-upgrade poll
		// re-detects, not just on the original falling edge.
		files := p.detectConflictFiles(ctx, ws, iss, prNumber, pr)

		// Emit only when something CHANGED — falling edge into dirty,
		// or same-dirty-state with a different file list. Avoids
		// flooding the bus with no-op events on every 5min tick.
		fileSetChanged := !sameStringSet(prevFiles, files)
		fellingEdge := prevState != "dirty"
		if fellingEdge || fileSetChanged {
			p.Bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
				WorkspaceID:      ws.ID,
				IssueNumber:      iss.Number,
				PRNumber:         prNumber,
				Base:             pr.BaseBranch,
				Head:             pr.HeadRef,
				ConflictingFiles: files,
			})
		}
		p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{
			mergeableState: pr.MergeableState,
			files:          files,
		})

	case pr.MergeableState != "" && pr.MergeableState != "unknown" && prevState == "dirty":
		// Recovery edge: conflicts cleared (clean, blocked, etc.).
		p.Bus.Publish(eventbus.EvtPRConflictsResolved, eventbus.PRConflictsResolved{
			WorkspaceID: ws.ID,
			IssueNumber: iss.Number,
			PRNumber:    prNumber,
		})
		p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{
			mergeableState: pr.MergeableState,
		})

	default:
		// Not dirty, not transitioning out of dirty — just refresh the
		// state cache. No emit.
		//
		// SPECIAL CASE: pr.MergeableState == "unknown" means GitHub is
		// still computing mergeability (typical for ~10-60 seconds after
		// a push). Don't cache it as "unknown" — that pins the state
		// across ticks and forces every subsequent probe to skip the
		// dirty branch above. Instead, leave the cache untouched so
		// the NEXT poll re-evaluates from a clean slate. The 15s
		// startup wake-up + the regular 5min ticker both eventually
		// catch the settled state.
		if pr.MergeableState == "unknown" {
			return
		}
		p.conflictStateCache.Store(cacheKey, conflictStateCacheEntry{
			mergeableState: pr.MergeableState,
		})
	}
}

// detectConflictFiles runs the precise `git merge-tree` 3-way merge
// simulation in the workspace's local repo and returns the conflicting
// paths sorted alphabetically. Falls back to the GitHub PR-files API
// (full PR changeset, less precise) when the local-git path errors —
// e.g. git too old, or the workspace doesn't have a RepoPath.
func (p *Poller) detectConflictFiles(ctx context.Context, ws types.Workspace, iss types.Issue, prNumber int, pr *PRState) []string {
	var files []string
	if ws.RepoPath != "" {
		_, _ = pcgit.RunInDir(ws.RepoPath, "git", "fetch", "origin",
			pr.BaseBranch+":refs/remotes/origin/"+pr.BaseBranch,
			pr.HeadRef+":refs/remotes/origin/"+pr.HeadRef)
		conflictFiles, mtErr := pcgit.MergeConflictFiles(
			ws.RepoPath,
			"origin/"+pr.BaseBranch,
			"origin/"+pr.HeadRef,
		)
		if mtErr == nil {
			files = conflictFiles
		} else {
			log.Printf("merge-tree %s#%d (pr %d): %v — falling back to PR file list",
				ws.ID, iss.Number, prNumber, mtErr)
		}
	}
	if files == nil {
		fallback, err := p.Client.FetchPRFiles(ctx, ws, prNumber)
		if err != nil {
			log.Printf("pr files %s#%d (pr %d): %v", ws.ID, iss.Number, prNumber, err)
		}
		files = fallback
	}
	sort.Strings(files)
	return files
}

// sameStringSet reports whether two []string slices contain the same
// elements regardless of order. Both inputs must already be sorted for
// the simple slice-compare; detectConflictFiles sorts before storing
// in the cache so this is the cheap path.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// probeComments fetches conversation and review comments for a REVIEW-column
// PR, upserts each into the store, and emits EvtPRCommentReceived for any
// newly-seen comment not authored by the conductor itself or known bots (#159).
func (p *Poller) probeComments(ctx context.Context, ws types.Workspace, iss types.Issue, prNumber int) {
	if p.Bus == nil || p.Client == nil || p.Store == nil {
		return
	}
	// Resolve the conductor's own gh user once per process lifetime.
	p.ghUserOnce.Do(func() {
		if login, err := p.Client.GetAuthenticatedUser(ctx); err == nil {
			p.ghUser = login
		}
	})

	cacheKey := ws.ID + "#" + strconv.Itoa(prNumber)
	var since time.Time
	if v, ok := p.commentSeenCache.Load(cacheKey); ok {
		since = v.(time.Time)
	}

	conv, err := p.Client.FetchIssueComments(ctx, ws, iss.Number, since)
	if err != nil {
		log.Printf("issue comments %s#%d: %v", ws.ID, iss.Number, err)
	}
	rev, err := p.Client.FetchPRReviewComments(ctx, ws, prNumber, since)
	if err != nil {
		log.Printf("review comments %s#%d (pr %d): %v", ws.ID, iss.Number, prNumber, err)
	}

	all := append(conv, rev...)
	var newest time.Time
	for _, cm := range all {
		if cm.CreatedAt.After(newest) {
			newest = cm.CreatedAt
		}
		isNew, err := p.Store.UpsertPRComment(cm)
		if err != nil {
			log.Printf("upsert pr comment %s#%d id %d: %v", ws.ID, iss.Number, cm.CommentID, err)
			continue
		}
		if !isNew {
			continue
		}
		if isBotOrSelf(cm.Author, p.ghUser) {
			continue
		}
		p.Bus.Publish(eventbus.EvtPRCommentReceived, eventbus.PRCommentReceived{
			WorkspaceID: ws.ID,
			IssueNumber: iss.Number,
			PRNumber:    prNumber,
			CommentID:   cm.CommentID,
			Author:      cm.Author,
		})
	}
	if !newest.IsZero() {
		p.commentSeenCache.Store(cacheKey, newest)
	}
}

// knownBotSuffixes are GitHub login suffixes that identify bot accounts.
var knownBotSuffixes = []string{"[bot]"}

// knownBotLogins are well-known bot account exact logins.
var knownBotLogins = map[string]bool{
	"dependabot":     true,
	"renovate":       true,
	"github-actions": true,
}

// isBotOrSelf returns true when the author login should be silenced.
func isBotOrSelf(author, selfLogin string) bool {
	if selfLogin != "" && author == selfLogin {
		return true
	}
	if knownBotLogins[author] {
		return true
	}
	for _, suffix := range knownBotSuffixes {
		if len(author) > len(suffix) && author[len(author)-len(suffix):] == suffix {
			return true
		}
	}
	return false
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
