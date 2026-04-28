import { ReactNode } from "react";

export function Column({ title, count, children }: { title: string; count: number; children?: ReactNode }) {
  return (
    <div className="flex-1 min-w-[200px] bg-slate-900/40 rounded-lg p-2 border border-slate-800">
      <div className="text-xs uppercase tracking-wide text-slate-400 mb-2 px-1">
        {title} <span className="text-slate-500">({count})</span>
      </div>
      <div className="space-y-1">{children}</div>
    </div>
  );
}
