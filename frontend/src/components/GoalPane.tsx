import { useEffect, useState } from "react";
import { types } from "../../wailsjs/go/models";
import { useGoalStore } from "../stores/goalStore";
import { GoalEditor } from "./GoalEditor";

export function GoalPane() {
  const { goals, refresh, activate, setStatus, remove } = useGoalStore();
  const [editing, setEditing] = useState<types.Goal | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const active = goals.find((g) => g.status === "active") ?? null;
  const upNext = goals.filter((g) => g.status === "backlog");
  const past = goals.filter((g) => g.status === "achieved" || g.status === "abandoned");

  function newGoal() {
    setEditing(null);
    setEditorOpen(true);
  }
  function edit(g: types.Goal) {
    setEditing(g);
    setEditorOpen(true);
  }

  return (
    <div className="px-4 py-2 border-b border-slate-800 text-sm">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-slate-500">Active Goal:</span>
          {active ? (
            <button
              onClick={() => edit(active)}
              className="text-amber-300 hover:underline truncate max-w-[420px]"
              title={active.intent || ""}
            >
              🎯 {active.title}
            </button>
          ) : (
            <span className="text-slate-600">none — {goals.length === 0 ? "create one →" : "activate from Up Next →"}</span>
          )}
          {active && (
            <button
              onClick={() => setStatus(active.id, "achieved")}
              className="text-xs text-emerald-400 hover:underline ml-2"
            >
              mark achieved
            </button>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs">
          <button onClick={newGoal} className="text-slate-300 hover:text-slate-100">+ New goal</button>
          <button onClick={() => setHistoryOpen((v) => !v)} className="text-slate-500 hover:text-slate-300">
            {historyOpen ? "Hide history" : `History (${past.length})`}
          </button>
        </div>
      </div>

      {(upNext.length > 0 || historyOpen) && (
        <div className="mt-1 text-xs text-slate-500 space-y-0.5">
          {upNext.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span>Up Next:</span>
              {upNext.map((g) => (
                <GoalChip key={g.id} goal={g} onActivate={() => activate(g.id)} onEdit={() => edit(g)} onDelete={() => remove(g.id)} />
              ))}
            </div>
          )}
          {historyOpen && past.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span>Past:</span>
              {past.map((g) => (
                <span key={g.id} className="text-slate-600">
                  {g.title}{" "}
                  {g.status === "achieved" && g.achieved_at
                    ? `(achieved ${new Date(g.achieved_at).toISOString().slice(0, 10)})`
                    : "(abandoned)"}
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      <GoalEditor open={editorOpen} onClose={() => setEditorOpen(false)} goal={editing} />
    </div>
  );
}

function GoalChip({
  goal,
  onActivate,
  onEdit,
  onDelete,
}: {
  goal: types.Goal;
  onActivate: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded border border-slate-700 bg-slate-900">
      <button onClick={onActivate} title="Activate" className="text-slate-300 hover:text-emerald-400">▶</button>
      <button onClick={onEdit} className="text-slate-200 hover:text-slate-100">{goal.title}</button>
      <button onClick={onDelete} title="Delete" className="text-slate-600 hover:text-red-400">×</button>
    </span>
  );
}
