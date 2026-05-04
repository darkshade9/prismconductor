import { useState } from "react";
import { AddWorkspace, DeployRemoteWorker, TestCloudflareToken, TestGitHubPAT } from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

type Step = "cf-token" | "github-pat" | "repo" | "deploy" | "done";

const COLOR_PALETTE = ["#22c55e", "#06b6d4", "#a855f7", "#f59e0b", "#ef4444", "#ec4899", "#14b8a6", "#eab308"];

export function RemoteWorkspaceSetup({ onDone }: { onDone: () => void }) {
  const refresh = useWorkspaceStore((s) => s.refresh);

  const [step, setStep] = useState<Step>("cf-token");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Step 1 — CF token
  const [cfToken, setCFToken] = useState("");
  const [cfResult, setCFResult] = useState<main.CFTokenResult | null>(null);

  // Step 2 — GitHub PAT
  const [githubPAT, setGitHubPAT] = useState("");
  const [patVerified, setPATVerified] = useState(false);

  // Step 3 — repo identity
  const [owner, setOwner] = useState("");
  const [repo, setRepo] = useState("");
  const [defaultBranch, setDefaultBranch] = useState("main");
  const [wsID, setWsID] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [color, setColor] = useState(COLOR_PALETTE[1]);

  // Step 4 — deploy
  const [deployResult, setDeployResult] = useState<main.RemoteDeployResult | null>(null);

  // ---

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
    if (!owner || !repo) {
      setStep("repo");
      return;
    }
    setBusy(true);
    try {
      await TestGitHubPAT(githubPAT.trim(), owner, repo);
      setPATVerified(true);
      setStep("repo");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function testPATWithRepo() {
    if (!owner || !repo) {
      setError("Owner and repo are required.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await TestGitHubPAT(githubPAT.trim(), owner, repo);
      setPATVerified(true);
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
      // First create a skeleton local workspace so DeployRemoteWorker can look it up.
      const skeleton = new types.Workspace({
        id: wsID,
        display_name: displayName || wsID,
        repo_path: "",
        github_owner: owner,
        github_repo: repo,
        default_branch: defaultBranch || "main",
        color,
        agent_env: { env_vars: {}, pre_commands: [], shell: "/bin/bash" },
        skill_profile: { mode: "bundled" },
        conventions: {},
        enabled: true,
        execution_target: "remote",
      });
      await AddWorkspace(skeleton);
      // Then deploy the CF Worker and persist RemoteConfig.
      const dr = await DeployRemoteWorker(wsID, cfToken.trim(), githubPAT.trim());
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
      <StepIndicator current={step} />

      {step === "cf-token" && (
        <div className="space-y-3">
          <div className="text-xs text-slate-400">
            Enter your Cloudflare API token. It needs{" "}
            <span className="text-slate-200">Workers Scripts:Edit</span> and{" "}
            <span className="text-slate-200">Account Settings:Read</span> permissions.
          </div>
          <label className="block">
            <div className="text-xs text-slate-500 mb-1">Cloudflare API Token</div>
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
            <div className="text-xs text-slate-500 mb-1">GitHub Personal Access Token</div>
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
            Identify the GitHub repository this workspace will work on.
          </div>
          <div className="grid grid-cols-2 gap-2">
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Owner</div>
              <input
                {...noAutoCorrect}
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                placeholder="octocat"
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Repository</div>
              <input
                {...noAutoCorrect}
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                placeholder="my-repo"
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
          </div>
          <label className="block">
            <div className="text-xs text-slate-500 mb-1">Default branch</div>
            <input
              {...noAutoCorrect}
              value={defaultBranch}
              onChange={(e) => setDefaultBranch(e.target.value)}
              placeholder="main"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
            />
          </label>
          <div className="grid grid-cols-2 gap-2">
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Workspace ID</div>
              <input
                {...noAutoCorrect}
                value={wsID}
                onChange={(e) => setWsID(e.target.value)}
                placeholder="my-repo-remote"
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Display name</div>
              <input
                {...noAutoCorrect}
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="My Repo (Remote)"
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
          {error && <div className="text-red-400 text-xs">{error}</div>}
          <div className="flex justify-between pt-1">
            <button onClick={() => setStep("repo")} className="text-xs text-slate-500 hover:text-slate-300">
              ← Back
            </button>
            <button
              onClick={deploy}
              disabled={busy}
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
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step indicator
// ---------------------------------------------------------------------------

const STEPS: { id: Step; label: string }[] = [
  { id: "cf-token", label: "CF Token" },
  { id: "github-pat", label: "GitHub PAT" },
  { id: "repo", label: "Repository" },
  { id: "deploy", label: "Deploy" },
  { id: "done", label: "Done" },
];

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
