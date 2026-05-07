import { useEffect, useState } from "react";
import { DeployRemoteWorker, GCWorktrees, ListPools, RemoveWorkspace, RotateRemoteWorkerKey, RunAutoArchiveNow, UpdateWorkspace } from "../../wailsjs/go/main/App";
import { types, workerpool } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { AddWorkspaceModal } from "./AddWorkspaceModal";
import { PipelineEditor } from "./PipelineEditor";
import { SkillProfileEditor } from "./SkillProfileEditor";
import { LabelsPanel } from "./LabelsPanel";
import { noAutoCorrect } from "../lib/inputs";

const SIGNING_OPTIONS: { value: string; label: string; description: string }[] = [
  { value: "auto",       label: "Auto (recommended)", description: "Try GitHub API → local signing → manual fallback" },
  { value: "github_api", label: "GitHub API only",    description: "Only use GitHub API commits; emit NEEDS_PR if that fails" },
  { value: "local",      label: "Local signing only", description: "Only attempt local git commit -S + push" },
  { value: "manual",     label: "Manual always",      description: "Never attempt automation; always prepare a worktree command" },
];

function SigningStrategyEditor({ workspace, onSave }: { workspace: types.Workspace; onSave: () => void }) {
  const current = workspace.signing_strategy || "auto";

  async function save(strategy: string) {
    const updated = types.Workspace.createFrom({
      ...workspace,
      signing_strategy: strategy === "auto" ? "" : strategy,
    });
    await UpdateWorkspace(updated);
    onSave();
  }

  return (
    <div>
      <div className="text-xs text-slate-400 mb-2">Commit signing strategy</div>
      <div className="flex flex-col gap-1.5">
        {SIGNING_OPTIONS.map((opt) => (
          <label key={opt.value} className="flex items-start gap-2 text-xs cursor-pointer">
            <input
              type="radio"
              name={`signing-${workspace.id}`}
              value={opt.value}
              checked={current === opt.value}
              onChange={() => save(opt.value)}
              className="mt-0.5"
            />
            <span>
              <span className="text-slate-200">{opt.label}</span>
              <span className="text-slate-500 ml-1">— {opt.description}</span>
            </span>
          </label>
        ))}
      </div>
    </div>
  );
}

const COMPLEXITY_SCALE_OPTIONS: { value: string; label: string; description: string; advanced?: boolean }[] = [
  { value: "",         label: "T-shirt (default)", description: "XS, S, M, L, XL — intuitive for most teams" },
  { value: "fibonacci", label: "Fibonacci",         description: "1, 2, 3, 5, 8, 13, 21, ? — more granular; use ? when work needs breakdown" },
  { value: "linear10",  label: "Linear 1–10",       description: "1–10 integer scale — only recommended when teams have calibrated baselines", advanced: true },
];

function ComplexityScaleEditor({ workspace, onSave }: { workspace: types.Workspace; onSave: () => void }) {
  const current = workspace.complexity_scale ?? "";
  const [showAdvanced, setShowAdvanced] = useState(current === "linear10");

  async function save(scale: string) {
    const updated = types.Workspace.createFrom({
      ...workspace,
      complexity_scale: scale === "" ? undefined : scale,
    });
    await UpdateWorkspace(updated);
    onSave();
  }

  const visibleOptions = COMPLEXITY_SCALE_OPTIONS.filter((opt) => !opt.advanced || showAdvanced);

  return (
    <div>
      <div className="text-xs text-slate-400 mb-2">Complexity scale</div>
      <div className="flex flex-col gap-1.5">
        {visibleOptions.map((opt) => (
          <label key={opt.value} className="flex items-start gap-2 text-xs cursor-pointer">
            <input
              type="radio"
              name={`complexity-scale-${workspace.id}`}
              value={opt.value}
              checked={current === opt.value}
              onChange={() => save(opt.value)}
              className="mt-0.5"
            />
            <span>
              <span className="text-slate-200">{opt.label}</span>
              <span className="text-slate-500 ml-1">— {opt.description}</span>
            </span>
          </label>
        ))}
        {!showAdvanced && (
          <button
            onClick={() => setShowAdvanced(true)}
            className="text-xs text-slate-500 hover:text-slate-400 text-left mt-0.5"
          >
            Show advanced options
          </button>
        )}
      </div>
    </div>
  );
}

function AutoArchiveEditor({ workspace, onSave }: { workspace: types.Workspace; onSave: () => void }) {
  const cfg = workspace.auto_archive ?? { enabled: false, days_closed: 7 };
  const [enabled, setEnabled] = useState(cfg.enabled);
  const [days, setDays] = useState(cfg.days_closed || 7);
  const [busy, setBusy] = useState(false);

  async function save(nextEnabled: boolean, nextDays: number) {
    const updated = types.Workspace.createFrom({
      ...workspace,
      auto_archive: { enabled: nextEnabled, days_closed: nextDays },
    });
    await UpdateWorkspace(updated);
    onSave();
  }

  async function runNow() {
    setBusy(true);
    try {
      const n = await RunAutoArchiveNow(workspace.id);
      alert(`Auto-archive complete: ${n} card(s) archived.`);
      onSave();
    } catch (err) {
      alert(`Auto-archive failed: ${err}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="text-xs text-slate-400 mb-2">Auto-archive</div>
      <div className="flex flex-col gap-2">
        <label className="flex items-center gap-2 text-xs text-slate-300">
          <input
            type="checkbox"
            checked={enabled}
            onChange={async (e) => {
              setEnabled(e.target.checked);
              await save(e.target.checked, days);
            }}
          />
          Archive DONE cards automatically
        </label>
        {enabled && (
          <label className="flex items-center gap-2 text-xs text-slate-400">
            After
            <input
              {...noAutoCorrect}
              type="number"
              min={1}
              max={365}
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              onBlur={async () => await save(enabled, days)}
              className="w-14 bg-slate-800 border border-slate-600 rounded px-1 py-0.5 text-slate-200"
            />
            days closed
          </label>
        )}
        <button
          onClick={runNow}
          disabled={busy || !enabled}
          className="text-xs text-slate-400 hover:text-amber-300 disabled:opacity-40 self-start"
        >
          {busy ? "Running…" : "Run auto-archive now"}
        </button>
      </div>
    </div>
  );
}

// Default retry policy values (mirrors types.DefaultRetryPolicy in Go).
const DEFAULT_RETRY_MAX_ATTEMPTS = 3;
const DEFAULT_RETRY_BASE_BACKOFF_S = 10;
const DEFAULT_RETRY_MAX_BACKOFF_S = 300;
const DEFAULT_RETRY_JITTER_PCT = 25;

function RetryPolicyEditor({ workspace, onSave }: { workspace: types.Workspace; onSave: () => void }) {
  const p = (workspace as any).retry_policy;
  const [maxAttempts, setMaxAttempts] = useState<number>(p?.max_attempts ?? DEFAULT_RETRY_MAX_ATTEMPTS);
  const [baseBackoffS, setBaseBackoffS] = useState<number>(
    p?.base_backoff ? Math.round(p.base_backoff / 1_000_000_000) : DEFAULT_RETRY_BASE_BACKOFF_S
  );
  const [maxBackoffS, setMaxBackoffS] = useState<number>(
    p?.max_backoff ? Math.round(p.max_backoff / 1_000_000_000) : DEFAULT_RETRY_MAX_BACKOFF_S
  );
  const [jitterPct, setJitterPct] = useState<number>(p?.jitter_pct ?? DEFAULT_RETRY_JITTER_PCT);
  const [enabled, setEnabled] = useState<boolean>(maxAttempts > 0);

  async function save(nextEnabled: boolean, ma: number, bb: number, mb: number, jp: number) {
    const policy = nextEnabled
      ? { max_attempts: ma, base_backoff: bb * 1_000_000_000, max_backoff: mb * 1_000_000_000, jitter_pct: jp }
      : { max_attempts: 0, base_backoff: 0, max_backoff: 0, jitter_pct: 0 };
    const updated = types.Workspace.createFrom({ ...workspace, retry_policy: policy });
    await UpdateWorkspace(updated);
    onSave();
  }

  return (
    <div>
      <div className="text-xs text-slate-400 mb-2">Auto-retry (transient failures)</div>
      <div className="flex flex-col gap-2">
        <label className="flex items-center gap-2 text-xs text-slate-300">
          <input
            type="checkbox"
            checked={enabled}
            onChange={async (e) => {
              setEnabled(e.target.checked);
              await save(e.target.checked, maxAttempts, baseBackoffS, maxBackoffS, jitterPct);
            }}
          />
          Automatically retry rate-limited / crashed sessions
        </label>
        {enabled && (
          <div className="flex flex-col gap-1.5 pl-5 text-xs text-slate-400">
            <label className="flex items-center gap-2">
              Max attempts
              <input
                {...noAutoCorrect}
                type="number" min={1} max={10} value={maxAttempts}
                onChange={(e) => setMaxAttempts(Number(e.target.value))}
                onBlur={() => save(enabled, maxAttempts, baseBackoffS, maxBackoffS, jitterPct)}
                className="w-14 bg-slate-800 border border-slate-600 rounded px-1 py-0.5 text-slate-200"
              />
              <span className="text-slate-600">(global cap: 10)</span>
            </label>
            <label className="flex items-center gap-2">
              Base backoff
              <input
                {...noAutoCorrect}
                type="number" min={1} max={300} value={baseBackoffS}
                onChange={(e) => setBaseBackoffS(Number(e.target.value))}
                onBlur={() => save(enabled, maxAttempts, baseBackoffS, maxBackoffS, jitterPct)}
                className="w-14 bg-slate-800 border border-slate-600 rounded px-1 py-0.5 text-slate-200"
              />
              s
            </label>
            <label className="flex items-center gap-2">
              Max backoff
              <input
                {...noAutoCorrect}
                type="number" min={10} max={3600} value={maxBackoffS}
                onChange={(e) => setMaxBackoffS(Number(e.target.value))}
                onBlur={() => save(enabled, maxAttempts, baseBackoffS, maxBackoffS, jitterPct)}
                className="w-14 bg-slate-800 border border-slate-600 rounded px-1 py-0.5 text-slate-200"
              />
              s
            </label>
            <label className="flex items-center gap-2">
              Jitter
              <input
                {...noAutoCorrect}
                type="number" min={0} max={50} value={jitterPct}
                onChange={(e) => setJitterPct(Number(e.target.value))}
                onBlur={() => save(enabled, maxAttempts, baseBackoffS, maxBackoffS, jitterPct)}
                className="w-14 bg-slate-800 border border-slate-600 rounded px-1 py-0.5 text-slate-200"
              />
              %
            </label>
          </div>
        )}
      </div>
    </div>
  );
}

function PoolBindings({ workspaceID }: { workspaceID: string }) {
  const [pools, setPools] = useState<workerpool.PoolStatus[]>([]);

  useEffect(() => {
    ListPools()
      .then((ps) => setPools((ps ?? []).filter((r) => r.pool.scope === "workspace" && r.pool.workspace_id === workspaceID)))
      .catch(() => {});
  }, [workspaceID]);

  if (pools.length === 0) {
    return <div className="text-xs text-slate-500">No workspace-specific pools. Assign a pool from the Pools tab.</div>;
  }
  return (
    <ul className="space-y-1">
      {pools.map((r) => (
        <li key={r.pool.id} className="flex items-center gap-2 text-xs">
          <span className="text-slate-200 font-mono">{r.pool.name}</span>
          <span className="text-slate-500">({r.pool.role})</span>
          <span className="font-mono text-slate-400">{r.active}/{r.pool.capacity}</span>
        </li>
      ))}
    </ul>
  );
}

function RemoteAuthPanel({ workspace, onSave }: { workspace: types.Workspace; onSave: () => void }) {
  const rc = workspace.remote_config;
  const [cfToken, setCFToken] = useState("");
  const [githubPAT, setGitHubPAT] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);

  async function redeploy() {
    if (!cfToken || !githubPAT) {
      setError("Both tokens are required to re-deploy.");
      return;
    }
    setBusy(true);
    setError(null);
    setOk(null);
    try {
      await DeployRemoteWorker(workspace.id, cfToken.trim(), githubPAT.trim());
      setOk("Re-deployed successfully.");
      await onSave();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function rotateKey() {
    if (!cfToken) {
      setError("Cloudflare API Token is required to rotate the key.");
      return;
    }
    setBusy(true);
    setError(null);
    setOk(null);
    setConfirmRotate(false);
    try {
      await RotateRemoteWorkerKey(workspace.id, cfToken.trim());
      setOk("Worker API key rotated. Old key is now invalid.");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="text-xs text-slate-400 mb-2">Remote Auth</div>
      {rc && (
        <div className="text-xs text-slate-500 mb-2 space-y-0.5">
          <div>Worker: <span className="text-slate-300 font-mono">{rc.cf_worker_endpoint_url}</span></div>
          <div>CF Secret refs: <span className="text-slate-300">{rc.cf_secret_refs?.github_pat_ref || "GITHUB_PAT"}</span></div>
          {rc.token_expired && (
            <div className="text-amber-400">⚠ Token expired — re-deploy to restore access.</div>
          )}
        </div>
      )}
      <div className="space-y-2">
        <label className="block">
          <div className="text-xs text-slate-500 mb-0.5">Cloudflare API Token</div>
          <input
            {...noAutoCorrect}
            type="password"
            value={cfToken}
            onChange={(e) => setCFToken(e.target.value)}
            placeholder="New CF API token…"
            className="w-full bg-slate-800 border border-slate-600 rounded px-2 py-1 text-xs text-slate-200 font-mono"
          />
        </label>
        <label className="block">
          <div className="text-xs text-slate-500 mb-0.5">GitHub PAT</div>
          <input
            {...noAutoCorrect}
            type="password"
            value={githubPAT}
            onChange={(e) => setGitHubPAT(e.target.value)}
            placeholder="New GitHub PAT…"
            className="w-full bg-slate-800 border border-slate-600 rounded px-2 py-1 text-xs text-slate-200 font-mono"
          />
        </label>
        {error && <div className="text-red-400 text-xs">{error}</div>}
        {ok && <div className="text-emerald-400 text-xs">{ok}</div>}
        <div className="flex gap-2 flex-wrap">
          <button
            onClick={redeploy}
            disabled={!cfToken || !githubPAT || busy}
            className="text-xs px-2 py-1 bg-sky-800 hover:bg-sky-700 rounded disabled:opacity-40"
          >
            {busy ? "Working…" : "Re-deploy worker"}
          </button>
          {rc && !confirmRotate && (
            <button
              onClick={() => setConfirmRotate(true)}
              disabled={!cfToken || busy}
              className="text-xs px-2 py-1 bg-amber-900 hover:bg-amber-800 rounded disabled:opacity-40"
            >
              Rotate worker API key
            </button>
          )}
          {rc && confirmRotate && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-amber-400">Rotation invalidates other machines. Confirm?</span>
              <button
                onClick={rotateKey}
                disabled={busy}
                className="text-xs px-2 py-1 bg-red-800 hover:bg-red-700 rounded disabled:opacity-40"
              >
                Rotate now
              </button>
              <button
                onClick={() => setConfirmRotate(false)}
                className="text-xs text-slate-400 hover:text-slate-200"
              >
                Cancel
          </button>
            </div>
          )}
        </div>
        {rc && (
          <div className="text-xs text-slate-600 mt-1">
            To use this workspace on another machine: rotate the key here, then re-deploy from the other machine. Or export the key from machine A and import it on machine B via Settings.
          </div>
        )}
      </div>
    </div>
  );
}

export function WorkspacesPanel() {
  const { workspaces, refresh, loading } = useWorkspaceStore();
  const [adding, setAdding] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [gcBusy, setGcBusy] = useState<string | null>(null);

  useEffect(() => {
    refresh();
  }, [refresh]);

  async function remove(id: string) {
    await RemoveWorkspace(id);
    await refresh();
    setConfirmRemove(null);
  }

  async function gc(id: string) {
    setGcBusy(id);
    try {
      const removed = await GCWorktrees(id);
      alert(`Removed ${removed} worktree(s).`);
    } catch (err) {
      alert(`GC worktrees failed: ${err}`);
    } finally {
      setGcBusy(null);
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="text-sm text-slate-300">Workspaces ({workspaces.length})</div>
        {!adding && (
          <button
            onClick={() => setAdding(true)}
            className="text-xs bg-emerald-700 hover:bg-emerald-600 px-2 py-1 rounded"
          >
            + Add workspace
          </button>
        )}
      </div>

      {adding && (
        <div className="rounded border border-slate-700 bg-slate-950 p-3">
          <AddWorkspaceModal onDone={() => setAdding(false)} />
        </div>
      )}

      {!adding && (
        <ul className="divide-y divide-slate-800 rounded border border-slate-800">
          {loading && workspaces.length === 0 && (
            <li className="px-3 py-3 text-xs text-slate-500">loading…</li>
          )}
          {!loading && workspaces.length === 0 && (
            <li className="px-3 py-3 text-xs text-slate-500">No workspaces yet. Click + Add workspace.</li>
          )}
          {workspaces.map((ws) => {
            const isExpanded = expanded === ws.id;
            return (
              <li key={ws.id} className="px-3 py-2">
                <div className="flex items-center gap-3">
                  <span
                    className="inline-block w-3 h-3 rounded-full"
                    style={{ backgroundColor: ws.color || "#64748b" }}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="text-sm text-slate-200">{ws.display_name || ws.id}</span>
                      {ws.execution_target === "remote" && (
                        <span className="text-[10px] bg-sky-900 text-sky-300 px-1.5 py-0.5 rounded">Remote</span>
                      )}
                      {ws.remote_config?.token_expired && (
                        <span className="text-[10px] bg-amber-900 text-amber-300 px-1.5 py-0.5 rounded">TOKEN EXPIRED</span>
                      )}
                      {ws.remote_config?.remote_unreachable && (
                        <span className="text-[10px] bg-red-900 text-red-300 px-1.5 py-0.5 rounded" title={`Endpoint unreachable: ${ws.remote_config.cf_worker_endpoint_url}`}>UNREACHABLE</span>
                      )}
                    </div>
                    <div className="text-xs text-slate-500 truncate">
                      {ws.github_owner}/{ws.github_repo} · {ws.skill_profile?.mode ?? "bundled"}
                      {ws.execution_target !== "remote" && ` · ${ws.repo_path}`}
                    </div>
                  </div>
                  <button
                    onClick={() => setExpanded(isExpanded ? null : ws.id)}
                    className="text-xs text-slate-400 hover:text-slate-200"
                  >
                    {isExpanded ? "Collapse" : "Settings…"}
                  </button>
                  <button
                    onClick={() => gc(ws.id)}
                    disabled={gcBusy === ws.id}
                    className="text-xs text-slate-400 hover:text-amber-300 disabled:opacity-50"
                    title="Remove all per-execute git worktrees under .prismconductor/worktrees/"
                  >
                    {gcBusy === ws.id ? "GC…" : "GC worktrees"}
                  </button>
                  {confirmRemove === ws.id ? (
                    <div className="flex gap-1 text-xs">
                      <button onClick={() => remove(ws.id)} className="text-red-400 hover:text-red-300 px-2">
                        Confirm
                      </button>
                      <button
                        onClick={() => setConfirmRemove(null)}
                        className="text-slate-500 hover:text-slate-300 px-2"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setConfirmRemove(ws.id)}
                      className="text-xs text-slate-500 hover:text-red-400"
                    >
                      Remove
                    </button>
                  )}
                </div>
                {isExpanded && (
                  <div className="mt-2 space-y-4">
                    {ws.remote_config?.remote_unreachable && (
                      <div className="rounded border border-red-800 bg-red-950 p-3 text-xs space-y-2">
                        <div className="text-red-300 font-medium">Remote worker endpoint unreachable</div>
                        <div className="text-red-400 font-mono break-all">{ws.remote_config.cf_worker_endpoint_url}</div>
                        <div className="text-slate-400">Re-run setup to redeploy with a corrected URL, or edit the endpoint directly in the Cloudflare dashboard.</div>
                        <div className="flex gap-2">
                          <button
                            onClick={() => setExpanded(ws.id)}
                            className="px-2 py-1 bg-sky-800 hover:bg-sky-700 rounded text-xs text-sky-200"
                          >
                            Re-run Setup
                          </button>
                        </div>
                      </div>
                    )}
                    {ws.execution_target === "remote" && (
                      <RemoteAuthPanel workspace={ws} onSave={refresh} />
                    )}
                    <SkillProfileEditor workspace={ws} />
                    <div>
                      <div className="text-xs text-slate-400 mb-2">Pipeline</div>
                      <PipelineEditor workspace={ws} />
                    </div>
                    <ComplexityScaleEditor workspace={ws} onSave={refresh} />
                    <AutoArchiveEditor workspace={ws} onSave={refresh} />
                    <RetryPolicyEditor workspace={ws} onSave={refresh} />
                    <SigningStrategyEditor workspace={ws} onSave={refresh} />
                    <div>
                      <div className="text-xs text-slate-400 mb-2">Labels</div>
                      <LabelsPanel workspaceID={ws.id} />
                    </div>
                    <div>
                      <div className="text-xs text-slate-400 mb-2">Pool bindings</div>
                      <PoolBindings workspaceID={ws.id} />
                    </div>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
