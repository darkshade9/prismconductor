package orchestrator

import (
	"log"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// drainPendingPools processes persisted pending-pool requests FIFO, spawning
// workers as slots become available. Called on EvtWorkerSlotFreed and at
// conductor startup to recover intent that survived a restart (issue #40).
func (o *Orchestrator) drainPendingPools(reason string) {
	if o.pool == nil || o.store == nil {
		return
	}
	pending, err := o.store.ListPendingPools(50)
	if err != nil {
		log.Printf("orchestrator: drain list pending: %v", err)
		return
	}
	for _, req := range pending {
		ws := types.Workspace{ID: req.WorkspaceID}
		if o.lookupWS != nil {
			if got, ok := o.lookupWS(req.WorkspaceID); ok {
				ws = got
			}
		}
		poolID, ok := o.selectPool(ws, req.Role)
		if !ok {
			// No slot available for this role yet — leave row in place.
			continue
		}
		var spawnErr error
		switch req.Role {
		case types.RolePlan:
			if o.spawn == nil {
				o.pool.ReleaseByPool(poolID)
				continue
			}
			// Ensure the issue is in PLAN before spawning; it may still be in
			// TODO if autoPull enqueued it without moving the card.
			loaded, lerr := o.store.LoadIssue(req.WorkspaceID, req.IssueNumber)
			if lerr != nil {
				// Issue gone — drop stale row.
				_ = o.store.DequeuePendingPool(req.ID)
				o.pool.ReleaseByPool(poolID)
				continue
			}
			if loaded.Column == types.ColTodo {
				if err := o.store.MoveIssueColumn(req.WorkspaceID, req.IssueNumber, types.ColPlan); err != nil {
					o.pool.ReleaseByPool(poolID)
					continue
				}
			}
			spawnErr = o.spawn(req.WorkspaceID, req.IssueNumber, poolID)
		case types.RoleWork:
			if o.spawnWork == nil {
				o.pool.ReleaseByPool(poolID)
				continue
			}
			spawnErr = o.spawnWork(req.WorkspaceID, req.IssueNumber, poolID)
		default:
			o.pool.ReleaseByPool(poolID)
			_ = o.store.DequeuePendingPool(req.ID)
			continue
		}

		if spawnErr != nil {
			o.pool.ReleaseByPool(poolID)
			log.Printf("orchestrator: drain spawn failed (reason=%s, issue=#%d role=%s): %v",
				reason, req.IssueNumber, req.Role, spawnErr)
			continue
		}

		// Spawn succeeded: clean up.
		_ = o.store.DequeuePendingPool(req.ID)
		o.clearWaitingForPool(req.WorkspaceID, req.IssueNumber)
		o.bus.Publish(eventbus.EvtPendingPoolDequeued, eventbus.PendingPoolChange{
			WorkspaceID: req.WorkspaceID,
			IssueNumber: req.IssueNumber,
			Role:        string(req.Role),
		})
	}
}

// enqueuePending persists a pending-pool request for iss and marks the issue
// as waiting_for_pool so the card shows the waiting decoration.
func (o *Orchestrator) enqueuePending(iss *types.Issue, role types.Role, action string) {
	if o.store == nil {
		return
	}
	if err := o.store.EnqueuePendingPool(iss.WorkspaceID, iss.Number, role, action); err != nil {
		log.Printf("orchestrator: enqueue pending pool (#%d): %v", iss.Number, err)
		return
	}
	if loaded, err := o.store.LoadIssue(iss.WorkspaceID, iss.Number); err == nil && !loaded.WaitingForPool {
		loaded.WaitingForPool = true
		_, _ = o.store.SaveIssue(loaded)
	}
	o.bus.Publish(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
		WorkspaceID: iss.WorkspaceID,
		IssueNumber: iss.Number,
		Role:        string(role),
	})
}

// clearWaitingForPool clears the WaitingForPool flag and removes any pending
// rows for the issue. No-op if the issue is already clear.
func (o *Orchestrator) clearWaitingForPool(workspaceID string, issueNumber int) {
	if o.store == nil {
		return
	}
	_ = o.store.DeletePendingForIssue(workspaceID, issueNumber)
	loaded, err := o.store.LoadIssue(workspaceID, issueNumber)
	if err != nil || !loaded.WaitingForPool {
		return
	}
	loaded.WaitingForPool = false
	_, _ = o.store.SaveIssue(loaded)
}
