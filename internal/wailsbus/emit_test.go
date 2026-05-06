package wailsbus

import (
	"testing"
)

func resetBuf() {
	mu.Lock()
	head = 0
	count = 0
	mu.Unlock()
}

func TestRingBufferOrdering(t *testing.T) {
	resetBuf()
	EmitEvent(nil, "a", 1)
	EmitEvent(nil, "b", 2)
	EmitEvent(nil, "c", 3)
	snap := GetDebugEmits()
	if len(snap) != 3 {
		t.Fatalf("want 3 entries, got %d", len(snap))
	}
	if snap[0].Name != "c" || snap[1].Name != "b" || snap[2].Name != "a" {
		t.Errorf("unexpected order: %v %v %v", snap[0].Name, snap[1].Name, snap[2].Name)
	}
}

func TestRingBufferWraps(t *testing.T) {
	resetBuf()
	for i := 0; i < BufCap+10; i++ {
		EmitEvent(nil, "e", i)
	}
	snap := GetDebugEmits()
	if len(snap) != BufCap {
		t.Fatalf("want %d entries after overflow, got %d", BufCap, len(snap))
	}
	// Most recent payload should be the last value emitted
	if snap[0].Payload.(int) != BufCap+9 {
		t.Errorf("newest payload: got %v, want %d", snap[0].Payload, BufCap+9)
	}
}

func TestReplayLatest(t *testing.T) {
	resetBuf()
	EmitEvent(nil, "x", "first")
	EmitEvent(nil, "y", "middle")
	EmitEvent(nil, "x", "last")

	// Nonexistent name returns false
	if ReplayLatest(nil, "nonexistent") {
		t.Error("should return false for unknown event name")
	}

	// Finds most recent "x" and re-emits (nil ctx = buffered only)
	if !ReplayLatest(nil, "x") {
		t.Error("should find and replay 'x'")
	}

	// The re-emitted entry should appear as the newest
	snap := GetDebugEmits()
	if snap[0].Name != "x" || snap[0].Payload.(string) != "last" {
		t.Errorf("replayed entry mismatch: %+v", snap[0])
	}
}
