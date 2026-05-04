package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"prismconductor/internal/eventbus"
	pcgit "prismconductor/internal/git"
	"prismconductor/internal/types"
)

// Reattach rebuilds in-memory session state for sessions that were live at
// last shutdown (issue #54).
//
// For each persisted session row in {running, waiting_for_input, blocked,
// paused_for_question}:
//
//   - if the worker PID is still alive on the OS, register a synthesized
//     runtimeSession and start a goroutine that polls the transcript file
//     from the recorded byte offset, feeding new lines through the same
//     parser/matchPatterns/handlers as a fresh spawn. The card stays in its
//     persisted state.
//   - if the worker is dead, scan the transcript tail for §10.3 sentinels.
//     The first matching arm sets the terminal state — including firing
//     onPROpened retroactively so a worker that finished off-conductor still
//     moves the card to REVIEW. If no sentinel was emitted, the session is
//     marked failed with an explanatory BlockedReason (q6).
//
// workspaces lets the goroutine reconstruct repoPath / worktreeDir for the
// post-mortem worktree cleanup that #22 relies on.
func (m *Manager) Reattach(sessions []types.Session, workspaces []types.Workspace) {
	if !detachedSupported() {
		log.Printf("session: detached worker spawning is not supported on this platform; conductor exit will terminate workers")
	}
	for _, sess := range sessions {
		sess := sess
		alive := pidAlive(sess.PID)
		if sess.Transcript == "" {
			// No transcript path on the row. Best effort: mark failed.
			m.markFailedReattach(sess, "transcript path missing on row")
			continue
		}
		ws := lookupWorkspace(workspaces, sess.WorkspaceID)
		rs := buildReattachedRuntime(sess, ws)
		if alive {
			// Rehydrate the pool slot that was occupied before shutdown so the
			// in-memory active counter reflects reality. Without this call the
			// UI worker counter undercounts by one per still-running session.
			if sess.PoolID != "" && m.poolReg != nil {
				if ok, err := m.poolReg.AcquireSpecific(sess.PoolID); err != nil {
					log.Printf("reattach: AcquireSpecific pool %s: %v", sess.PoolID, err)
				} else if !ok {
					log.Printf("reattach: pool %s not in registry for live session %s", sess.PoolID, sess.ID)
				}
			}
			// Register and follow the file. matchPatterns inside the goroutine
			// is responsible for any further state transitions (Q5 / Q6 /
			// PR_OPENED / Work complete / BLOCKED). On terminal exit the goroutine
			// itself handles the post-mortem classification + cleanup.
			ctx, cancel := context.WithCancel(context.Background())
			rs.cancel = cancel
			m.mu.Lock()
			m.sessions[sess.ID] = rs
			m.mu.Unlock()
			go m.followLive(ctx, rs)
		} else {
			// Worker is gone. Classify directly from whatever the file already
			// contains and mark the row terminal.
			m.classifyDeadWorker(rs)
		}
	}
}

// buildReattachedRuntime returns a runtimeSession in the same shape as one
// produced by spawnWithDir, minus the cmd / cancel (caller fills cancel for
// the live path). repoPath and the worktreeDir are best-effort: the
// post-mortem teardown runs only when both are present.
func buildReattachedRuntime(sess types.Session, ws *types.Workspace) *runtimeSession {
	rs := &runtimeSession{
		sess:             &sess,
		parser:           NewStreamParser(),
		transcriptPath:   sess.Transcript,
		transcriptOffset: sess.TranscriptOffset,
		reattached:       true,
		// Without this, the WorkerSlotFreed event published from
		// classifyDeadWorker carries an empty pool ID and the registry's
		// ReleaseByPool is a no-op — the in-memory active counter then
		// drifts up by one per app restart per dead-on-restart session.
		poolID: sess.PoolID,
	}
	if ws != nil {
		rs.repoPath = ws.RepoPath
		// The worktree dir naming convention from manager.go: <ws.ID>-<num>.
		if ws.RepoPath != "" && sess.IssueNumber > 0 && sess.Mode == types.ModeExecute {
			rs.worktreeDir = filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
				fmt.Sprintf("%s-%d", ws.ID, sess.IssueNumber))
			if _, err := os.Stat(rs.worktreeDir); err != nil {
				rs.worktreeDir = ""
			}
		}
	}
	return rs
}

func lookupWorkspace(workspaces []types.Workspace, id string) *types.Workspace {
	if id == "" {
		return nil
	}
	for i := range workspaces {
		if workspaces[i].ID == id {
			return &workspaces[i]
		}
	}
	return nil
}

// followLive polls the transcript file for a still-alive re-attached worker,
// feeding new lines through the parser. When the worker eventually dies, the
// goroutine drops into the dead-worker classification path before exiting.
func (m *Manager) followLive(ctx context.Context, rs *runtimeSession) {
	defer func() {
		m.mu.Lock()
		delete(m.sessions, rs.sess.ID)
		m.mu.Unlock()
	}()

	f, err := os.Open(rs.transcriptPath)
	if err != nil {
		log.Printf("reattach: open %s: %v", rs.transcriptPath, err)
		m.classifyDeadWorker(rs)
		return
	}
	defer f.Close()
	if rs.transcriptOffset > 0 {
		if _, err := f.Seek(rs.transcriptOffset, io.SeekStart); err != nil {
			log.Printf("reattach: seek %s offset=%d: %v",
				rs.transcriptPath, rs.transcriptOffset, err)
			rs.transcriptOffset = 0
		}
	}
	reader := bufio.NewReader(f)
	var lastFlush time.Time
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !pidAlive(rs.sess.PID) {
			// Drain whatever was written between the last EOF and process death,
			// then classify. The classifyDeadWorker path re-opens the file from
			// the *current* persisted offset, so persist before handing off.
			if m.store != nil {
				_ = m.store.UpdateSessionTranscriptOffset(rs.sess.ID, rs.transcriptOffset)
			}
			m.classifyDeadWorker(rs)
			return
		}
		line, rerr := reader.ReadString('\n')
		if len(line) > 0 {
			m.feedLine(rs, strings.TrimRight(line, "\r\n"))
			rs.transcriptOffset += int64(len(line))
			if m.store != nil && time.Since(lastFlush) >= 2*time.Second {
				_ = m.store.UpdateSessionTranscriptOffset(rs.sess.ID, rs.transcriptOffset)
				lastFlush = time.Now()
			}
		}
		if rerr == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if rerr != nil {
			log.Printf("reattach: read %s: %v", rs.transcriptPath, rerr)
			return
		}
	}
}

// classifyDeadWorker reads from the recorded transcript offset to EOF, feeds
// each line through the parser/matchPatterns flow, then determines the
// terminal state based on what sentinels (if any) showed up.
//
// matchPatterns already mutates rs.sess.State on Work complete / BLOCKED, and
// already calls onPROpened on PR_OPENED. So all we need to do here is:
//
//  1. feed the tail through.
//  2. check rs.sess.State afterwards.
//  3. for sessions still in non-terminal states, mark them failed with the
//     no-sentinel BlockedReason (q6).
//  4. persist the final state and run the post-mortem worktree teardown.
func (m *Manager) classifyDeadWorker(rs *runtimeSession) {
	prev := rs.sess.State
	if rs.transcriptPath != "" {
		f, err := os.Open(rs.transcriptPath)
		if err == nil {
			defer f.Close()
			if rs.transcriptOffset > 0 {
				_, _ = f.Seek(rs.transcriptOffset, io.SeekStart)
			}
			reader := bufio.NewReader(f)
			for {
				line, rerr := reader.ReadString('\n')
				if len(line) > 0 {
					m.feedLine(rs, strings.TrimRight(line, "\r\n"))
					rs.transcriptOffset += int64(len(line))
				}
				if rerr != nil {
					break
				}
			}
		} else {
			log.Printf("reattach: classify open %s: %v", rs.transcriptPath, err)
		}
	}
	if m.store != nil {
		_ = m.store.UpdateSessionTranscriptOffset(rs.sess.ID, rs.transcriptOffset)
	}
	switch rs.sess.State {
	case types.StateCompleted, types.StateBlocked, types.StateFailed,
		types.StatePausedForQuestion:
		// Already a terminal/awaiting state set by matchPatterns. Use as-is.
	default:
		// No terminal sentinel encountered — q6: mark failed with explanatory
		// reason so the card glows red and the user can re-approve.
		rs.sess.State = types.StateFailed
		rs.sess.BlockedReason = "worker exited without sentinel - see transcript"
	}
	now := time.Now()
	if rs.sess.EndedAt == nil {
		rs.sess.EndedAt = &now
	}
	if m.store != nil {
		_ = m.store.SaveSession(rs.sess, rs.transcriptPath)
		_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
	}
	if m.onStateChange != nil && prev != rs.sess.State {
		m.onStateChange(*rs.sess, prev)
	}

	// Worktree teardown for the dead-worker terminal cases. Mirror
	// manager.go's live-spawn cleanup: NEVER auto-remove a worktree that
	// contains user-paid-for work. Only safe-remove when the worktree is
	// verifiably empty (no uncommitted changes, no commits ahead of base).
	// See manager.go's tailAndParse cleanup for the full rationale.
	if rs.worktreeDir != "" {
		switch rs.sess.State {
		case types.StateBlocked, types.StateFailed:
			if pcgit.WorktreeIsEmpty(rs.worktreeDir) {
				if err := pcgit.Remove(rs.repoPath, rs.worktreeDir); err != nil {
					log.Printf("reattach: worktree cleanup (terminal=%s, empty) %s: %v",
						rs.sess.State, rs.worktreeDir, err)
				}
			} else {
				log.Printf("reattach: worktree preserved (terminal=%s) %s: contains uncommitted work; user can recover via `cd %s`",
					rs.sess.State, rs.worktreeDir, rs.worktreeDir)
			}
		}
	}

	// Mirror tailAndParse: publish the freed-slot event so any orchestrator
	// pool accounting that re-acquired the slot on startup can release it.
	if m.bus != nil {
		m.bus.Publish(eventbus.EvtWorkerSlotFreed, eventbus.WorkerSlotFreed{
			SessionID: rs.sess.ID,
			PoolID:    rs.poolID,
		})
	}
}

func (m *Manager) markFailedReattach(sess types.Session, reason string) {
	now := time.Now()
	prev := sess.State
	sess.State = types.StateFailed
	sess.EndedAt = &now
	sess.BlockedReason = reason
	if m.store != nil {
		_ = m.store.SaveSession(&sess, sess.Transcript)
		_ = m.store.UpdateSessionState(sess.ID, sess.State)
	}
	if m.onStateChange != nil {
		m.onStateChange(sess, prev)
	}
}

// pidAlive returns true if the given pid is still running. POSIX-only signal 0 trick.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
