// Package wailsbus wraps wruntime.EventsEmit with a bounded ring buffer so
// every emission is observable by the developer diagnostics panel.
package wailsbus

import (
	"context"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const BufCap = 500

// EmitRecord captures metadata about one Wails event emission.
type EmitRecord struct {
	Name    string      `json:"name"`
	Payload interface{} `json:"payload"`
	SentAt  time.Time   `json:"sent_at"`
}

var (
	mu      sync.RWMutex
	entries [BufCap]EmitRecord
	head    int
	count   int
)

// EmitEvent records the event in the ring buffer then forwards it to the Wails
// runtime. A nil ctx is accepted: the event is buffered but not forwarded
// (safe to call during startup races before the Wails context is ready).
func EmitEvent(ctx context.Context, name string, payload interface{}) {
	mu.Lock()
	entries[head] = EmitRecord{Name: name, Payload: payload, SentAt: time.Now()}
	head = (head + 1) % BufCap
	if count < BufCap {
		count++
	}
	mu.Unlock()
	if ctx != nil {
		wruntime.EventsEmit(ctx, name, payload)
	}
}

// GetDebugEmits returns a snapshot of the ring buffer, newest first.
func GetDebugEmits() []EmitRecord {
	mu.RLock()
	defer mu.RUnlock()
	n := count
	out := make([]EmitRecord, n)
	for i := 0; i < n; i++ {
		idx := (head - 1 - i + BufCap) % BufCap
		out[i] = entries[idx]
	}
	return out
}

// ReplayLatest re-emits the most recent buffered event whose name matches.
// Returns false when no matching entry exists.
func ReplayLatest(ctx context.Context, name string) bool {
	mu.RLock()
	var found *EmitRecord
	for i := 0; i < count; i++ {
		idx := (head - 1 - i + BufCap) % BufCap
		if entries[idx].Name == name {
			rec := entries[idx]
			found = &rec
			break
		}
	}
	mu.RUnlock()
	if found == nil {
		return false
	}
	EmitEvent(ctx, found.Name, found.Payload)
	return true
}
