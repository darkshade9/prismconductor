# remoteworker — Cloudflare Sandbox Worker execution (issue #284)

This package provisions and communicates with Cloudflare Worker deployments that
run conductor sessions remotely on behalf of a workspace.

## Status: Phase 1 active (plan sessions only)

Remote execution was paused in issue #254. Issue #284 re-activates plan sessions
via a new Cloudflare Sandbox Worker (`worker/sandbox-deploy/`) that runs in an
isolated Durable Object per session.

**Phase 1 scope:** plan sessions only. Execute and resume paths remain paused
until the Phase 1 POC gate metrics pass (cold-start, stream reliability, cost
threshold).

**Phase 2 (future):** The `SandboxSession.runPlan()` method in the worker will be
replaced with a Cloudflare Container spawn that clones the repo and runs the
`claude-code` CLI directly. The PHASE-2 comment markers in the worker source show
the exact insertion points.

## Worker bundles

Two bundles live in the binary:

| Variable | Source | Use |
|---|---|---|
| `WorkerBundle` | `worker/dist/worker.js` | Legacy Durable Objects worker (retained for existing remote workspaces) |
| `SandboxWorkerBundle` | `worker/sandbox-deploy/dist/worker.js` | New sandbox worker; used for all new deployments |

`CreateRemoteWorkspace` and `DeployRemoteWorker` in `app.go` deploy
`SandboxWorkerBundle` via `DeploySandboxWorker`. Existing remote workspaces
continue to use their already-deployed endpoint until the user re-deploys.

## Session wire protocol

The conductor client (`client.go`) speaks the same wire protocol to both workers:

```
POST   /sessions         { workspace_id, issue_number, mode, github_owner, github_repo, default_branch, pool_model }
                         → { session_id }  HTTP 201

GET    /sessions/:id/stream   (SSE — id: / data: lines)
DELETE /sessions/:id     → { ok: true }
```

The sandbox worker rejects `mode != "plan"` in Phase 1 with HTTP 400.

## Authentication

All session endpoints require `Authorization: Bearer <CONDUCTOR_API_KEY>`.
The API key is generated during workspace setup and stored in:
- The local keychain (via `remoteworker.SetKey`) so the conductor can sign requests.
- As a CF Secret `CONDUCTOR_API_KEY` on the deployed worker.

The sandbox worker also requires `ANTHROPIC_API_KEY` as a mandatory CF Secret
(set during workspace deploy via `UpsertSecret`). Subscription/OAuth-only auth
modes are explicitly blocked.

## Spawn flow

1. `session.Manager.SpawnPlan` detects `ExecutionTarget == "remote"` → delegates to `spawnRemotePlan`.
2. `spawnRemotePlan` reads the workspace's `RemoteConfig.CFWorkerEndpointURL`, retrieves the conductor API key from the keychain, and calls `remoteworker.Spawn`.
3. `remoteworker.Spawn` `POST /sessions` with the plan params and starts a goroutine that reads the SSE transcript into the local transcript file.
4. The existing `tailAndParse` goroutine reads the transcript and advances session state identically to local sessions.

## Pause state for execute paths

`SpawnExecute` and `SpawnExecuteResume` still return `ErrRemoteWorkspacePaused`
for remote workspaces. This guard will be removed when Phase 2 is ready.
