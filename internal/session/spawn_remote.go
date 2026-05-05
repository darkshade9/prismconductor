package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"prismconductor/internal/remoteworker"
	"prismconductor/internal/types"
)

// spawnRemotePlan creates a plan-mode session that runs inside a remote
// Cloudflare Worker (§10.1 remote variant). It mirrors spawnRemote but uses
// ModePlan and posts to the worker's /sessions endpoint with mode="plan".
//
// ErrRemoteNotReady is returned immediately when the workspace has no
// CFWorkerEndpointURL configured, so the caller can surface an error to the
// UI before any session row is created.
func (m *Manager) spawnRemotePlan(ws types.Workspace, issue types.Issue, pool types.Pool) (*types.Session, error) {
	rc := ws.RemoteConfig
	if rc == nil || rc.CFWorkerEndpointURL == "" {
		return nil, ErrRemoteNotReady
	}

	key := fmt.Sprintf("%s:%d:%s", ws.ID, issue.Number, types.ModePlan)
	m.mu.Lock()
	if m.inFlight[key] {
		m.mu.Unlock()
		return nil, ErrDuplicateSpawn
	}
	for _, rs := range m.sessions {
		if rs == nil || rs.sess == nil {
			continue
		}
		if rs.sess.WorkspaceID != ws.ID || rs.sess.IssueNumber != issue.Number || rs.sess.Mode != types.ModePlan {
			continue
		}
		switch rs.sess.State {
		case types.StateRunning, types.StateWaitingForInput, types.StatePausedForQuestion, types.StateBlocked:
			m.mu.Unlock()
			return nil, ErrDuplicateSpawn
		}
	}
	m.inFlight[key] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.inFlight, key)
		m.mu.Unlock()
	}()

	if m.transcriptDir == "" {
		return nil, fmt.Errorf("session manager: transcript dir not configured")
	}
	if err := os.MkdirAll(m.transcriptDir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	sessID := uuid.NewString()
	transcriptPath := filepath.Join(m.transcriptDir, sessID+".log")
	tf, err := os.Create(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("create transcript: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &types.Session{
		ID:          sessID,
		WorkspaceID: ws.ID,
		IssueNumber: issue.Number,
		Mode:        types.ModePlan,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
		PoolID:      pool.ID,
	}
	sess.Transcript = transcriptPath

	params := remoteworker.SpawnParams{
		WorkspaceID:   ws.ID,
		IssueNumber:   issue.Number,
		Mode:          string(types.ModePlan),
		GitHubOwner:   ws.GitHubOwner,
		GitHubRepo:    ws.GitHubRepo,
		DefaultBranch: ws.DefaultBranch,
		PoolModel:     pool.Model,
	}
	apiKey, err := remoteworker.GetKey(ws.ID)
	if err != nil {
		_ = tf.Close()
		_ = os.Remove(transcriptPath)
		cancel()
		return nil, fmt.Errorf("remote plan spawn: read API key: %w", err)
	}
	remCmd, err := remoteworker.Spawn(ctx, rc.CFWorkerEndpointURL, apiKey, params, tf)
	if err != nil {
		_ = tf.Close()
		_ = os.Remove(transcriptPath)
		cancel()
		return nil, fmt.Errorf("remote plan spawn: %w", err)
	}

	rs := &runtimeSession{
		sess:           sess,
		cmd:            remCmd,
		cancel:         cancel,
		parser:         NewStreamParser(),
		transcriptPath: transcriptPath,
		transcriptFile: tf,
		poolID:         pool.ID,
		poolName:       pool.Name,
		poolModel:      pool.Model,
		lastLineAt:     time.Now(),
	}

	if m.store != nil {
		_ = m.store.SaveSession(sess, rs.transcriptPath)
	}
	if m.onStateChange != nil {
		m.onStateChange(*sess, "")
	}

	m.mu.Lock()
	m.sessions[sess.ID] = rs
	m.mu.Unlock()

	go m.tailAndParse(ctx, rs)
	return sess, nil
}
