package orchestrator

import "prismconductor/internal/types"

// selectPool acquires a slot on the highest-preference available pool for the
// given role (preference = lowest Priority value, then created_at). Returns
// the pool ID on success. The registry's acquireRoundRobin already sorts by
// priority; this wrapper just dispatches to the right role method.
func (o *Orchestrator) selectPool(ws types.Workspace, role types.Role) (string, bool) {
	switch role {
	case types.RolePlan:
		return o.pool.AcquireForPlan(ws)
	case types.RoleWork:
		return o.pool.AcquireForWork(ws)
	}
	return "", false
}
