package github

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// Store is the slice of *store.Store the poller needs.
type Store interface {
	ListIssues(workspaceID string) ([]types.Issue, error)
	SaveIssue(iss types.Issue) error
	MarkIssueClosed(workspaceID string, number int) error
	MarkPRMerged(workspaceID string, number int) error
	MarkPRClosedUnmerged(workspaceID string, number int) error
	SaveLabels(workspaceID string, labels []types.Label) error
}

// WorkspaceSource lets the poller pick up newly-added workspaces between ticks.
type WorkspaceSource interface {
	List() []types.Workspace
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
		if err := p.Store.SaveIssue(fr); err != nil {
			log.Printf("save issue %s#%d: %v", ws.ID, fr.Number, err)
			continue
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
		case pr.ClosedAt != nil:
			if err := p.Store.MarkPRClosedUnmerged(ws.ID, iss.Number); err != nil {
				log.Printf("mark pr closed-unmerged %s#%d: %v", ws.ID, iss.Number, err)
				continue
			}
			p.publishPR(eventbus.EvtPRClosedUnmerged, ws, iss)
		}
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
