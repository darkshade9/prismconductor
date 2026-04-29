import { useEffect, useMemo, useState } from "react";
import { FilterIssuesByActiveGoal, GetAutoPullPaused } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { types } from "../../wailsjs/go/models";
import { useGoalStore } from "../stores/goalStore";
import { useIssueStore } from "../stores/issueStore";
import { GoalEditor } from "./GoalEditor";

export function GoalPane() {
  const { goals, refresh, activate, setStatus, remove } = useGoalStore();
  const issues = useIssueStore((s) => s.issues);
  const [editing, setEditing] = useState<types.Goal | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [scopedIssues, setScopedIssues] = useState<types.Issue[] | null>(null);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    GetAutoPullPaused().then(setPaused).catch(() => {});
    const off = EventsOn(
      "bus.auto_pull_paused_changed",
      (d: { paused: boolean }) => setPaused(!!d?.paused),
    );
    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  const active = goals.find((g) => g.status === "active") ?? null;
  const upNext = goals.filter((g) => g.status === "backlog");
  const past = goals.filter((g) => g.status === "achieved" || g.status === "abandoned");

  // Recompute the scoped issue list whenever the issue table or active goal changes.
  useEffect(() => {
    if (!active) {
      setScopedIssues(null);
      return;
    }
    let cancelled = false;
    FilterIssuesByActiveGoal(issues)
      .then((scoped) => {
        if (!cancelled) setScopedIssues(scoped ?? []);
      })
      .catch(() => setScopedIssues(null));
    return () => {
      cancelled = true;
    };
  }, [issues, active?.id]);

  const achievement = useMemo(() => {
    if (!active || !scopedIssues || scopedIssues.length === 0) return null;
    const allDone = scopedIssues.every((i) => i.column === "done" || i.state === "closed");
    return allDone ? { count: scopedIssues.length } : null;
  }, [active, scopedIssues]);

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
              🎯 {active.title}{paused ? " (paused)" : ""}
            </button>
          ) : (
            <span className="text-slate-600">none — {goals.length === 0 ? "create one →" : "activate from Up Next →"}</span>
          )}
          {active && (
            <>
              <button
                onClick={() => setStatus(active.id, "achieved")}
                className="text-xs text-emerald-400 hover:underline ml-2"
              >
                mark achieved
              </button>
              <button
                onClick={() => setStatus(active.id, "backlog")}
                className="text-xs text-slate-400 hover:text-slate-200 hover:underline ml-2"
                title="Stop auto-pull and return goal to Up Next"
              >
                deactivate
              </button>
            </>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs">
          <button onClick={newGoal} className="text-slate-300 hover:text-slate-100">+ New goal</button>
          <button onClick={() => setHistoryOpen((v) => !v)} className="text-slate-500 hover:text-slate-300">
            {historyOpen ? "Hide history" : `History (${past.length})`}
          </button>
        </div>
      </div>

      {achievement && active && (
        <div className="mt-2 px-3 py-2 rounded border border-emerald-700 bg-emerald-950/40 flex items-center justify-between">
          <span className="text-emerald-300">
            ✓ All {achievement.count} issues for this goal are done. Mark achieved?
          </span>
          <div className="flex gap-2">
            <button
              onClick={() => setStatus(active.id, "achieved")}
              className="text-xs bg-emerald-700 hover:bg-emerald-600 px-2 py-1 rounded"
            >
              Mark achieved
            </button>
            {upNext.length > 0 && (
              <button
                onClick={async () => {
                  await setStatus(active.id, "achieved");
                  await activate(upNext[0].id);
                }}
                className="text-xs bg-slate-700 hover:bg-slate-600 px-2 py-1 rounded"
                title={`Achieved + activate "${upNext[0].title}"`}
              >
                Achieve & advance
              </button>
            )}
          </div>
        </div>
      )}

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
