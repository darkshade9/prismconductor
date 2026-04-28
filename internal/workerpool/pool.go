// Package workerpool tracks worker slot capacity (PRISMCONDUCTOR_PLAN.md §14 Phase 5).
package workerpool

import "sync"

type Pool struct {
	mu       sync.Mutex
	capacity int
	active   int
}

func New(capacity int) *Pool { return &Pool{capacity: capacity} }

func (p *Pool) Capacity() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.capacity
}

func (p *Pool) Active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *Pool) Free() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.capacity - p.active
}

// SetCapacity changes capacity. Caller decides whether to publish EvtAgentCountChanged.
func (p *Pool) SetCapacity(n int) {
	p.mu.Lock()
	p.capacity = n
	p.mu.Unlock()
}

// TryAcquire reserves a slot if free.
func (p *Pool) TryAcquire() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active >= p.capacity {
		return false
	}
	p.active++
	return true
}

func (p *Pool) Release() {
	p.mu.Lock()
	if p.active > 0 {
		p.active--
	}
	p.mu.Unlock()
}
