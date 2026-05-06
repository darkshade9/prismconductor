# PrismConductor Remote Worker

Cloudflare Worker that runs agent sessions (plan / execute) on behalf of
PrismConductor, allowing work to happen off the user's laptop.

## Architecture

```
Conductor (desktop)  ──POST /sessions──►  CF Worker (Durable Object)
                                                  │
                     ◄──SSE /sessions/:id/stream──┘
                     (transcript lines in real-time)
```

The worker exposes a small HTTP API. For each session it:

1. Clones the repository over HTTPS using the `GITHUB_PAT` CF Secret.
2. Calls the Anthropic API (Claude) with the appropriate skill prompt.
3. Commits and pushes the result using the same PAT.
4. Emits `PR_OPENED: <url>` and `Work complete.` sentinels so the conductor
   can advance the card state exactly as with a local session.

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/sessions` | Spawn a new session. Body: `SpawnRequest`. Returns `{ session_id }`. |
| `GET` | `/sessions/:id/stream` | SSE transcript stream. Supports `Last-Event-ID` reconnect. |
| `POST` | `/sessions/:id/input` | Send stdin (interactive input). |
| `POST` | `/sessions/:id/answer` | Answer a mid-run question. |
| `DELETE` | `/sessions/:id` | Kill a session. |
| `GET` | `/sessions/active` | List active session IDs. |
| `GET` | `/health` | Health check. |

### SpawnRequest shape

```json
{
  "workspace_id": "my-repo",
  "issue_number": 42,
  "mode": "execute",
  "github_owner": "octocat",
  "github_repo": "my-repo",
  "default_branch": "main",
  "plan_revision": 1,
  "pool_model": "claude-sonnet-4-5"
}
```

## Development

```bash
cd worker
npm install
npx esbuild src/index.ts --bundle --format=esm --outfile=dist/worker.js
```

## Deployment

Deployment is managed by the conductor via the **Settings → Workspaces → + Add workspace → Remote** flow. The conductor:

1. Calls `TestCloudflareToken` to verify the CF API token and resolve the account ID.
2. Calls `TestGitHubPAT` to verify push access to the target repo.
3. Calls `DeployRemoteWorker` which uploads `worker/dist/worker.js` (embedded in the binary) and stores the GitHub PAT as a CF Secret (`GITHUB_PAT`).

To re-deploy after updating the bundle: rebuild the Go binary (which embeds the updated `dist/worker.js`) and use **Settings → Workspaces → {workspace} → Remote Auth → Re-deploy worker**.

## Secrets

| Secret name | Description |
|-------------|-------------|
| `GITHUB_PAT` | GitHub Personal Access Token with `repo` scope. Never stored on the user's disk. |

## Durable Objects

The worker uses a single Durable Object class (`SessionDO`) to manage the session lifecycle and buffer the SSE transcript ring. The `wrangler.toml` must declare:

```toml
[[durable_objects.bindings]]
name = "SESSIONS"
class_name = "SessionDO"

[[migrations]]
tag = "v1"
new_classes = ["SessionDO"]
```

## Commit-and-push tier strategy

The `conductor-execute` skill uses a four-tier fallback to commit and push work.
Tiers are attempted in order; the first success stops the chain.

| Tier | Name | Mechanism | Falls back when |
|------|------|-----------|-----------------|
| 1 | GitHub API | `createCommitOnBranch` GraphQL mutation (GitHub-signed) | payload > 8 MB, > 200 files, or API error |
| 2a | Local signed | `git commit -S` + `git push` | GPG unavailable / key blocked / TTY timeout |
| 2b | Local unsigned | `git commit` + `git push` after probing `required_signatures` | `required_signatures: true` on target branch |
| 3 | NEEDS_PR | Staged worktree preserved; user pushes manually | all above fail or signing required |

**Tier 1 pre-flight:** before calling `createCommitOnBranch`, the worker checks
`git ls-remote --heads origin <branch>` and pushes the branch if it is not yet
on origin. This avoids a "Reference does not exist" GraphQL rejection on fresh
branches (issue #205).

**Tier 2b probe:** before attempting an unsigned commit the worker calls
`gh api repos/{owner}/{repo}/branches/{BASE}/protection --jq '.required_signatures.enabled'`.
A 404 / error is treated as `false` (no enforcement). The result is cached for
the duration of the worker run.

## Feature flag

Remote workspaces are gated behind `execution_target: "remote"` on the
workspace JSON (issue #171, q3). Local workspaces are unaffected; no config
changes are required for existing users.
