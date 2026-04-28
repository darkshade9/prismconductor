// Package orchestrator wires goal/event-driven backlog ranking (PRISMCONDUCTOR_PLAN.md §8, §11).
package orchestrator

import (
	"prismconductor/internal/eventbus"
	"prismconductor/internal/ollama"
)

type Orchestrator struct {
	bus    *eventbus.Bus
	llm    *ollama.Client
}

func New(bus *eventbus.Bus, llm *ollama.Client) *Orchestrator {
	o := &Orchestrator{bus: bus, llm: llm}
	bus.Subscribe(o.handle)
	return o
}

// handle is the per-event router from §8. Each branch is stubbed.
func (o *Orchestrator) handle(e eventbus.Event) {
	switch e.Type {
	case eventbus.EvtGoalActivated, eventbus.EvtGoalUpdated:
		// TODO Phase 3: reload candidate issues, run rank+deps, reorder TODO.
	case eventbus.EvtIssueAdded:
		// TODO Phase 3: insert into TODO at correct position.
	case eventbus.EvtIssueClosed:
		// TODO Phase 3: recompute unblocked status of dependents.
	case eventbus.EvtPlanReady:
		// TODO Phase 4: notify; do NOT auto-pull next.
	case eventbus.EvtPlanApproved:
		// TODO Phase 4: spawn execute-mode worker.
	case eventbus.EvtPlanRejected:
		// TODO Phase 4: free worker slot, return card to TODO.
	case eventbus.EvtWorkerSlotFreed:
		// TODO Phase 5: pull next unblocked from TODO into PLAN.
	case eventbus.EvtWorkerBlocked:
		// TODO Phase 1: notify user.
	case eventbus.EvtCardMovedManually:
		// Respect override; do not re-rank.
	}
}
