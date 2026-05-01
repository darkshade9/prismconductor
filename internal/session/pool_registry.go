package session

// PoolRehydrator is satisfied by workerpool.Registry. Reattach calls
// AcquireSpecific once per live re-attached session to restore the in-memory
// active counter to the slot that was occupied when the conductor shut down,
// so the UI worker counter reflects reality without waiting for a slot-freed
// event that will never arrive for a still-running worker.
type PoolRehydrator interface {
	AcquireSpecific(poolID string) (bool, error)
}

// SetPoolRehydrator wires the pool registry so Reattach can mark live-session
// slots as occupied on startup. Call before the first Reattach call.
func (m *Manager) SetPoolRehydrator(r PoolRehydrator) { m.poolReg = r }
