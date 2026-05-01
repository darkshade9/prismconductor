// Package diagnose implements the "Tell me about X?" decision tree for
// PrismConductor issues (issue #100). All logic is rules-based and
// deterministic — no LLM calls, no side effects.
package diagnose

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"prismconductor/internal/types"
)

// IssueDiagnosis is the structured result of a "Tell me about X?" lookup.
type IssueDiagnosis struct {
	Summary       string   `json:"summary"`
	Detail        []string `json:"detail"`
	Suggestion    string   `json:"suggestion"`
	SeverityLevel string   `json:"severity_level"` // "ok" | "warn" | "stuck"
}

// Snapshot is all system state needed to produce a diagnosis.
// Assembled by the App layer; this package contains only pure decision logic.
type Snapshot struct {
	Issue          types.Issue
	Sessions       []types.Session // all persisted sessions, newest first
	LiveSession    *types.Session  // in-process session from mgr.Snapshot(), nil if none
	PoolActive     int             // active slot count for the pool the last session used
	PoolCapacity   int             // capacity of that pool
	PoolName       string          // display name of that pool
	PendingPool    bool            // pending pool-queue entry exists for this issue
	WorktreeOnDisk bool            // worktree directory exists on disk
	AnswerPending  bool            // answer file for a paused mid-run question is on disk
	PlanOnDisk     bool            // plan file exists on disk
	PlanRevision   int             // revision of the plan on disk (0 if none)
}

// Run evaluates the Snapshot and returns a diagnosis.
func Run(s Snapshot) IssueDiagnosis {
	dx := &diagnoser{s: s}
	return dx.run()
}

// PidAlive returns true if the given pid is still running (POSIX signal-0 check).
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// --- internal ---

type diagnoser struct {
	s Snapshot
	d []string
}

func (dx *diagnoser) add(line string) { dx.d = append(dx.d, line) }

func (dx *diagnoser) result(summary, suggestion, severity string) IssueDiagnosis {
	d := make([]string, len(dx.d))
	copy(d, dx.d)
	return IssueDiagnosis{
		Summary:       summary,
		Detail:        d,
		Suggestion:    suggestion,
		SeverityLevel: severity,
	}
}

func (dx *diagnoser) run() IssueDiagnosis {
	s := dx.s

	// 1–3. Live in-manager session covers active process, slot, and
	// column-vs-state mismatches all at once.
	if s.LiveSession != nil {
		return dx.diagnoseLive()
	}

	// 4. Pending pool queue.
	if s.PendingPool || s.Issue.WaitingForPool {
		dx.add("Issue is in the pool queue — waiting for a free worker slot.")
		dx.addPoolLine()
		label := "queued, waiting for pool capacity"
		if s.Issue.WaitingForPool && !s.PendingPool {
			dx.add("waiting_for_pool flag is set but no queue entry found — conductor may be about to retry.")
			label = "waiting for pool capacity"
		}
		return dx.result(
			fmt.Sprintf("#%d — %s", s.Issue.Number, label),
			"No action needed; the conductor will start this as soon as a slot is free. Or drag to TODO to cancel.",
			"warn",
		)
	}

	// 5–9. Last persisted session.
	if len(s.Sessions) > 0 {
		return dx.diagnoseLastSession(s.Sessions[0])
	}

	// No sessions at all.
	return dx.diagnoseIdle()
}

func (dx *diagnoser) diagnoseLive() IssueDiagnosis {
	sess := dx.s.LiveSession
	elapsed := time.Since(sess.StartedAt)

	switch sess.State {
	case types.StateBlocked:
		reason := sess.BlockedReason
		if reason == "" {
			reason = "(no reason captured)"
		}
		dx.add(fmt.Sprintf("%s session %s started %s ago — now blocked",
			sess.Mode, shortID(sess.ID), fmtElapsed(elapsed)))
		dx.add("Blocked reason: " + reason)
		dx.addPoolLine()
		dx.addWorktreeLine()
		dx.add("PR: " + prState(dx.s.Issue))
		return dx.result(
			fmt.Sprintf("#%d — %s session blocked", dx.s.Issue.Number, sess.Mode),
			"Read the blocked reason above. Drag to TODO to clear, fix the root cause, then requeue.",
			"stuck",
		)

	case types.StatePausedForQuestion:
		dx.add(fmt.Sprintf("Session %s is paused waiting for your answer (question %s)",
			shortID(sess.ID), sess.PendingQuestionID))
		if dx.s.AnswerPending {
			dx.add("Answer file is on disk — conductor may not have picked it up yet.")
		}
		return dx.result(
			fmt.Sprintf("#%d — awaiting your answer", dx.s.Issue.Number),
			"Click the card to open the question form and submit your answer.",
			"warn",
		)
	}

	// Running or waiting normally.
	pidInfo := pidSummary(sess)
	dx.add(fmt.Sprintf("Active %s session %s, running for %s — %s",
		sess.Mode, shortID(sess.ID), fmtElapsed(elapsed), pidInfo))
	if sess.PID > 0 && !PidAlive(sess.PID) {
		dx.add(fmt.Sprintf("⚠ PID %d not found — process may have exited; conductor will detect shortly.", sess.PID))
	}
	dx.addPoolLine()
	dx.addWorktreeLine()
	dx.add("PR: " + prState(dx.s.Issue))
	return dx.result(
		fmt.Sprintf("#%d — %s worker active (%s)", dx.s.Issue.Number, sess.Mode, pidInfo),
		"This is normal — worker is mid-task.",
		"ok",
	)
}

func (dx *diagnoser) diagnoseLastSession(last types.Session) IssueDiagnosis {
	elapsed := ""
	if last.EndedAt != nil {
		elapsed = fmtElapsed(time.Since(*last.EndedAt))
	}

	switch last.State {
	case types.StateBlocked, types.StateFailed:
		reason := last.BlockedReason
		if reason == "" {
			reason = "session ended without success"
		}
		dx.add(fmt.Sprintf("Last %s session ended %s ago in state %s", last.Mode, elapsed, last.State))
		dx.add("Reason: " + reason)
		dx.addWorktreeLine()
		dx.add("PR: " + prState(dx.s.Issue))
		return dx.result(
			fmt.Sprintf("#%d — last session %s %s ago", dx.s.Issue.Number, last.State, elapsed),
			"Read the failure reason above. Fix the root cause, then drag to TODO/PLAN and requeue.",
			"stuck",
		)

	case types.StatePausedForQuestion:
		dx.add(fmt.Sprintf("Session %s paused for answer (question %s)",
			shortID(last.ID), last.PendingQuestionID))
		if dx.s.AnswerPending {
			dx.add("Answer file is on disk — conductor may not have picked it up yet.")
		}
		return dx.result(
			fmt.Sprintf("#%d — session paused for your answer", dx.s.Issue.Number),
			"Click the card to open the question form.",
			"warn",
		)

	case types.StateCompleted:
		dx.add(fmt.Sprintf("Last %s session completed %s ago", last.Mode, elapsed))
		dx.add("PR: " + prState(dx.s.Issue))
		dx.add(fmt.Sprintf("Column: %s", dx.s.Issue.Column))
		// 6. Plan on disk but not ingested into DB?
		if dx.s.PlanOnDisk && dx.s.Issue.Plan == nil {
			dx.add(fmt.Sprintf("Plan file on disk (rev %d) but not loaded — ingest may have failed.", dx.s.PlanRevision))
			return dx.result(
				fmt.Sprintf("#%d — plan on disk but not ingested", dx.s.Issue.Number),
				"Check logs for planio errors. Try RefreshIssuesNow or drag to PLAN to re-queue a planner.",
				"stuck",
			)
		}
		return dx.result(
			fmt.Sprintf("#%d — last session completed %s ago", dx.s.Issue.Number, elapsed),
			"Card looks healthy. If PLAN completed, click to review the plan. If EXECUTE, check the PR.",
			"ok",
		)
	}

	dx.add(fmt.Sprintf("Last session state: %s, ended %s ago", last.State, elapsed))
	return dx.result(
		fmt.Sprintf("#%d — last session %s", dx.s.Issue.Number, last.State),
		"Check session transcript for details.",
		"warn",
	)
}

func (dx *diagnoser) diagnoseIdle() IssueDiagnosis {
	// 6. Plan on disk but not in DB?
	if dx.s.PlanOnDisk && dx.s.Issue.Plan == nil {
		dx.add(fmt.Sprintf("Plan file on disk (rev %d) but not loaded — ingest may have failed.", dx.s.PlanRevision))
		return dx.result(
			fmt.Sprintf("#%d — plan on disk but not ingested", dx.s.Issue.Number),
			"Check logs for planio errors. Try RefreshIssuesNow or drag to PLAN to re-queue a planner.",
			"stuck",
		)
	}

	dx.add(fmt.Sprintf("Column: %s, no sessions found", dx.s.Issue.Column))
	dx.add("PR: " + prState(dx.s.Issue))

	switch dx.s.Issue.Column {
	case types.ColPlan:
		return dx.result(
			fmt.Sprintf("#%d — in PLAN column, no planner running", dx.s.Issue.Number),
			"The orchestrator should pick this up next cycle. If stuck, check pool capacity or drag to TODO and back.",
			"warn",
		)
	case types.ColInProgress:
		return dx.result(
			fmt.Sprintf("#%d — in IN PROGRESS, no execute running", dx.s.Issue.Number),
			"The orchestrator should pick this up next cycle. If stuck, check pool capacity or drag back to PLAN.",
			"warn",
		)
	}

	return dx.result(
		fmt.Sprintf("#%d — idle (%s)", dx.s.Issue.Number, dx.s.Issue.Column),
		"Card is idle. Move to PLAN or IN PROGRESS to start work.",
		"ok",
	)
}

func (dx *diagnoser) addPoolLine() {
	if dx.s.PoolName != "" {
		dx.add(fmt.Sprintf("Slot: %d/%d %s", dx.s.PoolActive, dx.s.PoolCapacity, dx.s.PoolName))
	}
}

func (dx *diagnoser) addWorktreeLine() {
	if dx.s.WorktreeOnDisk {
		dx.add("Worktree: still on disk")
	} else {
		dx.add("Worktree: not on disk")
	}
}

func prState(issue types.Issue) string {
	if issue.PRNumber != nil {
		u := issue.PRURL
		if u == "" {
			u = fmt.Sprintf("PR #%d", *issue.PRNumber)
		}
		return fmt.Sprintf("PR #%d open (%s)", *issue.PRNumber, u)
	}
	return "not opened"
}

func pidSummary(sess *types.Session) string {
	if sess.PID <= 0 {
		return "harness session (no subprocess)"
	}
	if PidAlive(sess.PID) {
		return fmt.Sprintf("PID %d alive", sess.PID)
	}
	return fmt.Sprintf("PID %d not found", sess.PID)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func fmtElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%dm", m)
}
