package remoteworker

// WorkerBundle holds the pre-built legacy Cloudflare Worker JS bundle.
// Populated at startup by app.go via SetWorkerBundle so the embed
// directive (which cannot reference paths outside the package tree) lives
// at the module root level instead of here.
var WorkerBundle []byte

// SetWorkerBundle wires the embedded bundle from the caller (app.go).
func SetWorkerBundle(b []byte) { WorkerBundle = b }

// SandboxWorkerBundle holds the pre-built sandbox worker JS bundle (issue #284).
// Uses Durable Objects for per-session state and requires API-key auth.
// Populated at startup via SetSandboxWorkerBundle.
var SandboxWorkerBundle []byte

// SetSandboxWorkerBundle wires the embedded sandbox bundle from the caller (app.go).
func SetSandboxWorkerBundle(b []byte) { SandboxWorkerBundle = b }
