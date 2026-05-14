# Remote Workspaces

> **Current status:** remote workspace execution is paused in the local app. Existing remote workspace configuration is preserved so users can convert or inspect it, but normal product usage should use local workspaces until the remote execution/auth model is re-enabled.

Remote workspaces run agent tasks inside a Cloudflare Worker instead of on your local machine. This means tasks survive laptop sleep, run in parallel, and never consume your local CPU or memory during long builds.

## Choosing local vs remote

| Concern | Local | Remote |
|--|--|--|
| Where the work runs | Your machine | Cloudflare's servers |
| Survives laptop sleep | No | Yes |
| GitHub auth | Your local SSH/gh auth | A PAT you supply |
| Cost | Your hardware + LLM API | Your CF compute + LLM API |
| Privacy | Code never leaves your laptop | Code clones to a CF Worker |
| Setup time | Add repo path | ~5 min — paste two tokens |
| Best for | Quick tasks, sensitive code | Long runs, parallel work, big repos |

**Use local** when you're working with code you don't want to leave your machine, or for short tasks where the extra setup isn't worth it.

**Use remote** when you're running long jobs, working with a big monorepo, or want tasks to keep running while your laptop is closed.

## Cloudflare API token: how to create

Go to [dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens) → **Create Token** → **Custom Token**.

### Required permissions

| Category | Permission | Level |
|--|--|--|
| Account → Workers Scripts | Edit | Required |
| Account → Account Settings | Read | Required |

### Optional permissions (future-proofing)

| Category | Permission | Level |
|--|--|--|
| Account → Workers Routes | Edit | Optional — custom domains |
| Account → Workers KV Storage | Edit | Optional — per-session state |

**Account resources:** Select **All accounts** or specifically the account you'll use.
**Zone resources:** Not needed — leave at default.

Click **Continue to summary** → **Create Token** → copy the token value immediately (it won't be shown again).

### Why these scopes

These are the minimum permissions needed to deploy and manage the conductor's worker:
- **Workers Scripts: Edit** — lets the conductor upload the worker bundle and update it.
- **Account Settings: Read** — lets the conductor resolve your account ID (required by the CF API).

The token **cannot** read DNS records, edit user account settings, change billing, or access any other Cloudflare product. If your token has more permissions than these two, you're granting the conductor more authority than it needs.

## GitHub PAT: how to create

### Fine-grained tokens (recommended)

Go to [github.com/settings/tokens?type=beta](https://github.com/settings/tokens?type=beta) → **Generate new token**.

1. **Token name:** something descriptive, e.g. `prismconductor-myrepo`
2. **Expiration:** 90 days maximum recommended; shorter is safer.
3. **Repository access:** select **Only select repositories** → choose the repos the conductor will work on.
4. **Repository permissions:**

| Permission | Level |
|--|--|
| Contents | Read and write |
| Pull requests | Read and write |
| Metadata | Read (mandatory — can't opt out) |

### Classic tokens (broader; use only if org policy requires)

Go to [github.com/settings/tokens/new](https://github.com/settings/tokens/new).

Select the `repo` scope. This grants more access than fine-grained (includes all private repo access across your account), so only use this if your GitHub organization's settings block fine-grained tokens.

### Token expiry

When a PAT expires, agent cards that would require a GitHub push return to PLAN state with a TOKEN EXPIRED badge. Go to **Settings → Workspaces → Auth → Replace GitHub PAT** and paste the new token. The conductor calls the CF API to overwrite the `GITHUB_PAT` Secret on your worker — no redeploy needed.

**GitHub Enterprise Server:** Fine-grained tokens are supported on GHES 3.10+. If you're on an older version, use classic tokens. The token endpoint differs (`https://GHES_HOST/settings/tokens`) — out of scope for v1 setup, but the conductor will surface an error if the PAT endpoint returns a non-GitHub.com response.

## Where each secret is stored

The conductor handles three secrets for remote workspaces:

| Secret | Lives in | Conductor reads it… |
|--|--|--|
| **Cloudflare API token** | Your OS keychain (macOS Keychain / Windows Credential Vault / Linux Secret Service) | Whenever it talks to the CF API (deploy, rotate, teardown) |
| **GitHub PAT** | Cloudflare Secrets (bound to your worker) | Never — only the worker reads it via `env.GITHUB_PAT` |
| **Conductor ↔ Worker API key** | Both your OS keychain AND CF Secrets | Conductor reads from keychain; worker reads from `env.CONDUCTOR_API_KEY` |

### What the conductor's local files do NOT contain

- `~/Library/Application Support/PrismConductor/conductor.db` — workspace metadata only; no tokens.
- `~/Library/Application Support/PrismConductor/transcripts/*.log` — agent transcripts; no tokens (token-shaped strings are redacted).
- App logs — tokens are never logged. If you see a token-shaped string in a log file, file a bug — that's a leak.

**What's checked into your repo:** Nothing. The conductor's config is local-only and never touches your repository.

## Rotating tokens

### Cloudflare API token expired or compromised

**Settings → Workspaces → [name] → Auth → Replace Cloudflare token**

Paste the new token. The conductor verifies it against the CF API and writes it to your OS keychain. No redeploy needed unless the underlying worker was deleted.

### GitHub PAT expired or compromised

**Settings → Workspaces → [name] → Auth → Replace GitHub PAT**

Paste the new token. The conductor calls the CF API to overwrite the `GITHUB_PAT` Secret on the worker. The old PAT is immediately invalid for new agent runs; any in-flight run that already loaded the old PAT will fail on its next push.

### Conductor ↔ Worker API key suspected leaked

**Settings → Workspaces → [name] → Auth → Rotate worker API key**

The conductor generates a new 256-bit key, writes it to both CF Secrets and your local keychain atomically. Any request carrying the old key returns HTTP 401 immediately.

## Tearing down a remote workspace

**Settings → Workspaces → [name] → Delete**

1. Conductor reads your CF token from the OS keychain.
2. Calls `DELETE /accounts/:id/workers/scripts/<name>` to remove the worker from your CF account.
3. Deletes the keychain entries (CF token and worker API key).
4. Removes the workspace entry from the local database.

After deletion: nothing referencing this workspace remains on your CF account or local machine. The GitHub PAT you supplied was stored only as a CF Secret — it is deleted with the worker.

## Threat model

### What the conductor protects against

- **Random internet traffic reaching your worker:** The worker rejects unauthenticated requests. Every conductor ↔ worker call carries the shared API key; requests without it get HTTP 401.
- **Tokens leaking via process memory dumps:** Tokens spend minimal time in process memory. Persistent storage is your OS keychain, not files or environment variables in the main process.
- **Tokens accidentally committed to git:** Nothing the conductor writes ends up in your repository. Workspace config is local-only.

### What it does NOT protect against

- **Compromise of your local OS user account.** Anyone with access to your unlocked keychain has access to your CF token and worker API key. This is the standard OS-keychain trust boundary — the conductor inherits it, not bypasses it.
- **Compromise of your Cloudflare account credentials.** If someone has your CF login, they can read or delete the worker and its secrets regardless of the conductor.
- **Malicious code in skills or pipelines.** Skills run inside the Cloudflare Worker with the worker's auth. A malicious skill could exfiltrate data or make API calls using the worker's identity. Only install skills from sources you trust.
- **Cloudflare infrastructure compromise.** Your GitHub PAT is stored in CF Secrets. If Cloudflare's Secret storage were compromised, so would your PAT. This is no different from using Cloudflare Workers for any other secret-dependent workload.

## Screenshots and version notes

Cloudflare's dashboard UI changes over time. The permission names above reflect the CF API v4 token model as of 2026; the form labels may differ slightly in newer dashboard versions but the underlying permission scopes are stable. If a label no longer matches, the [CF API token documentation](https://developers.cloudflare.com/api/tokens/) is authoritative.
