package remoteworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSpawn_success(t *testing.T) {
	sessionID := "sess-abc123"
	events := []string{
		`{"type":"text","text":"hello"}`,
		`[conductor] session started`,
		`Work complete.`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(SpawnResponse{SessionID: sessionID})

		case r.Method == "GET" && r.URL.Path == "/sessions/"+sessionID+"/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for i, ev := range events {
				fmt.Fprintf(w, "id:%d\ndata:%s\n\n", i, ev)
				flusher.Flush()
			}
			// Clean EOF.

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	replaceHTTPClient(t, srv.Client())

	tf, err := os.CreateTemp(t.TempDir(), "transcript-*.log")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer tf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rc, err := Spawn(ctx, srv.URL, SpawnParams{
		WorkspaceID: "ws1",
		IssueNumber: 42,
		Mode:        "execute",
	}, tf)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if rc.SessionID() != sessionID {
		t.Errorf("SessionID = %q, want %q", rc.SessionID(), sessionID)
	}

	// Wait for SSE goroutine to finish.
	if err := rc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// Transcript file should contain the emitted lines.
	_ = tf.Sync()
	content, _ := os.ReadFile(tf.Name())
	for _, ev := range events {
		if !containsLine(string(content), ev) {
			t.Errorf("transcript missing line: %q\nfull content:\n%s", ev, content)
		}
	}
}

func TestSpawn_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	replaceHTTPClient(t, srv.Client())

	tf, _ := os.CreateTemp(t.TempDir(), "transcript-*.log")
	defer tf.Close()

	ctx := context.Background()
	_, err := Spawn(ctx, srv.URL, SpawnParams{}, tf)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

func containsLine(content, line string) bool {
	for _, l := range splitLines(content) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
