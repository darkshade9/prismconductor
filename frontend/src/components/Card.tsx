import { cn } from "../lib/cn";

export type CardProps = {
  number: number;
  workspace: string;
  title: string;
  state?: "primitive" | "plan_ready" | "working" | "pr_open" | "blocked";
  blockedBy?: number;
  priority?: number;
  color?: string;
  onClick?: () => void;
};

const stateLabel: Record<NonNullable<CardProps["state"]>, string> = {
  primitive: "primitive",
  plan_ready: "Plan Ready",
  working: "working",
  pr_open: "PR open",
  blocked: "blocked",
};

export function Card(p: CardProps) {
  return (
    <button
      onClick={p.onClick}
      className={cn(
        "w-full text-left rounded-md border border-slate-700 bg-slate-800/70 hover:bg-slate-800 px-3 py-2 mb-2",
        "shadow-sm transition-colors",
      )}
    >
      <div className="flex items-center justify-between text-xs text-slate-400">
        <span className="flex items-center gap-2">
          <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: p.color ?? "#64748b" }} />
          #{p.number}
          <span className="text-slate-500">{p.workspace}</span>
        </span>
        {p.priority != null && <span className="text-slate-500">P{p.priority.toFixed(2)}</span>}
      </div>
      <div className="text-sm text-slate-100 mt-1 line-clamp-2">{p.title}</div>
      {p.state && (
        <div className="text-[11px] text-slate-400 mt-1">
          {stateLabel[p.state]}
          {p.state === "blocked" && p.blockedBy ? ` by #${p.blockedBy}` : ""}
        </div>
      )}
    </button>
  );
}
