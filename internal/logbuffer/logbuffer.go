// Package logbuffer captures every line written to the standard log + a
// designated stderr-tee into a bounded in-memory ring so the UI can surface
// what the backend has been doing.
package logbuffer

import (
	"bytes"
	"io"
	"log"
	"sync"
	"time"
)

type Entry struct {
	TS     time.Time `json:"ts"`
	Level  string    `json:"level"` // "info" | "stderr"
	Source string    `json:"source"`
	Text   string    `json:"text"`
}

type Ring struct {
	mu      sync.RWMutex
	cap     int
	entries []Entry
	subs    []chan Entry
}

func New(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 500
	}
	return &Ring{cap: capacity}
}

// Append adds an entry; oldest are evicted when capacity is reached.
func (r *Ring) Append(e Entry) {
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	r.mu.Lock()
	r.entries = append(r.entries, e)
	if len(r.entries) > r.cap {
		r.entries = r.entries[len(r.entries)-r.cap:]
	}
	subs := append([]chan Entry(nil), r.subs...)
	r.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (r *Ring) Snapshot() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// HookStdLog rewires the global `log` package so every Printf also lands here.
func (r *Ring) HookStdLog(source string) {
	log.SetOutput(io.MultiWriter(log.Writer(), &logSink{ring: r, source: source}))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

// TeeWriter wraps an io.Writer (e.g. os.Stderr) so writes are mirrored into
// the ring with the given level/source label.
func (r *Ring) TeeWriter(w io.Writer, level, source string) io.Writer {
	return io.MultiWriter(w, &logSink{ring: r, source: source, level: level})
}

type logSink struct {
	ring   *Ring
	source string
	level  string
}

func (s *logSink) Write(p []byte) (int, error) {
	level := s.level
	if level == "" {
		level = "info"
	}
	for _, line := range bytes.Split(bytes.TrimRight(p, "\n"), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		s.ring.Append(Entry{Level: level, Source: s.source, Text: string(line)})
	}
	return len(p), nil
}
