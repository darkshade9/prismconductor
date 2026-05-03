import { useEffect, useState } from "react";
import { GoalSpendToday } from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";

interface Props {
  pools: types.PoolUsage[];
  maxVisible?: number;
  goalID?: string;
}

function barColor(pct: number): string {
  if (pct >= 90) return "bg-red-500";
  if (pct >= 70) return "bg-amber-400";
  return "bg-emerald-500";
}

function textColor(pct: number): string {
  if (pct >= 90) return "text-red-400";
  if (pct >= 70) return "text-amber-400";
  return "text-emerald-400";
}

function formatResetTime(resetsAt: string): string {
  const t = new Date(resetsAt);
  const diff = t.getTime() - Date.now();
  if (diff <= 0) return "now";
  const mins = Math.round(diff / 60_000);
  if (mins < 60) return `${mins}m`;
  const hrs = Math.round(diff / 3_600_000);
  return `${hrs}h`;
}

function UsageBar({ u }: { u: types.PoolUsage }) {
  const [hovered, setHovered] = useState(false);
  const pct = u.limit_value > 0 ? Math.min(100, Math.round((u.used / u.limit_value) * 100)) : 0;
  const remaining = u.limit_value - u.used;
  const resetLabel = formatResetTime(u.resets_at as unknown as string);
  const windowLabel = u.window === "tokens" ? "tok" : "req";

  return (
    <div
      className="relative flex flex-col gap-0.5 cursor-default"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div className="flex items-center gap-1">
        <span className="text-slate-400 text-xs whitespace-nowrap">
          {u.pool_name} {windowLabel}
        </span>
        <span className={`text-xs font-mono ${textColor(pct)}`}>{pct}%</span>
      </div>
      <div className="w-20 h-1.5 rounded-full bg-slate-700 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${barColor(pct)}`}
          style={{ width: `${pct}%` }}
        />
      </div>

      {/* Hover tooltip */}
      {hovered && (
        <div className="absolute bottom-full mb-1.5 left-0 z-50 w-max max-w-[220px] rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-xs shadow-lg">
          <div className="text-slate-200 font-medium mb-0.5">
            {u.pool_name} — {u.window}
          </div>
          <div className="text-slate-400">
            {remaining.toLocaleString()} of {u.limit_value.toLocaleString()} remaining
          </div>
          <div className="text-slate-500">resets in {resetLabel}</div>
          {pct >= 90 && (
            <div className="text-red-400 mt-0.5">⚠ Near limit</div>
          )}
          {pct >= 70 && pct < 90 && (
            <div className="text-amber-400 mt-0.5">⚠ Approaching limit</div>
          )}
        </div>
      )}
    </div>
  );
}

export function GoalRowStats({ pools, maxVisible = 3, goalID }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [spend, setSpend] = useState<main.GoalSpendResult | null>(null);
  const [spendDismissed, setSpendDismissed] = useState(false);

  useEffect(() => {
    if (!goalID) return;
    setSpendDismissed(false);
    GoalSpendToday(goalID)
      .then((r) => setSpend(r ?? null))
      .catch(() => {});
  }, [goalID]);

  const visible = expanded ? pools : pools.slice(0, maxVisible);
  const overflow = pools.length - maxVisible;

  return (
    <div className="flex items-center gap-3">
      {visible.map((u) => (
        <UsageBar key={`${u.pool_id}:${u.window}`} u={u} />
      ))}
      {!expanded && overflow > 0 && (
        <button
          onClick={() => setExpanded(true)}
          className="text-xs text-slate-500 hover:text-slate-300 whitespace-nowrap"
        >
          +{overflow} more
        </button>
      )}
      {expanded && overflow > 0 && (
        <button
          onClick={() => setExpanded(false)}
          className="text-xs text-slate-500 hover:text-slate-300 whitespace-nowrap"
        >
          show less
        </button>
      )}
      {!spendDismissed && spend && spend.run_count > 0 && (
        <span className="flex items-center gap-1 text-[10px] font-mono text-slate-500 whitespace-nowrap">
          Today: ${spend.total_usd.toFixed(2)} across {spend.run_count} run{spend.run_count !== 1 ? "s" : ""}
          <button
            onClick={() => setSpendDismissed(true)}
            className="text-slate-600 hover:text-slate-400 ml-0.5 leading-none"
            title="Dismiss"
          >
            ×
          </button>
        </span>
      )}
    </div>
  );
}
