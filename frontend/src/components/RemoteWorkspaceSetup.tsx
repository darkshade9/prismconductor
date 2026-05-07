import { useState } from "react";
import { CreateRemoteWorkspace, GetRepoDefaultBranch, TestCloudflareToken, TestGitHubPAT } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";
import { deriveWorkspaceID, parseRepoURL } from "../lib/parseRepoURL";

type Step = "cf-token" | "github-pat" | "repo" | "deploy" | "done";

export const COLOR_PALETTE = ["#22c55e", "#06b6d4", "#a855f7", "#f59e0b", "#ef4444", "#ec4899", "#14b8a6", "#eab308"];

export const STEPS: { id: Step; label: string }[] = [
  { id: "cf-token", label: "CF Token" },
  { id: "github-pat", label: "GitHub PAT" },
  { id: "repo", label: "Repository" },
  { id: "deploy", label: "Deploy" },
  { id: "done", label: "Done" },
];

export const CF_REQUIRED_SCOPES = [
  { category: "Account → Workers Scripts", permission: "Edit" },
  { category: "Account → Account Settings", permission: "Read" },
] as const;

export const GITHUB_FINE_GRAINED_SCOPES = [
  { permission: "Contents", level: "Read and write" },
  { permission: "Pull requests", level: "Read and write" },
  { permission: "Metadata", level: "Read (mandatory)" },
] as const;

export function RemoteWorkspaceSetup({ onDone }: { onDone: () => void }) {
  const refresh = useWorkspaceStore((s) => s.refresh);
  const existingWorkspaces = useWorkspaceStore((s) => s.workspaces);

  const [step, setStep] = useState<Step>("cf-token");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [helpOpen, setHelpOpen] = useState(false);

  // Step 1 — CF token
  const [cfToken, setCFToken] = useState("");
  const [cfResult, setCFResult] = useState<main.CFTokenResult | null>(null);

  // Step 2 — GitHub PAT
  const [githubPAT, setGitHubPAT] = useState("");

  // Step 3 — repo identity (derived from a single URL input)
  const [repoURL, setRepoURL] = useState("");
  const [urlError, setURLError] = useState<string | null>(null);
  const [owner, setOwner] = useState("");
  const [repo, setRepo] = useState("");
  const [defaultBranch, setDefaultBranch] = useState("");
  const [branchFetchState, setBranchFetchState] = useState<"idle" | "loading" | "error" | "done">("idle");
  const [branchFetchError, setBranchFetchError] = useState<string | null>(null);
  const [wsID, setWsID] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [color, setColor] = useState(COLOR_PALETTE[1]);

  // Step 4 — deploy
  const [deployResult, setDeployResult] = useState<main.RemoteDeployResult | null>(null);

  // ---

  async function handleRepoURLChange(raw: string) {
    setRepoURL(raw);
    setURLError(null);
    setBranchFetchError(null);

    const parsed = parseRepoURL(raw);
    if (!parsed) {
      if (raw.trim()) setURLError("Invalid GitHub URL — use https://github.com/owner/repo or git@github.com:owner/repo");
      setOwner("");
      setRepo("");
      setDefaultBranch("");
      setBranchFetchState("idle");
      return;
    }

    setOwner(parsed.owner);
    setRepo(parsed.repo);

    // Auto-suggest workspace ID and display name immediately.
    const existingIDs = existingWorkspaces.map((w) => w.id);
    setWsID((prev) => (!prev || prev === deriveWorkspaceID(owner, repo, existingIDs)) ? deriveWorkspaceID(parsed.owner, parsed.repo, existingIDs) : prev);
    setDisplayName((prev) => (!prev || prev === `${owner}/${repo}`) ? `${parsed.owner}/${parsed.repo}` : prev);

    // Fetch default branch from GitHub (requires PAT).
    if (!githubPAT.trim()) {
      setBranchFetchState("idle");
      return;
    }

    setBranchFetchState("loading");
    try {
      const branch = await GetRepoDefaultBranch(githubPAT.trim(), parsed.owner, parsed.repo);
      setDefaultBranch(branch);
      setBranchFetchState("done");
    } catch (e: any) {
      setBranchFetchState("error");
      setBranchFetchError(String(e?.message ?? e));
    }
  }

  async function testCFToken() {
    setError(null);
    setBusy(true);
    try {
      const r = await TestCloudflareToken(cfToken.trim());
      setCFResult(r);
      setStep("github-pat");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function testGitHubPAT() {
    setError(null);
    setStep("repo");
  }

  async function testPATWithRepo() {
    const parsed = parseRepoURL(repoURL);
    if (!parsed) {
      setError("Enter a valid GitHub repository URL first.");
      return;
    }
    if (!wsID) {
      setError("Workspace ID is required.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await TestGitHubPAT(githubPAT.trim(), parsed.owner, parsed.repo);
      setStep("deploy");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function deploy() {
    if (!wsID) {
      setError("Workspace ID is required.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      // Single atomic call: CF deploy + secret upload + API key + registry row.
      // The registry row is only written after all remote steps succeed, so a
      // failure at any point leaves no zombie workspace row (issue #192).
      const dr = await CreateRemoteWorkspace(new main.RemoteWorkspaceForm({
        cf_token: cfToken.trim(),
        github_pat: githubPAT.trim(),
        workspace_id: wsID,
        display_name: displayName || wsID,
        github_owner: owner,
        github_repo: repo,
        default_branch: defaultBranch || "main",
        color,
      }));
      setDeployResult(dr);
      await refresh();
      setStep("done");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  // ---

  return (
    <div className="space-y-4 text-sm">
      <div className="flex items-center justify-between">
        <StepIndicator current={step} />
        <button
          onClick={() => setHelpOpen(true)}
          title="Setup guide"
          className="w-5 h-5 rounded-full bg-slate-700 hover:bg-slate-600 text-slate-300 text-[10px] font-bold flex items-center justify-center shrink-0"
        >
          ?
        </button>
      </div>

      {step === "cf-token" && (
        <div className="space-y-3">
          <div className="text-xs text-slate-400">
            Enter your Cloudflare API token. It needs{" "}
            <span className="text-slate-200">Workers Scripts:Edit</span> and{" "}
            <span className="text-slate-200">Account Settings:Read</span> permissions.
          </div>
          <label className="block">
            <FieldLabel
              label="Cloudflare API Token"
              tip={
                <div className="space-y-2">
                  <p className="font-medium text-slate-200">Required scopes:</p>
                  <ul className="space-y-0.5">
                    {CF_REQUIRED_SCOPES.map((s) => (
                      <li key={s.category}>
                        • {s.category} → <strong>{s.permission}</strong>
                      </li>
                    ))}
                  </ul>
                  <a
                    href="https://dash.cloudflare.com/profile/api-tokens"
                    target="_blank"
                    rel="noreferrer"
                    className="block text-sky-400 hover:underline"
                  >
                    Create a token now →
                  </a>
                </div>
              }
            />
            <input
              {...noAutoCorrect}
              type="password"
              value={cfToken}
              onChange={(e) => setCFToken(e.target.value)}
              placeholder="cf_api_token_..."
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1.5 text-slate-200 font-mono text-xs"
            />
          </label>
          {error && <div className="text-red-400 text-xs">{error}</div>}
          <div className="flex justify-between pt-1">
            <button onClick={onDone} className="text-xs text-slate-500 hover:text-slate-300">
              Cancel
            </button>
            <button
              onClick={testCFToken}
              disabled={!cfToken.trim() || busy}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded text-xs disabled:opacity-40"
            >
              {busy ? "Verifying…" : "Verify & Continue →"}
            </button>
          </div>
        </div>
      )}

      {step === "github-pat" && (
        <div className="space-y-3">
          {cfResult && (
            <div className="text-xs text-emerald-400">
              ✓ CF account: {cfResult.account_name || cfResult.account_id}
            </div>
          )}
          <div className="text-xs text-slate-400">
            Enter a GitHub PAT with <span className="text-slate-200">repo</span> scope. It will be
            stored as a Cloudflare Secret — never on disk.
          </div>
          <label className="block">
            <FieldLabel
              label="GitHub Personal Access Token"
              tip={
                <div className="space-y-2">
                  <p className="font-medium text-slate-200">Fine-grained (recommended):</p>
                  <ul className="space-y-0.5">
                    {GITHUB_FINE_GRAINED_SCOPES.map((s) => (
                      <li key={s.permission}>
                        • {s.permission} → <strong>{s.level}</strong>
                      </li>
                    ))}
                  </ul>
                  <p className="font-medium text-slate-200">Classic (broader):</p>
                  <ul>
                    <li>
                      • <code className="bg-slate-700 px-0.5 rounded">repo</code> scope
                    </li>
                  </ul>
                  <a
                    href="https://github.com/settings/tokens?type=beta"
                    target="_blank"
                    rel="noreferrer"
                    className="block text-sky-400 hover:underline"
                  >
                    Create a fine-grained token →
                  </a>
                </div>
              }
            />
            <input
              {...noAutoCorrect}
              type="password"
              value={githubPAT}
              onChange={(e) => setGitHubPAT(e.target.value)}
              placeholder="ghp_..."
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1.5 text-slate-200 font-mono text-xs"
            />
          </label>
          <div className="text-xs text-slate-500">
            You can verify push access on the next step once you enter the repo.
          </div>
          {error && <div className="text-red-400 text-xs">{error}</div>}
          <div className="flex justify-between pt-1">
            <button onClick={() => setStep("cf-token")} className="text-xs text-slate-500 hover:text-slate-300">
              ← Back
            </button>
            <button
              onClick={() => setStep("repo")}
              disabled={!githubPAT.trim() || busy}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded text-xs disabled:opacity-40"
            >
              Continue →
            </button>
          </div>
        </div>
      )}

      {step === "repo" && (
        <div className="space-y-3">
          <div className="text-xs text-slate-400">
            Paste the GitHub repository URL — owner, repo, and default branch will be filled in automatically.
          </div>
          <label className="block">
            <FieldLabel
              label="Repository URL"
              tip={
                <div className="space-y-1">
                  <p>Paste any GitHub URL form:</p>
                  <ul className="space-y-0.5 font-mono text-[11px]">
                    <li>https://github.com/owner/repo</li>
                    <li>https://github.com/owner/repo.git</li>
                    <li>git@github.com:owner/repo.git</li>
                  </ul>
                </div>
              }
            />
            <input
              {...noAutoCorrect}
              value={repoURL}
              onChange={(e) => handleRepoURLChange(e.target.value)}
              placeholder="https://github.com/owner/repo"
              className={
                "w-full bg-slate-800 border rounded px-2 py-1.5 text-slate-200 font-mono text-xs " +
                (urlError ? "border-red-500" : "border-slate-700")
              }
            />
            {urlError && <div className="text-red-400 text-xs mt-1">{urlError}</div>}
          </label>

          {owner && repo && (
            <div className="text-xs text-slate-500 font-mono">
              {owner} / {repo}
            </div>
          )}

          <label className="block">
            <div className="flex items-center gap-1 text-xs text-slate-500 mb-1">
              Default branch
              {branchFetchState === "loading" && (
                <span className="text-slate-500 animate-pulse">fetching…</span>
              )}
              {branchFetchState === "done" && (
                <span className="text-emerald-400">✓ fetched</span>
              )}
            </div>
            <input
              {...noAutoCorrect}
              value={defaultBranch}
              onChange={(e) => setDefaultBranch(e.target.value)}
              placeholder="main"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
            />
            {branchFetchState === "error" && branchFetchError && (
              <div className="text-amber-400 text-xs mt-1">
                Could not fetch default branch — {branchFetchError}. Enter it manually.
              </div>
            )}
          </label>

          <div className="grid grid-cols-2 gap-2">
            <label className="block">
              <FieldLabel
                label="Workspace ID"
                tip={<p>A unique slug for this workspace, e.g. <code className="bg-slate-700 px-0.5 rounded">my-repo</code>. Used as part of the CF Worker name — lowercase letters, numbers, and hyphens only.</p>}
              />
              <input
                {...noAutoCorrect}
                value={wsID}
                onChange={(e) => setWsID(e.target.value)}
                placeholder="my-repo"
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Display name</div>
              <input
                {...noAutoCorrect}
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="owner/repo"
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
          </div>

          <div>
            <div className="text-xs text-slate-500 mb-1">Card color</div>
            <div className="flex gap-1">
              {COLOR_PALETTE.map((c) => (
                <button
                  key={c}
                  onClick={() => setColor(c)}
                  style={{ backgroundColor: c }}
                  className={"w-6 h-6 rounded-full border-2 " + (color === c ? "border-white" : "border-transparent")}
                />
              ))}
            </div>
          </div>

          {error && <div className="text-red-400 text-xs">{error}</div>}
          <div className="flex justify-between pt-1">
            <button onClick={() => setStep("github-pat")} className="text-xs text-slate-500 hover:text-slate-300">
              ← Back
            </button>
            <button
              onClick={testPATWithRepo}
              disabled={!owner || !repo || !wsID || busy}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded text-xs disabled:opacity-40"
            >
              {busy ? "Verifying…" : "Verify PAT & Continue →"}
            </button>
          </div>
        </div>
      )}

      {step === "deploy" && (
        <div className="space-y-3">
          <div className="text-xs text-emerald-400">✓ GitHub PAT verified for {owner}/{repo}</div>
          <div className="rounded border border-slate-700 bg-slate-900 p-3 text-xs text-slate-400 space-y-1">
            <div><span className="text-slate-300">Account:</span> {cfResult?.account_name || cfResult?.account_id}</div>
            <div><span className="text-slate-300">Repo:</span> {owner}/{repo} @ {defaultBranch}</div>
            <div><span className="text-slate-300">Worker name:</span> prismconductor-{wsID.slice(0, 30).toLowerCase().replace(/[^a-z0-9]/g, "-")}</div>
            <div className="text-slate-500 mt-1">
              The worker bundle will be uploaded to your CF account. The GitHub PAT will be stored as a CF Secret (GITHUB_PAT) — never saved locally.
            </div>
          </div>
          {error && (
            <div className="space-y-1">
              <div className="text-red-400 text-xs">{error}</div>
              {error.includes("no Workers subdomain configured") && (
                <div className="text-xs text-slate-400">
                  Visit{" "}
                  <a
                    href="https://dash.cloudflare.com"
                    target="_blank"
                    rel="noreferrer"
                    className="text-sky-400 hover:underline"
                  >
                    dash.cloudflare.com
                  </a>
                  {" "}→ Workers &amp; Pages → Choose a subdomain, then retry.
                </div>
              )}
            </div>
          )}
          <div className="flex justify-between pt-1">
            <button onClick={() => setStep("repo")} className="text-xs text-slate-500 hover:text-slate-300">
              ← Back
            </button>
            <button
              onClick={deploy}
              disabled={busy || (!!error && error.includes("no Workers subdomain configured"))}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded text-xs disabled:opacity-40"
            >
              {busy ? "Deploying…" : "Deploy & Create Workspace"}
            </button>
          </div>
        </div>
      )}

      {step === "done" && deployResult && (
        <div className="space-y-3">
          <div className="text-xs text-emerald-400 font-medium">Remote workspace created!</div>
          <div className="rounded border border-slate-700 bg-slate-900 p-3 text-xs space-y-1">
            <div className="text-slate-400">
              <span className="text-slate-300">Worker:</span>{" "}
              <a
                href={deployResult.cf_worker_endpoint_url}
                target="_blank"
                rel="noreferrer"
                className="text-sky-400 hover:underline font-mono"
              >
                {deployResult.cf_worker_endpoint_url}
              </a>
            </div>
            <div className="text-slate-400">
              <span className="text-slate-300">Version:</span>{" "}
              <span className="font-mono text-slate-300">{deployResult.deployment_version || "deployed"}</span>
            </div>
          </div>
          <div className="text-xs text-slate-500">
            Agent work for this workspace will now run inside the Cloudflare Worker. To update tokens later, go to Settings → Workspaces → Auth.
          </div>
          <div className="flex justify-end pt-1">
            <button onClick={onDone} className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded text-xs">
              Done
            </button>
          </div>
        </div>
      )}

      {helpOpen && <HelpDrawer onClose={() => setHelpOpen(false)} />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step indicator
// ---------------------------------------------------------------------------

function StepIndicator({ current }: { current: Step }) {
  const currentIdx = STEPS.findIndex((s) => s.id === current);
  return (
    <div className="flex items-center gap-1 text-[10px]">
      {STEPS.map((s, i) => {
        const done = i < currentIdx;
        const active = i === currentIdx;
        return (
          <div key={s.id} className="flex items-center gap-1">
            <span
              className={
                done
                  ? "text-emerald-400"
                  : active
                  ? "text-sky-300 font-medium"
                  : "text-slate-600"
              }
            >
              {done ? "✓ " : ""}{s.label}
            </span>
            {i < STEPS.length - 1 && <span className="text-slate-700">→</span>}
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Field label with hover tooltip
// ---------------------------------------------------------------------------

function FieldLabel({ label, tip }: { label: string; tip: React.ReactNode }) {
  const [show, setShow] = useState(false);
  return (
    <div className="relative flex items-center gap-1 text-xs text-slate-500 mb-1">
      {label}
      <span
        className="cursor-help select-none text-slate-600 hover:text-slate-400"
        onMouseEnter={() => setShow(true)}
        onMouseLeave={() => setShow(false)}
      >
        ⓘ
      </span>
      {show && (
        <div className="absolute bottom-full left-0 mb-1 z-50 w-64 bg-slate-800 border border-slate-600 rounded p-2 text-xs text-slate-300 shadow-xl">
          {tip}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Help drawer — full-screen overlay with setup guide
// ---------------------------------------------------------------------------

function HelpDrawer({ onClose }: { onClose: () => void }) {
  return (
    <div
      className="fixed inset-0 z-50 flex"
      onClick={onClose}
    >
      <div className="flex-1 bg-black/40" />
      <div
        className="w-96 bg-slate-900 border-l border-slate-700 overflow-y-auto flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700 sticky top-0 bg-slate-900">
          <span className="text-sm font-medium text-slate-200">Remote Workspace Guide</span>
          <button
            onClick={onClose}
            className="text-slate-500 hover:text-slate-300 text-sm leading-none"
          >
            ✕
          </button>
        </div>

        <div className="p-4 space-y-5 text-xs text-slate-300">

          {/* Local vs Remote */}
          <HelpSection title="Local vs remote">
            <table className="w-full text-[11px] border-collapse">
              <thead>
                <tr className="text-slate-400">
                  <th className="text-left pb-1 font-normal">Concern</th>
                  <th className="text-left pb-1 font-normal">Local</th>
                  <th className="text-left pb-1 font-normal">Remote</th>
                </tr>
              </thead>
              <tbody className="text-slate-300">
                {[
                  ["Survives laptop sleep", "No", "Yes"],
                  ["GitHub auth", "Local SSH/gh", "PAT you supply"],
                  ["Privacy", "Code stays local", "Clones to CF"],
                  ["Setup time", "Add repo path", "~5 min"],
                  ["Best for", "Quick, sensitive", "Long runs, big repos"],
                ].map(([concern, local, remote]) => (
                  <tr key={concern} className="border-t border-slate-800">
                    <td className="py-0.5 pr-2 text-slate-400">{concern}</td>
                    <td className="py-0.5 pr-2">{local}</td>
                    <td className="py-0.5">{remote}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </HelpSection>

          {/* CF Token */}
          <HelpSection title="Cloudflare API token">
            <p className="text-slate-400 mb-2">
              Go to{" "}
              <a
                href="https://dash.cloudflare.com/profile/api-tokens"
                target="_blank"
                rel="noreferrer"
                className="text-sky-400 hover:underline"
              >
                dash.cloudflare.com/profile/api-tokens
              </a>{" "}
              → Create Token → Custom Token.
            </p>
            <p className="font-medium text-slate-200 mb-1">Required permissions:</p>
            <ul className="space-y-0.5">
              {CF_REQUIRED_SCOPES.map((s) => (
                <li key={s.category}>
                  • {s.category} → <strong>{s.permission}</strong>
                </li>
              ))}
            </ul>
            <p className="text-slate-400 mt-2">
              Account resources: All accounts. Zone resources: not needed.
            </p>
            <p className="text-slate-500 mt-2">
              These scopes let the conductor deploy and update the worker bundle. The token cannot read DNS, edit billing, or touch other CF products.
            </p>
          </HelpSection>

          {/* GitHub PAT */}
          <HelpSection title="GitHub PAT">
            <p className="font-medium text-slate-200 mb-1">Fine-grained (recommended):</p>
            <p className="text-slate-400 mb-1">
              <a
                href="https://github.com/settings/tokens?type=beta"
                target="_blank"
                rel="noreferrer"
                className="text-sky-400 hover:underline"
              >
                github.com/settings/tokens?type=beta
              </a>{" "}
              → Generate new token
            </p>
            <ul className="space-y-0.5 mb-2">
              {GITHUB_FINE_GRAINED_SCOPES.map((s) => (
                <li key={s.permission}>
                  • {s.permission} → <strong>{s.level}</strong>
                </li>
              ))}
            </ul>
            <p className="font-medium text-slate-200 mb-1">Classic (if org requires):</p>
            <ul className="mb-2">
              <li>• <code className="bg-slate-800 px-0.5 rounded">repo</code> scope (broader — covers all private repos)</li>
            </ul>
            <p className="text-slate-500">
              Expiration: 90 days max recommended. When expired, cards return to PLAN with TOKEN EXPIRED badge — paste a new PAT in Settings → Workspaces → Auth.
            </p>
          </HelpSection>

          {/* Where secrets live */}
          <HelpSection title="Where secrets are stored">
            <table className="w-full text-[11px] border-collapse">
              <thead>
                <tr className="text-slate-400">
                  <th className="text-left pb-1 font-normal">Secret</th>
                  <th className="text-left pb-1 font-normal">Stored in</th>
                </tr>
              </thead>
              <tbody className="text-slate-300">
                {[
                  ["CF API token", "OS keychain"],
                  ["GitHub PAT", "CF Secrets (worker only)"],
                  ["Worker API key", "OS keychain + CF Secrets"],
                ].map(([secret, store]) => (
                  <tr key={secret} className="border-t border-slate-800">
                    <td className="py-0.5 pr-2 text-slate-200">{secret}</td>
                    <td className="py-0.5">{store}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="text-slate-500 mt-2">
              Nothing is written to disk as plaintext. No tokens end up in your repo. Transcripts redact token-shaped strings.
            </p>
          </HelpSection>

          {/* Rotating tokens */}
          <HelpSection title="Rotating tokens">
            <ul className="space-y-1.5">
              <li>
                <span className="font-medium text-slate-200">CF token expired:</span>
                <span className="text-slate-400"> Settings → Workspaces → Auth → Replace Cloudflare token</span>
              </li>
              <li>
                <span className="font-medium text-slate-200">GitHub PAT expired:</span>
                <span className="text-slate-400"> Settings → Workspaces → Auth → Replace GitHub PAT (updates CF Secret, no redeploy)</span>
              </li>
              <li>
                <span className="font-medium text-slate-200">Worker API key suspected leaked:</span>
                <span className="text-slate-400"> Settings → Workspaces → Auth → Rotate worker API key (generates new 256-bit key, old key returns 401 immediately)</span>
              </li>
            </ul>
          </HelpSection>

          {/* Tearing down */}
          <HelpSection title="Tearing down">
            <p className="text-slate-400 mb-1">Settings → Workspaces → [name] → Delete:</p>
            <ol className="space-y-0.5 list-decimal list-inside text-slate-300">
              <li>Reads CF token from keychain</li>
              <li>Calls CF API to delete the worker</li>
              <li>Deletes keychain entries (CF token, worker API key)</li>
              <li>Removes workspace from local database</li>
            </ol>
            <p className="text-slate-500 mt-2">
              After deletion: nothing referencing this workspace remains on your CF account or machine.
            </p>
          </HelpSection>

          {/* Threat model */}
          <HelpSection title="Threat model">
            <p className="font-medium text-slate-200 mb-1">Protected against:</p>
            <ul className="space-y-0.5 text-slate-400 mb-2">
              <li>• Random traffic reaching your worker (API key auth on every request)</li>
              <li>• Tokens leaking via process memory (OS keychain, not files)</li>
              <li>• Tokens committed to git (conductor config is local-only)</li>
            </ul>
            <p className="font-medium text-slate-200 mb-1">NOT protected against:</p>
            <ul className="space-y-0.5 text-slate-400">
              <li>• Compromise of your local OS user account (unlocked keychain = access to tokens)</li>
              <li>• Compromise of your CF account credentials</li>
              <li>• Malicious skills — they run with the worker's auth; only install skills from sources you trust</li>
            </ul>
          </HelpSection>

          <div className="pt-1 border-t border-slate-800">
            <a
              href="https://github.com/darkshade9/prismconductor/blob/main/docs/remote-workspaces.md"
              target="_blank"
              rel="noreferrer"
              className="text-sky-400 hover:underline text-[11px]"
            >
              Full reference docs →
            </a>
          </div>
        </div>
      </div>
    </div>
  );
}

function HelpSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="text-[11px] font-semibold text-slate-200 uppercase tracking-wide mb-2">{title}</h3>
      {children}
    </div>
  );
}
