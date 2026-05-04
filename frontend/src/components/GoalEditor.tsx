import { useEffect, useState } from "react";
import { types } from "../../wailsjs/go/models";
import { useGoalStore } from "../stores/goalStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

export function GoalEditor({
  open,
  onClose,
  goal,
}: {
  open: boolean;
  onClose: () => void;
  goal: types.Goal | null;
}) {
  const save = useGoalStore((s) => s.save);
  const workspaces = useWorkspaceStore((s) => s.workspaces);

  const [title, setTitle] = useState("");
  const [intent, setIntent] = useState("");
  const [acceptance, setAcceptance] = useState("");
  const [workspaceID, setWorkspaceID] = useState("");
  const [labels, setLabels] = useState("");
  const [milestone, setMilestone] = useState("");
  const [freeText, setFreeText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setTitle(goal?.title ?? "");
    setIntent(goal?.intent ?? "");
    setAcceptance(goal?.acceptance_rule ?? "");
    setWorkspaceID(goal?.workspace_id ?? "");
    setLabels((goal?.issue_filter?.labels ?? []).join(", "));
    setMilestone(goal?.issue_filter?.milestone ?? "");
    setFreeText(goal?.issue_filter?.free_text ?? "");
    setError(null);
  }, [open, goal]);

  if (!open) return null;

  async function submit() {
    if (!title.trim()) {
      setError("Title required");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const next = new types.Goal({
        ...(goal ?? { id: "", status: "backlog", order: 0, created_at: new Date().toISOString() }),
        title: title.trim(),
        intent,
        acceptance_rule: acceptance,
        workspace_id: workspaceID,
        issue_filter: {
          labels: labels.split(",").map((s) => s.trim()).filter(Boolean),
          milestone: milestone.trim(),
          free_text: freeText,
          includes: goal?.issue_filter?.includes ?? [],
          excludes: goal?.issue_filter?.excludes ?? [],
        },
      });
      await save(next);
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[560px] max-h-[80vh] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">{goal ? "Edit goal" : "New goal"}</div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="p-4 space-y-3 text-sm overflow-y-auto">
          <Field label="Title">
            <input {...noAutoCorrect} value={title} onChange={(e) => setTitle(e.target.value)} className={inputCls} />
          </Field>
          <Field label="Intent (markdown)">
            <textarea {...noAutoCorrect} value={intent} onChange={(e) => setIntent(e.target.value)} rows={3} className={inputCls} />
          </Field>
          <Field label="Acceptance rule">
            <textarea {...noAutoCorrect} value={acceptance} onChange={(e) => setAcceptance(e.target.value)} rows={2} className={inputCls} />
          </Field>
          <Field label="Workspace (optional — blank = cross-workspace)">
            <select value={workspaceID} onChange={(e) => setWorkspaceID(e.target.value)} className={inputCls}>
              <option value="">All workspaces</option>
              {workspaces.map((w) => (
                <option key={w.id} value={w.id}>{w.display_name || w.id}</option>
              ))}
            </select>
          </Field>
          <div className="grid grid-cols-2 gap-2">
            <Field label="Labels (comma-separated)">
              <input {...noAutoCorrect} value={labels} onChange={(e) => setLabels(e.target.value)} className={inputCls} />
            </Field>
            <Field label="Milestone">
              <input {...noAutoCorrect} value={milestone} onChange={(e) => setMilestone(e.target.value)} className={inputCls} />
            </Field>
          </div>
          <Field label="Free-text match (title/body)">
            <input {...noAutoCorrect} value={freeText} onChange={(e) => setFreeText(e.target.value)} className={inputCls} />
          </Field>
          {error && <div className="text-red-400 text-xs">{error}</div>}
        </div>
        <div className="px-4 py-2 border-t border-slate-800 flex justify-end gap-2">
          <button onClick={onClose} className="px-3 py-1 text-slate-400 hover:text-slate-200">Cancel</button>
          <button
            onClick={submit}
            disabled={busy}
            className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
          >
            {busy ? "Saving…" : goal ? "Save" : "Create"}
          </button>
        </div>
      </div>
    </div>
  );
}

const inputCls = "w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-xs text-slate-500 mb-1">{label}</div>
      {children}
    </label>
  );
}
