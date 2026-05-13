package tracker

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[TrackerKind]Tracker{}
)

// Register adds an implementation to the global registry.
// Panics on duplicate — call from init().
func Register(t Tracker) {
	mu.Lock()
	defer mu.Unlock()
	k := t.Kind()
	if _, dup := registry[k]; dup {
		panic(fmt.Sprintf("tracker: duplicate registration for kind %q", k))
	}
	registry[k] = t
}

// Get returns the registered Tracker for kind, or an error when none is
// registered.
func Get(kind TrackerKind) (Tracker, error) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("tracker: no implementation registered for kind %q", kind)
	}
	return t, nil
}

// Registered returns the kinds of all currently registered trackers.
func Registered() []TrackerKind {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]TrackerKind, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
