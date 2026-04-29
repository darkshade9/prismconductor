import { useEffect, useState } from "react";
import { ListBundledSkills, OpenBundledSkill } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";

export function BundledSkillsViewer() {
  const [skills, setSkills] = useState<main.BundledSkill[]>([]);
  const [active, setActive] = useState<string | null>(null);

  useEffect(() => {
    ListBundledSkills()
      .then((s) => {
        setSkills(s ?? []);
        if ((s ?? []).length > 0) setActive(s[0].name);
      })
      .catch(() => {});
  }, []);

  const current = skills.find((s) => s.name === active);

  return (
    <div className="space-y-3 text-sm">
      <div className="text-xs text-slate-500">
        Read-only. Edit on disk to override; conductor uses the on-disk copy.
      </div>
      <div className="flex gap-2 flex-wrap">
        {skills.map((s) => (
          <button
            key={s.name}
            onClick={() => setActive(s.name)}
            className={
              "px-2 py-0.5 rounded border text-xs " +
              (active === s.name
                ? "bg-slate-800 border-slate-600 text-slate-200"
                : "border-slate-700 text-slate-400 hover:text-slate-200")
            }
          >
            {s.name}
          </button>
        ))}
      </div>
      {current && (
        <div className="rounded border border-slate-800 bg-slate-950 overflow-hidden">
          <div className="flex items-center justify-between px-3 py-1.5 border-b border-slate-800 text-xs">
            <span className="text-slate-500 font-mono truncate">{current.path}</span>
            <button
              onClick={() => OpenBundledSkill(current.name)}
              className="text-sky-400 hover:underline"
            >
              Open in editor
            </button>
          </div>
          <pre className="p-3 text-xs text-slate-300 whitespace-pre-wrap max-h-[40vh] overflow-y-auto font-mono">
            {current.body}
          </pre>
        </div>
      )}
    </div>
  );
}
