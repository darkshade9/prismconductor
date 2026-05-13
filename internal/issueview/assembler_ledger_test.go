package issueview

import (
	"fmt"
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// poolStore extends fakeStore with a controllable pool lookup.
type poolStore struct {
	fakeStore
	pools map[string]types.Pool
}

func (p *poolStore) GetPool(id string) (types.Pool, error) {
	if pool, ok := p.pools[id]; ok {
		return pool, nil
	}
	return types.Pool{}, fmt.Errorf("pool not found: %s", id)
}

func newPoolStore(sessions map[int][]types.Session, pools map[string]types.Pool) *poolStore {
	return &poolStore{
		fakeStore: fakeStore{sessions: sessions},
		pools:     pools,
	}
}

func newLedgerAssembler(st issueStore) *Assembler {
	bus := eventbus.New()
	return New(bus, st)
}

func TestBuildSessionLedger_Empty(t *testing.T) {
	st := newPoolStore(nil, nil)
	a := newLedgerAssembler(st)
	rows, err := a.BuildSessionLedger("ws1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestBuildSessionLedger_PoolResolved(t *testing.T) {
	now := time.Now()
	ended := now.Add(2 * time.Minute)
	sess := types.Session{
		ID:                 "s1",
		WorkspaceID:        "ws1",
		IssueNumber:        42,
		Mode:               types.ModeExecute,
		State:              types.StateCompleted,
		StartedAt:          now,
		EndedAt:            &ended,
		PoolID:             "pool-a",
		InputTokens:        100,
		OutputTokens:       200,
		EstimatedCostCents: 50,
	}
	pool := types.Pool{ID: "pool-a", Name: "MyPool", Provider: types.ProviderClaude, Model: "claude-opus-4-7"}
	st := newPoolStore(map[int][]types.Session{42: {sess}}, map[string]types.Pool{"pool-a": pool})
	a := newLedgerAssembler(st)

	rows, err := a.BuildSessionLedger("ws1", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.SessionID != "s1" {
		t.Errorf("session_id: got %q, want %q", r.SessionID, "s1")
	}
	if r.Role != "execute" {
		t.Errorf("role: got %q, want %q", r.Role, "execute")
	}
	if r.PoolName != "MyPool" {
		t.Errorf("pool_name: got %q, want %q", r.PoolName, "MyPool")
	}
	if r.Provider != "claude" {
		t.Errorf("provider: got %q, want %q", r.Provider, "claude")
	}
	if r.Model != "claude-opus-4-7" {
		t.Errorf("model: got %q, want %q", r.Model, "claude-opus-4-7")
	}
	if r.CostUSD != 0.5 {
		t.Errorf("cost_usd: got %f, want 0.5", r.CostUSD)
	}
	if r.InputTokens != 100 || r.OutputTokens != 200 {
		t.Errorf("tokens: got in=%d out=%d, want in=100 out=200", r.InputTokens, r.OutputTokens)
	}
	if r.Outcome != "completed" {
		t.Errorf("outcome: got %q, want %q", r.Outcome, "completed")
	}
	wantDuration := ended.Sub(now).Seconds()
	if r.DurationS != wantDuration {
		t.Errorf("duration_s: got %f, want %f", r.DurationS, wantDuration)
	}
}

func TestBuildSessionLedger_DeletedPool(t *testing.T) {
	now := time.Now()
	sess := types.Session{
		ID:          "s2",
		WorkspaceID: "ws1",
		IssueNumber: 10,
		Mode:        types.ModePlan,
		State:       types.StateCompleted,
		StartedAt:   now,
		PoolID:      "pool-gone",
	}
	// No pool registered — simulates a deleted pool.
	st := newPoolStore(map[int][]types.Session{10: {sess}}, nil)
	a := newLedgerAssembler(st)

	rows, err := a.BuildSessionLedger("ws1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.PoolName != "(pool deleted)" {
		t.Errorf("pool_name: got %q, want %q", r.PoolName, "(pool deleted)")
	}
	if r.Role != "plan" {
		t.Errorf("role: got %q, want %q", r.Role, "plan")
	}
}

func TestBuildSessionLedger_PipelineStepRole(t *testing.T) {
	now := time.Now()
	sess := types.Session{
		ID:               "s3",
		WorkspaceID:      "ws1",
		IssueNumber:      7,
		Mode:             types.ModeExecute,
		State:            types.StateCompleted,
		StartedAt:        now,
		PipelineStepName: "adversarial-review",
	}
	st := newPoolStore(map[int][]types.Session{7: {sess}}, nil)
	a := newLedgerAssembler(st)

	rows, err := a.BuildSessionLedger("ws1", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Role != "pipeline:adversarial-review" {
		t.Errorf("role: got %q, want %q", rows[0].Role, "pipeline:adversarial-review")
	}
}

func TestBuildSessionLedger_FailureCausePassthrough(t *testing.T) {
	now := time.Now()
	cause := &types.FailureCause{Kind: "blocked", Reason: "lint failed — go vet exited 1"}
	sess := types.Session{
		ID:           "s4",
		WorkspaceID:  "ws1",
		IssueNumber:  5,
		Mode:         types.ModeExecute,
		State:        types.StateBlocked,
		StartedAt:    now,
		BlockedReason: "lint failed",
		FailureCause: cause,
	}
	st := newPoolStore(map[int][]types.Session{5: {sess}}, nil)
	a := newLedgerAssembler(st)

	rows, err := a.BuildSessionLedger("ws1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Outcome != "blocked" {
		t.Errorf("outcome: got %q, want %q", r.Outcome, "blocked")
	}
	if r.FailureCause == nil || r.FailureCause.Kind != "blocked" {
		t.Errorf("failure_cause: got %+v, want kind=blocked", r.FailureCause)
	}
}

func TestBuildSessionLedger_PoolCacheHit(t *testing.T) {
	now := time.Now()
	pool := types.Pool{ID: "pool-x", Name: "CachedPool", Provider: types.ProviderOpenAI, Model: "gpt-4o"}
	sessions := []types.Session{
		{ID: "s5", WorkspaceID: "ws1", IssueNumber: 99, Mode: types.ModeExecute, State: types.StateCompleted, StartedAt: now, PoolID: "pool-x"},
		{ID: "s6", WorkspaceID: "ws1", IssueNumber: 99, Mode: types.ModeExecute, State: types.StateCompleted, StartedAt: now.Add(-time.Minute), PoolID: "pool-x"},
	}
	st := newPoolStore(map[int][]types.Session{99: sessions}, map[string]types.Pool{"pool-x": pool})
	a := newLedgerAssembler(st)

	rows, err := a.BuildSessionLedger("ws1", 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.PoolName != "CachedPool" {
			t.Errorf("pool_name: got %q, want %q", r.PoolName, "CachedPool")
		}
	}
}
