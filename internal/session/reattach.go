package session

import (
	"os"
	"syscall"
	"time"

	"prismconductor/internal/types"
)

// Reattach handles sessions that were live at last shutdown.
//
// We can't re-tail the PTY of a process we didn't fork, so this is a
// "did it survive?" check. If alive, we leave it running (it will keep
// writing to its transcript file) and present it as a re-attached session
// the user can monitor via the transcript. If dead, we mark it failed.
func (m *Manager) Reattach(sessions []types.Session) {
	for _, sess := range sessions {
		alive := pidAlive(sess.PID)
		if alive {
			// Keep the persisted state; the process is still doing its thing.
			// We can't pipe its PTY into our event stream, so the UI surfaces
			// it as "re-attached — read transcript from disk".
			continue
		}
		now := time.Now()
		sess.State = types.StateFailed
		sess.EndedAt = &now
		if m.store != nil {
			_ = m.store.UpdateSessionState(sess.ID, sess.State)
		}
		if m.onStateChange != nil {
			m.onStateChange(sess, types.StateRunning)
		}
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
