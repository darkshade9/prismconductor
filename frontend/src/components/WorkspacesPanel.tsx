import { useEffect, useState } from "react";
import { GCWorktrees, RemoveWorkspace } from "../../wailsjs/go/main/App";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { AddWorkspaceForm } from "./AddWorkspaceForm";
import { SkillProfileEditor } from "./SkillProfileEditor";
import { LabelsPanel } from "./LabelsPanel";

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
          <AddWorkspaceForm onDone={() => setAdding(false)} />
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
                    <div className="text-sm text-slate-200">{ws.display_name || ws.id}</div>
                    <div className="text-xs text-slate-500 truncate">
                      {ws.github_owner}/{ws.github_repo} · {ws.skill_profile?.mode ?? "bundled"} · {ws.repo_path}
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
                    <SkillProfileEditor workspace={ws} />
                    <div>
                      <div className="text-xs text-slate-400 mb-2">Labels</div>
                      <LabelsPanel workspaceID={ws.id} />
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
