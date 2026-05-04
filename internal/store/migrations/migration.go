// Package migrations defines the versioned migration framework for conductor.db.
// Migration IDs sort lexicographically in application order (YYYYMMDD_NN_desc).
package migrations

import "database/sql"

// Migration is one unit of schema change. Either SQL or Up (or both) must be set.
type Migration struct {
	ID          string
	Description string
	SQL         string               // executed verbatim inside a transaction if non-empty
	Up          func(*sql.Tx) error  // called inside the same transaction after SQL
}
