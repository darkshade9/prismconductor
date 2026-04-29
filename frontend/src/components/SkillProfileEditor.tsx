import { useEffect, useState } from "react";
import { ListPools, UpdateWorkspace } from "../../wailsjs/go/main/App";
import { types, workerpool } from "../../wailsjs/go/models";
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
  const [pools, setPools] = useState<workerpool.PoolStatus[]>([]);

  useEffect(() => {
    ListPools()
      .then((rows) => setPools(rows ?? []))
      .catch((err) => console.error("SkillProfileEditor ListPools", err));
  }, []);

  async function update(patch: Partial<types.SkillProfile>) {
    const next = new types.Workspace({
      ...workspace,
      skill_profile: { ...workspace.skill_profile, ...patch },
    });
    await UpdateWorkspace(next);
    await refresh();
  }

  const sp = workspace.skill_profile;
  const planPools = pools.filter((row) => row.pool.role === "plan" && row.pool.enabled);
  const workPools = pools.filter((row) => row.pool.role === "work" && row.pool.enabled);

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

      <div className="grid grid-cols-2 gap-3">
        <PoolPicker
          label="Plan pool"
          value={sp.preferred_plan_pool_id ?? ""}
          pools={planPools}
          onChange={(v) => update({ preferred_plan_pool_id: v })}
        />
        <PoolPicker
          label="Work pool"
          value={sp.preferred_work_pool_id ?? ""}
          pools={workPools}
          onChange={(v) => update({ preferred_work_pool_id: v })}
        />
      </div>

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

      <div className="flex items-start justify-between border-t border-slate-800 pt-3">
        <div className="flex-1 mr-3">
          <div className="text-sm text-slate-200">Auto-apply planner label suggestions</div>
          <div className="text-[11px] text-slate-500 mt-0.5">
            When a plan is ready, apply each <code>suggested_labels</code> entry that exists in
            the repo's label cache. Missing labels are surfaced in the plan modal with a (create) chip.
          </div>
        </div>
        <Toggle
          label=""
          checked={sp.auto_apply_labels !== false}
          onChange={(v) => update({ auto_apply_labels: v })}
        />
      </div>
    </div>
  );
}

function PoolPicker({
  label,
  value,
  pools,
  onChange,
}: {
  label: string;
  value: string;
  pools: workerpool.PoolStatus[];
  onChange: (v: string) => void;
}) {
  return (
    <label className="block">
      <div className="text-xs text-slate-500 mb-1">{label}</div>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-200 text-xs"
      >
        <option value="">(any compatible)</option>
        {pools.map((row) => (
          <option key={row.pool.id} value={row.pool.id}>
            {row.pool.name} · {row.pool.provider}
          </option>
        ))}
      </select>
    </label>
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
