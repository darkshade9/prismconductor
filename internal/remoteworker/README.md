# remoteworker — Cloudflare Worker execution (paused)

This package provisions and communicates with Cloudflare Worker deployments that
run Claude sessions remotely on behalf of a workspace.

## Status: paused as of issue #254

Remote execution is no longer offered during workspace creation. Existing remote
workspaces can be converted to local workspaces via Settings → Workspace →
"Convert to local". The code in this package is preserved so that conversion and
cleanup paths (key deletion, worker teardown) continue to work.

No new remote workspace creation or session spawning is wired up from the conductor.
The three spawn paths in `internal/session/manager.go` (`SpawnPlan`,
`SpawnExecute`, `SpawnExecuteResume`) all return `ErrRemoteWorkspacePaused`
immediately when `workspace.ExecutionTarget == "remote"`.
