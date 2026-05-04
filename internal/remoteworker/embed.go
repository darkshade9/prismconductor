package remoteworker

// WorkerBundle holds the pre-built Cloudflare Worker JS bundle.
// Populated at startup by app.go via SetWorkerBundle so the embed
// directive (which cannot reference paths outside the package tree) lives
// at the module root level instead of here.
var WorkerBundle []byte

// SetWorkerBundle wires the embedded bundle from the caller (app.go).
func SetWorkerBundle(b []byte) { WorkerBundle = b }
