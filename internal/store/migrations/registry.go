package migrations

// all is the ordered list of every migration this binary knows about.
// IDs must be lexicographically ordered (YYYYMMDD_NN_description).
// Never remove or reorder entries; only append.
var all = []Migration{
	{
		ID:          "20250504_00_initial_migration_framework",
		Description: "baseline: record all pre-framework schema (created by legacy migrate()) as applied",
		// No SQL/Up — the schema already exists via the legacy migrate() call.
		// Recording this entry means any future binary can detect a downgrade.
	},
}

// All returns every known migration in application order.
func All() []Migration { return all }

// MaxID returns the highest known migration ID (the binary's schema level).
func MaxID() string {
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1].ID
}
