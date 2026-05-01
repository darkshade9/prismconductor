import { types } from "../../wailsjs/go/models";

const STAGE_LABELS: Record<string, string> = {
  plan: "Plan",
  execute: "Execute",
  continue: "Continue",
  close: "Close",
};

export function SkillDropdown({
  stage,
  skills,
  selected,
  onChange,
}: {
  stage: string;
  skills: types.SkillRef[];
  selected: types.SkillRef | undefined;
  onChange: (ref: types.SkillRef) => void;
}) {
  const label = STAGE_LABELS[stage] ?? stage;
  const selectedPath = selected?.path ?? "";

  function handleChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const ref = skills.find((s) => s.path === e.target.value);
    if (ref) onChange(ref);
  }

  return (
    <label className="flex items-center gap-2">
      <span className="w-20 text-xs text-slate-400 shrink-0">{label}</span>
      <select
        value={selectedPath}
        onChange={handleChange}
        className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-200 text-xs"
      >
        {skills.map((s) => (
          <option key={s.path} value={s.path}>
            {s.display_name}
          </option>
        ))}
      </select>
      {selected?.source === "repo" && (
        <span className="text-[10px] text-amber-400 shrink-0" title={selected.path}>
          repo
        </span>
      )}
    </label>
  );
}
