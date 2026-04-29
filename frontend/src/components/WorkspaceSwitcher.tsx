import { useEffect, useState } from "react";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { LabelsModal } from "./LabelsModal";

export function WorkspaceSwitcher() {
  const { workspaces, selectedID, setSelected, refresh, loading } = useWorkspaceStore();
  const [labelsOpen, setLabelsOpen] = useState(false);
  const labelsTarget = selectedID ?? workspaces[0]?.id ?? "";

  useEffect(() => {
    if (workspaces.length === 0 && !loading) refresh();
  }, [workspaces.length, loading, refresh]);

  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800 text-sm">
      <span className="text-slate-500">Workspace:</span>
      <button
        onClick={() => setSelected(null)}
        className={
          "px-2 py-0.5 rounded border " +
          (selectedID === null
            ? "bg-slate-800 border-slate-600 text-slate-200"
            : "border-slate-700 text-slate-400 hover:text-slate-200")
        }
      >
        All
      </button>
      {workspaces.map((w) => (
        <button
          key={w.id}
          onClick={() => setSelected(w.id)}
          className={
            "px-2 py-0.5 rounded border inline-flex items-center gap-1.5 " +
            (selectedID === w.id
              ? "bg-slate-800 border-slate-600 text-slate-200"
              : "border-slate-700 text-slate-400 hover:text-slate-200")
          }
        >
          <span className="inline-block w-2 h-2 rounded-full" style={{ backgroundColor: w.color || "#64748b" }} />
          {w.display_name || w.id}
        </button>
      ))}
      {workspaces.length === 0 && !loading && (
        <span className="text-xs text-slate-600">— no workspaces yet, add one in Settings</span>
      )}
      {labelsTarget && (
        <button
          onClick={() => setLabelsOpen(true)}
          className="ml-auto px-2 py-0.5 rounded border border-slate-700 text-xs text-slate-400 hover:text-slate-200 hover:border-slate-500"
          title="Manage labels for the active workspace"
        >
          🏷  Manage labels
        </button>
      )}
      <LabelsModal
        open={labelsOpen}
        onClose={() => setLabelsOpen(false)}
        workspaceID={labelsTarget}
      />
    </div>
  );
}
