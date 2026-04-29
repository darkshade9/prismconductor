import { UpdateWorkspace } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useWorkspaceStore } from "../stores/workspaceStore";

type SkillMode = "bundled" | "hybrid" | "native";
const MODES: SkillMode[] = ["bundled", "hybrid", "native"];

const MODE_DESC: Record<SkillMode, string> = {
  bundled: "Use the conductor's universal skills only. Works for any repo.",
  hybrid: "Mix the repo's own skills (e.g. /start-issue) with bundled fallbacks.",
  native: "Use only the repo's own skills (PrismEngine pattern).",
};

export function SkillProfileEditor({ workspace }: { workspace: types.Workspace }) {
  const refresh = useWorkspaceStore((s) => s.refresh);

  async function update(patch: Partial<types.SkillProfile>) {
    const next = new types.Workspace({
      ...workspace,
      skill_profile: { ...workspace.skill_profile, ...patch },
    });
    await UpdateWorkspace(next);
    await refresh();
  }

  const sp = workspace.skill_profile;

  return (
    <div className="space-y-3 text-sm bg-slate-950/40 rounded p-3 border border-slate-800">
      <div>
        <div className="text-xs text-slate-500 mb-1">Mode</div>
        <div className="grid grid-cols-3 gap-2">
          {MODES.map((m) => (
            <button
              key={m}
              onClick={() => update({ mode: m })}
              className={
                "rounded border px-2 py-1.5 text-left " +
                (sp.mode === m
                  ? "border-emerald-600 bg-emerald-950/30 text-slate-100"
                  : "border-slate-700 bg-slate-900 hover:bg-slate-800 text-slate-300")
              }
            >
              <div className="text-sm font-medium">{m}</div>
              <div className="text-[11px] text-slate-500 mt-0.5">{MODE_DESC[m]}</div>
            </button>
          ))}
        </div>
      </div>

      {sp.mode !== "bundled" && (
        <div className="space-y-2">
          <div className="text-xs text-slate-500">Native skill commands</div>
          <NativeRow
            label="Plan"
            value={sp.native_plan_command}
            onChange={(v) => update({ native_plan_command: v })}
          />
          <NativeRow
            label="Execute"
            value={sp.native_execute_command}
            onChange={(v) => update({ native_execute_command: v })}
          />
          <NativeRow
            label="Close"
            value={sp.native_close_command}
            onChange={(v) => update({ native_close_command: v })}
          />
        </div>
      )}

      {sp.mode === "hybrid" && (
        <div>
          <div className="text-xs text-slate-500 mb-1">Use bundled fallback for</div>
          <div className="flex flex-wrap gap-3">
            <Toggle
              label="plan"
              checked={sp.use_conductor_plan}
              onChange={(v) => update({ use_conductor_plan: v })}
            />
            <Toggle
              label="execute"
              checked={sp.use_conductor_execute}
              onChange={(v) => update({ use_conductor_execute: v })}
            />
            <Toggle
              label="close"
              checked={sp.use_conductor_close}
              onChange={(v) => update({ use_conductor_close: v })}
            />
          </div>
        </div>
      )}
    </div>
  );
}

function NativeRow({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="flex items-center gap-2">
      <span className="w-16 text-xs text-slate-500">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="/start-issue"
        className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 text-xs"
      />
    </label>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-1.5 text-slate-300 text-xs">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  );
}
