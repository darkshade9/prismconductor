import { useState } from "react";
import { AddWorkspace, InspectRepo, PickRepoPath } from "../../wailsjs/go/main/App";
import { types, workspace } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

const COLOR_PALETTE = ["#22c55e", "#06b6d4", "#a855f7", "#f59e0b", "#ef4444", "#ec4899", "#14b8a6", "#eab308"];

export function AddWorkspaceForm({ onDone }: { onDone: () => void }) {
  const refresh = useWorkspaceStore((s) => s.refresh);

  const [path, setPath] = useState("");
  const [insp, setInsp] = useState<workspace.Inspection | null>(null);
  const [id, setID] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [color, setColor] = useState(COLOR_PALETTE[0]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function pickPath() {
    setError(null);
    try {
      const p = await PickRepoPath();
      if (p) {
        setPath(p);
        await inspect(p);
      }
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  }

  async function inspect(p: string) {
    setBusy(true);
    setError(null);
    try {
      const i = await InspectRepo(p);
      setInsp(i);
      if (!id) setID(i.suggested_id || "");
      if (!displayName) setDisplayName(i.suggested_id || "");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  const allChecksPass = insp?.checks?.every((c) => c.pass) ?? false;

  async function save() {
    if (!insp || !id) return;
    setBusy(true);
    setError(null);
    try {
      const ws = new types.Workspace({
        id,
        display_name: displayName || id,
        repo_path: insp.repo_path,
        github_owner: insp.github_owner,
        github_repo: insp.github_repo,
        default_branch: insp.default_branch || "main",
        color,
        agent_env: { env_vars: {}, pre_commands: [], shell: "/bin/bash" },
        skill_profile: insp.skill_profile,
        conventions: insp.conventions,
        enabled: true,
      });
      await AddWorkspace(ws);
      await refresh();
      onDone();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3 text-sm">
      <div className="flex items-center gap-2">
        <input
          {...noAutoCorrect}
          type="text"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          onBlur={() => path && inspect(path)}
          placeholder="/absolute/path/to/repo"
          className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
        />
        <button onClick={pickPath} className="px-2 py-1 border border-slate-700 rounded hover:bg-slate-800">
          Browse…
        </button>
      </div>

      {insp && (
        <>
          <div className="rounded border border-slate-800 bg-slate-900 p-2">
            <div className="text-xs text-slate-500 mb-1">Onboarding checks (§3)</div>
            <ul className="space-y-1">
              {insp.checks.map((c) => (
                <li key={c.name} className="flex items-center gap-2">
                  <span className={c.pass ? "text-emerald-400" : "text-red-400"}>{c.pass ? "✓" : "✗"}</span>
                  <span className="text-slate-300">{c.name}</span>
                  {c.info && <span className="text-slate-500 text-xs truncate max-w-[280px]" title={c.info}>{c.info.split("\n")[0]}</span>}
                </li>
              ))}
            </ul>
          </div>

          {insp.github_owner && (
            <div className="text-xs text-slate-400">
              Detected: <span className="text-slate-200">{insp.github_owner}/{insp.github_repo}</span>{" "}
              · branch <span className="text-slate-200">{insp.default_branch || "?"}</span>{" "}
              · skill mode <span className="text-amber-300">{insp.skill_profile.mode}</span>
            </div>
          )}

          <div className="grid grid-cols-2 gap-2">
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">ID</div>
              <input
                {...noAutoCorrect}
                value={id}
                onChange={(e) => setID(e.target.value)}
                className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
              />
            </label>
            <label className="block">
              <div className="text-xs text-slate-500 mb-1">Display name</div>
              <input
                {...noAutoCorrect}
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
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
        </>
      )}

      {error && <div className="text-red-400 text-xs">{error}</div>}

      <div className="flex justify-end gap-2 pt-2 border-t border-slate-800">
        <button onClick={onDone} className="px-3 py-1 text-slate-400 hover:text-slate-200">Cancel</button>
        <button
          onClick={save}
          disabled={!insp || !allChecksPass || !id || busy}
          className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-40 disabled:cursor-not-allowed"
          title={!allChecksPass ? "All onboarding checks must pass" : ""}
        >
          {busy ? "Saving…" : "Add workspace"}
        </button>
      </div>
    </div>
  );
}
