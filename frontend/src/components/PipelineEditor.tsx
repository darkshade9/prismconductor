import { useEffect, useState } from "react";
import { ListAvailableSkills, SaveWorkspacePipeline, ValidatePipeline } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

const DEFAULT_MAX_LOOPS = 3;

function makeStep(skills: types.SkillRef[]): types.PipelineStep {
  const first = skills[0];
  return new types.PipelineStep({
    id: crypto.randomUUID(),
    name: first?.display_name ?? "New Step",
    skill_ref: first ?? new types.SkillRef({ path: "", source: "bundled", name: "", display_name: "" }),
    auto_chain: false,
    max_loops: 0,
    on_success: "",
    on_fail: "",
  });
}

export function PipelineEditor({ workspace }: { workspace: types.Workspace }) {
  const refresh = useWorkspaceStore((s) => s.refresh);
  const [skills, setSkills] = useState<types.SkillRef[]>([]);
  const [steps, setSteps] = useState<types.PipelineStep[]>(
    workspace.pipeline?.steps ?? []
  );
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    ListAvailableSkills(workspace.id)
      .then((refs) => setSkills(refs ?? []))
      .catch((err) => console.error("PipelineEditor ListAvailableSkills", err));
  }, [workspace.id]);

  // Sync steps when workspace.pipeline changes externally (e.g. after refresh).
  useEffect(() => {
    if (!dirty) {
      setSteps(workspace.pipeline?.steps ?? []);
    }
  }, [workspace.pipeline, dirty]);

  function updateStep(idx: number, patch: Partial<types.PipelineStep>) {
    setSteps((prev) => {
      const next = [...prev];
      next[idx] = new types.PipelineStep({ ...next[idx], ...patch });
      return next;
    });
    setDirty(true);
    setError(null);
  }

  function addStep() {
    setSteps((prev) => [...prev, makeStep(skills)]);
    setDirty(true);
    setError(null);
  }

  function removeStep(idx: number) {
    const removedID = steps[idx].id;
    setSteps((prev) => {
      const next = prev.filter((_, i) => i !== idx).map((s) => {
        // Clear dangling references to the removed step.
        return new types.PipelineStep({
          ...s,
          on_success: s.on_success === removedID ? "" : s.on_success,
          on_fail: s.on_fail === removedID ? "" : s.on_fail,
        });
      });
      return next;
    });
    setDirty(true);
    setError(null);
  }

  function moveStep(idx: number, dir: -1 | 1) {
    const next = [...steps];
    const target = idx + dir;
    if (target < 0 || target >= next.length) return;
    [next[idx], next[target]] = [next[target], next[idx]];
    setSteps(next);
    setDirty(true);
  }

  async function save() {
    setSaving(true);
    setError(null);
    try {
      const pipeline = new types.WorkspacePipeline({ steps, version: 0 });
      await ValidatePipeline(pipeline);
      await SaveWorkspacePipeline(workspace.id, pipeline);
      await refresh();
      setDirty(false);
    } catch (err: any) {
      setError(String(err?.message ?? err));
    } finally {
      setSaving(false);
    }
  }

  async function clearPipeline() {
    setSaving(true);
    setError(null);
    try {
      await SaveWorkspacePipeline(workspace.id, new types.WorkspacePipeline({ steps: [], version: 0 }));
      setSteps([]);
      setDirty(false);
      await refresh();
    } catch (err: any) {
      setError(String(err?.message ?? err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-3 text-sm bg-slate-950/40 rounded p-3 border border-slate-800">
      <div className="flex items-center justify-between">
        <div className="text-xs text-slate-400">Custom pipeline steps</div>
        <div className="text-[10px] text-slate-600">
          runs after Execute, before Close
        </div>
      </div>

      {steps.length === 0 ? (
        <div className="text-xs text-slate-600 italic">
          No custom steps — default Execute → Close flow.
        </div>
      ) : (
        <div className="space-y-3">
          {steps.map((step, idx) => (
            <StepRow
              key={step.id}
              step={step}
              idx={idx}
              total={steps.length}
              skills={skills}
              allSteps={steps}
              onUpdate={(patch) => updateStep(idx, patch)}
              onRemove={() => removeStep(idx)}
              onMove={(dir) => moveStep(idx, dir)}
            />
          ))}
        </div>
      )}

      {error && (
        <div className="text-xs text-red-400 break-words border border-red-900 rounded px-2 py-1">
          ⚠ {error}
        </div>
      )}

      <div className="flex items-center gap-2 pt-1">
        <button
          onClick={addStep}
          className="px-2 py-1 text-xs rounded border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500"
        >
          + Add step
        </button>
        {dirty && (
          <button
            onClick={save}
            disabled={saving}
            className="px-2 py-1 text-xs rounded border border-sky-700 text-sky-300 hover:border-sky-500 disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save pipeline"}
          </button>
        )}
        {steps.length > 0 && !dirty && (
          <button
            onClick={clearPipeline}
            disabled={saving}
            className="ml-auto px-2 py-1 text-xs rounded border border-slate-800 text-slate-600 hover:text-red-400 hover:border-red-900"
          >
            Reset to default
          </button>
        )}
      </div>
    </div>
  );
}

function StepRow({
  step,
  idx,
  total,
  skills,
  allSteps,
  onUpdate,
  onRemove,
  onMove,
}: {
  step: types.PipelineStep;
  idx: number;
  total: number;
  skills: types.SkillRef[];
  allSteps: types.PipelineStep[];
  onUpdate: (patch: Partial<types.PipelineStep>) => void;
  onRemove: () => void;
  onMove: (dir: -1 | 1) => void;
}) {
  const otherSteps = allSteps.filter((s) => s.id !== step.id);

  return (
    <div className="border border-slate-700 rounded p-2 space-y-2 bg-slate-900/50">
      <div className="flex items-center gap-2">
        <div className="flex flex-col gap-0.5">
          <button
            onClick={() => onMove(-1)}
            disabled={idx === 0}
            className="text-slate-600 hover:text-slate-300 disabled:opacity-30 text-[10px] leading-none"
            title="Move up"
          >▲</button>
          <button
            onClick={() => onMove(1)}
            disabled={idx === total - 1}
            className="text-slate-600 hover:text-slate-300 disabled:opacity-30 text-[10px] leading-none"
            title="Move down"
          >▼</button>
        </div>

        <span className="text-[10px] text-slate-600 w-4 shrink-0">{idx + 1}.</span>

        <input
          {...noAutoCorrect}
          type="text"
          value={step.name}
          onChange={(e) => onUpdate({ name: e.target.value })}
          placeholder="Step name"
          className="flex-1 min-w-0 bg-slate-950 border border-slate-700 rounded px-2 py-0.5 text-xs text-slate-200"
        />

        <button
          onClick={onRemove}
          className="text-slate-600 hover:text-red-400 text-[10px] px-1 shrink-0"
          title="Remove step"
        >✕</button>
      </div>

      <div className="pl-7 space-y-1.5">
        {/* Skill selector */}
        <label className="flex items-center gap-2 text-xs">
          <span className="w-16 text-slate-500 shrink-0">Skill</span>
          <select
            value={step.skill_ref?.path ?? ""}
            onChange={(e) => {
              const ref = skills.find((s) => s.path === e.target.value);
              if (ref) onUpdate({ skill_ref: ref, name: step.name || ref.display_name });
            }}
            className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-0.5 text-slate-200 text-xs"
          >
            {skills.map((s) => (
              <option key={s.path} value={s.path}>
                {s.display_name}
              </option>
            ))}
          </select>
        </label>

        {/* on_success */}
        <label className="flex items-center gap-2 text-xs">
          <span className="w-16 text-slate-500 shrink-0">On pass</span>
          <select
            value={step.on_success ?? ""}
            onChange={(e) => onUpdate({ on_success: e.target.value })}
            className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-0.5 text-slate-200 text-xs"
          >
            <option value="">→ done (advance to Close)</option>
            {otherSteps.map((s) => (
              <option key={s.id} value={s.id}>
                → {s.name || s.id.slice(0, 8)}
              </option>
            ))}
          </select>
        </label>

        {/* on_fail */}
        <label className="flex items-center gap-2 text-xs">
          <span className="w-16 text-slate-500 shrink-0">On fail</span>
          <select
            value={step.on_fail ?? ""}
            onChange={(e) => onUpdate({ on_fail: e.target.value })}
            className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-0.5 text-slate-200 text-xs"
          >
            <option value="">→ stop (BLOCKED)</option>
            {otherSteps.map((s) => (
              <option key={s.id} value={s.id}>
                → {s.name || s.id.slice(0, 8)}
              </option>
            ))}
          </select>
        </label>

        <div className="flex items-center gap-4">
          {/* auto_chain */}
          <label className="flex items-center gap-1.5 text-xs text-slate-400 cursor-pointer">
            <input
              type="checkbox"
              checked={step.auto_chain ?? false}
              onChange={(e) => onUpdate({ auto_chain: e.target.checked })}
              className="accent-sky-500"
            />
            Auto-advance (no confirm)
          </label>

          {/* max_loops */}
          <label className="flex items-center gap-1.5 text-xs text-slate-400">
            <span>Max loops</span>
            <input
              {...noAutoCorrect}
              type="number"
              min={0}
              max={20}
              value={step.max_loops ?? 0}
              onChange={(e) => onUpdate({ max_loops: parseInt(e.target.value, 10) || 0 })}
              className="w-12 bg-slate-950 border border-slate-700 rounded px-1 py-0.5 text-xs text-slate-200 text-center"
            />
            <span className="text-slate-600">(0 = no loop)</span>
          </label>
        </div>
      </div>
    </div>
  );
}
