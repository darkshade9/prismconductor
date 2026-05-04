package remoteworker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SpawnParams is the payload for POST /sessions on the remote worker.
type SpawnParams struct {
	WorkspaceID   string `json:"workspace_id"`
	IssueNumber   int    `json:"issue_number"`
	Mode          string `json:"mode"` // "plan" | "execute"
	GitHubOwner   string `json:"github_owner"`
	GitHubRepo    string `json:"github_repo"`
	DefaultBranch string `json:"default_branch"`
	PlanRevision  int    `json:"plan_revision,omitempty"` // execute only
	QuestionID    string `json:"question_id,omitempty"`   // resume only
	PoolModel     string `json:"pool_model,omitempty"`
}

// SpawnResponse is the JSON returned by POST /sessions.
type SpawnResponse struct {
	SessionID string `json:"session_id"`
}

// RemoteCmd implements session.sessionCmd for a session running inside a
// Cloudflare Worker. The SSE goroutine reads transcript lines from
// GET /sessions/:id/stream and writes each one to the local transcript file,
// so the existing tailAndParse path sees identical input regardless of whether
// the session is local or remote.
type RemoteCmd struct {
	endpoint  string
	sessionID string
	done      chan struct{}
	termErr   error
	cancel    context.CancelFunc
}

// Wait blocks until the SSE stream ends or the context is cancelled.
func (r *RemoteCmd) Wait() error {
	<-r.done
	return r.termErr
}

// Kill cancels the SSE goroutine and sends DELETE /sessions/:id to the worker.
func (r *RemoteCmd) Kill() error {
	r.cancel()
	return deleteSession(r.endpoint, r.sessionID)
}

// Pid returns 0; remote sessions have no local PID.
func (r *RemoteCmd) Pid() int { return 0 }

// SessionID returns the remote session identifier.
func (r *RemoteCmd) SessionID() string { return r.sessionID }

// Spawn posts to POST /sessions on the remote worker, starts a goroutine that
// copies SSE transcript lines to tf (the local transcript file), and returns a
// RemoteCmd that the session manager can Wait/Kill as usual.
//
// Callers must not close tf; the returned RemoteCmd's goroutine owns the write
// side and the caller's tailAndParse owns the read side.
func Spawn(ctx context.Context, endpoint string, params SpawnParams, tf *os.File) (*RemoteCmd, error) {
	body, _ := json.Marshal(params)
	url := strings.TrimRight(endpoint, "/") + "/sessions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote spawn POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("remote worker: 401 unauthorized — CF or GitHub token may be expired")
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote spawn: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var sr SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("remote spawn parse: %w", err)
	}
	if sr.SessionID == "" {
		return nil, fmt.Errorf("remote spawn: empty session_id")
	}

	childCtx, cancel := context.WithCancel(ctx)
	rc := &RemoteCmd{
		endpoint:  endpoint,
		sessionID: sr.SessionID,
		done:      make(chan struct{}),
		cancel:    cancel,
	}

	go func() {
		defer close(rc.done)
		rc.termErr = streamToFile(childCtx, endpoint, sr.SessionID, tf)
	}()

	return rc, nil
}

// streamToFile connects to GET /sessions/:id/stream (SSE) and writes each
// data line into tf. Returns nil on clean EOF (session completed), or the
// context error on cancellation.
func streamToFile(ctx context.Context, endpoint, sessionID string, tf *os.File) error {
	url := fmt.Sprintf("%s/sessions/%s/stream", strings.TrimRight(endpoint, "/"), sessionID)
	const maxRetries = 5
	lastEventID := ""

	for attempt := range maxRetries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/event-stream")
		if lastEventID != "" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}

		err = readSSE(ctx, resp.Body, tf, &lastEventID)
		resp.Body.Close()

		if err == nil {
			// Clean EOF = session done.
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Transient error: retry with backoff.
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
	return fmt.Errorf("SSE stream: exhausted %d retries", maxRetries)
}

// readSSE parses an SSE stream, writing "data:" payloads as newline-terminated
// lines to tf. Updates lastEventID from "id:" fields so the caller can
// reconnect with Last-Event-ID on transient disconnect.
func readSSE(ctx context.Context, r io.Reader, tf *os.File, lastEventID *string) error {
	sc := bufio.NewScanner(r)
	var dataLines []string

	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id:"):
			*lastEventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataLines = append(dataLines, payload)
		case line == "":
			// Blank line: dispatch accumulated event.
			if len(dataLines) > 0 {
				for _, dl := range dataLines {
					_, _ = fmt.Fprintln(tf, dl)
				}
				dataLines = dataLines[:0]
			}
		}
	}
	return sc.Err()
}

// deleteSession sends DELETE /sessions/:id to the remote worker (best-effort).
func deleteSession(endpoint, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", strings.TrimRight(endpoint, "/"), sessionID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
