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

	// Anything previously open and now missing from the open list: closed.
	for _, old := range prev {
		if _, stillOpen := freshByNum[old.Number]; stillOpen {
			continue
		}
		if old.State != "open" {
			continue
		}
		old.State = "closed"
		if old.Column != types.ColDone {
			old.Column = types.ColDone
		}
		if err := p.Store.SaveIssue(old); err != nil {
			log.Printf("close issue %s#%d: %v", ws.ID, old.Number, err)
			continue
		}
		p.publish(eventbus.EvtIssueClosed, ws, old)
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
