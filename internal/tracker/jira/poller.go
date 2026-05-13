package jira

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/tracker"
	"prismconductor/internal/types"
)

// Store is the slice of store.Store the Jira poller needs.
type Store interface {
	ListIssues(workspaceID string) ([]types.Issue, error)
	SaveIssue(iss types.Issue) (bool, error)
	MarkIssueClosed(workspaceID string, number int) error
}

// WorkspaceSource lists workspaces for the poller to iterate.
type WorkspaceSource interface {
	List() []types.Workspace
}

// Poller fans out per-workspace Jira fetches every Interval. Only workspaces
// with TrackerKind == "jira" are processed. The poller emits the same
// EvtIssueAdded / EvtIssueClosed / EvtIssueLabelChanged events as the GitHub
// poller so the rest of the conductor is tracker-agnostic.
type Poller struct {
	Bus      *eventbus.Bus
	Store    Store
	Source   WorkspaceSource
	Interval time.Duration

	mu      sync.Mutex
	running bool
	pokeCh  chan struct{}
}

// NewPoller constructs a Poller with sensible defaults.
func NewPoller(bus *eventbus.Bus, store Store, src WorkspaceSource, interval time.Duration) *Poller {
	if interval == 0 {
		interval = 5 * time.Minute
	}
	return &Poller{
		Bus:      bus,
		Store:    store,
		Source:   src,
		Interval: interval,
		pokeCh:   make(chan struct{}, 1),
	}
}

// PokeNow asks the loop to fan out immediately. Safe to call concurrently.
func (p *Poller) PokeNow() {
	select {
	case p.pokeCh <- struct{}{}:
	default:
	}
}

// FetchNow runs an immediate fetch for a single Jira workspace.
func (p *Poller) FetchNow(ctx context.Context, ws types.Workspace) {
	var fetchErr error
	if ws.TrackerKind != string(tracker.KindJira) {
		return
	}
	fetchErr = p.pollOne(ctx, ws)
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

// Run loops until ctx is canceled.
func (p *Poller) Run(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

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
	workspaces := p.Source.List()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, ws := range workspaces {
		if !ws.Enabled {
			continue
		}
		if ws.TrackerKind != string(tracker.KindJira) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ws types.Workspace) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := p.pollOne(ctx, ws); err != nil {
				log.Printf("jira poll %s: %v", ws.ID, err)
			}
		}(ws)
	}
	wg.Wait()
}

func (p *Poller) pollOne(ctx context.Context, ws types.Workspace) error {
	client, err := NewClientForWorkspace(ws)
	if err != nil {
		return err
	}

	fresh, err := client.ListIssues(ctx, ws)
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
		unarchived, err := p.Store.SaveIssue(fr)
		if err != nil {
			log.Printf("jira save issue %s#%d: %v", ws.ID, fr.Number, err)
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

	for _, old := range prev {
		if _, stillOpen := freshByNum[old.Number]; stillOpen {
			continue
		}
		if old.Column == types.ColDone {
			continue
		}
		if err := p.Store.MarkIssueClosed(ws.ID, old.Number); err != nil {
			log.Printf("jira close issue %s#%d: %v", ws.ID, old.Number, err)
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
