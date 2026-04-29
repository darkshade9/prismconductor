package session

import (
	"errors"
	"testing"

	"prismconductor/internal/types"
)

func TestMapTerminalStatePausedSurvivesCleanExit(t *testing.T) {
	got := mapTerminalState(types.StatePausedForQuestion, nil)
	if got != types.StatePausedForQuestion {
		t.Errorf("paused + clean exit → %s, want paused_for_question", got)
	}
}

func TestMapTerminalStatePausedSurvivesNonZeroExit(t *testing.T) {
	got := mapTerminalState(types.StatePausedForQuestion, errors.New("exit 1"))
	if got != types.StatePausedForQuestion {
		t.Errorf("paused + non-zero exit → %s, want paused_for_question", got)
	}
}

func TestMapTerminalStateRunningCleanExitCompletes(t *testing.T) {
	got := mapTerminalState(types.StateRunning, nil)
	if got != types.StateCompleted {
		t.Errorf("running + clean exit → %s, want completed", got)
	}
}

func TestMapTerminalStateBlockedCleanExitFails(t *testing.T) {
	got := mapTerminalState(types.StateBlocked, nil)
	if got != types.StateFailed {
		t.Errorf("blocked + clean exit → %s, want failed", got)
	}
}

func TestMapTerminalStateWaitingCleanExitFails(t *testing.T) {
	got := mapTerminalState(types.StateWaitingForInput, nil)
	if got != types.StateFailed {
		t.Errorf("waiting + clean exit → %s, want failed", got)
	}
}

func TestMapTerminalStateNonZeroExitFailsByDefault(t *testing.T) {
	got := mapTerminalState(types.StateRunning, errors.New("exit 137"))
	if got != types.StateFailed {
		t.Errorf("running + non-zero exit → %s, want failed", got)
	}
}

// fakePersister records UpdateSessionState calls without touching SQLite.
type fakePersister struct {
	saves    []types.Session
	stateUpd []string
	pendUpd  map[string]string
}

func (p *fakePersister) SaveSession(s *types.Session, _ string) error {
	cp := *s
	p.saves = append(p.saves, cp)
	return nil
}
func (p *fakePersister) UpdateSessionState(id string, state types.SessionState) error {
	p.stateUpd = append(p.stateUpd, id+":"+string(state))
	return nil
}
func (p *fakePersister) UpdateSessionPendingQuestion(id, qid string) error {
	if p.pendUpd == nil {
		p.pendUpd = map[string]string{}
	}
	p.pendUpd[id] = qid
	return nil
}

func TestMatchPatternsQuestionPending(t *testing.T) {
	const qid = "11111111-2222-3333-4444-555555555555"
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{ID: "sess-1", State: types.StateRunning}}
	m.matchPatterns(rs, "@asst QUESTION_PENDING: "+qid)
	if rs.sess.State != types.StatePausedForQuestion {
		t.Fatalf("state after QUESTION_PENDING = %s, want paused_for_question", rs.sess.State)
	}
	if rs.sess.PendingQuestionID != qid {
		t.Errorf("pending question id = %q, want %q", rs.sess.PendingQuestionID, qid)
	}
	if len(store.saves) == 0 {
		t.Errorf("expected SaveSession to be called once on pause-transition")
	}
}

func TestMatchPatternsQuestionPendingIgnoresSecondInFlight(t *testing.T) {
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{
		ID:                "sess-1",
		State:             types.StatePausedForQuestion,
		PendingQuestionID: "first-id",
	}}
	m.matchPatterns(rs, "QUESTION_PENDING: second-id")
	if rs.sess.PendingQuestionID != "first-id" {
		t.Errorf("expected first-id to stick, got %q", rs.sess.PendingQuestionID)
	}
}

func TestMatchPatternsQuestionPendingEmptyIDIgnored(t *testing.T) {
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{ID: "sess-1", State: types.StateRunning}}
	m.matchPatterns(rs, "QUESTION_PENDING: ")
	if rs.sess.State != types.StateRunning {
		t.Errorf("empty id should be ignored, state = %s", rs.sess.State)
	}
	if rs.sess.PendingQuestionID != "" {
		t.Errorf("empty id should not set PendingQuestionID, got %q", rs.sess.PendingQuestionID)
	}
}
