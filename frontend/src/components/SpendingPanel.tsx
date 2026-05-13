import { useEffect, useState } from "react";
import { GetGlobalCostProjection, GetWorkspaceCostProjection } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";
import { Sparkline } from "./SpendingChart";

const COLOR_THRESHOLDS = { amber: 50, red: 200 };

function projColor(usd: number, free: boolean): string {
  if (free) return "text-emerald-400";
  if (usd < COLOR_THRESHOLDS.amber) return "text-emerald-400";
  if (usd < COLOR_THRESHOLDS.red) return "text-amber-400";
  return "text-rose-400";
}

function usd(v: number): string {
  return `$${v.toFixed(2)}`;
}

function projUsd(v: number): string {
  return v < 0.01 ? "$0" : `~$${v.toFixed(0)}`;
}

function GlobalSummary({ global: g }: { global: main.PoolCostProjection }) {
  const sparkPoints = [
    { label: "7d", value: g.last_7d_usd },
    { label: "30d", value: g.last_30d_usd },
  ];
  return (
    <div className="border border-slate-700 rounded p-3 bg-slate-900/60 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-slate-300 text-sm font-medium">Total spend</span>
        <span className={`text-sm font-mono font-semibold tabular-nums ${projColor(g.projected_month_usd, false)}`}>
          {projUsd(g.projected_month_usd)}/mo projected
        </span>
      </div>
      <div className="flex items-center gap-6 text-xs text-slate-400 font-mono tabular-nums">
        <span title="Last 7 days">7d: {usd(g.last_7d_usd)}</span>
        <span title="Last 30 days">30d: {usd(g.last_30d_usd)}</span>
        <span title="Terminal sessions in the last 30 days">{g.sessions_30d} sessions</span>
        {g.avg_per_session_usd > 0 && (
          <span title="Average spend per session">{usd(g.avg_per_session_usd)}/run avg</span>
        )}
      </div>
      {g.days_with_data > 0 && (
        <Sparkline
          points={[
            ...Array.from({ length: Math.max(0, g.days_with_data - 1) }, (_, i) => ({
              label: `d${i}`,
              value: g.last_30d_usd / Math.max(g.days_with_data, 1),
            })),
            { label: "recent", value: g.last_7d_usd / 7 },
          ]}
          width={240}
          height={36}
          color="#38bdf8"
        />
      )}
      {g.days_with_data === 0 && (
        <p className="text-xs text-slate-500">No completed sessions in the last 30 days.</p>
      )}
      <p className="text-[10px] text-slate-600">
        Straight-line projection from 30-day rolling total.{" "}
        <a
          href="#"
          className="underline hover:text-slate-400"
          onClick={(e) => {
            e.preventDefault();
            // Opening docs would require wails BrowserOpenURL — show inline note instead.
          }}
          title="See docs/cost-expectations.md for assumptions"
        >
          Learn about cost assumptions
        </a>
      </p>
    </div>
  );
}

function PoolProjectionRow({ p }: { p: main.PoolCostProjection }) {
  return (
    <div className="flex items-center gap-3 py-1.5 border-b border-slate-800 last:border-0 text-xs">
      <div className="flex-1 min-w-0">
        <span className="text-slate-200 font-mono">{p.pool_name}</span>
        <span className="ml-1.5 text-slate-500">({p.provider})</span>
      </div>
      <span className="text-slate-400 tabular-nums font-mono">
        {p.sessions_30d} sessions
      </span>
      <span className="text-slate-400 tabular-nums font-mono">
        30d: {usd(p.last_30d_usd)}
      </span>
      <span className={`tabular-nums font-mono font-medium ${projColor(p.projected_month_usd, p.free_tier)}`}>
        {p.free_tier
          ? "$0/mo (free)"
          : p.days_with_data === 0
          ? "—"
          : `${projUsd(p.projected_month_usd)}/mo`}
      </span>
    </div>
  );
}

export function SpendingPanel({ workspaceID }: { workspaceID: string }) {
  const [global, setGlobal] = useState<main.PoolCostProjection | null>(null);
  const [pools, setPools] = useState<main.PoolCostProjection[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    Promise.all([
      GetGlobalCostProjection(),
      GetWorkspaceCostProjection(workspaceID),
    ])
      .then(([g, ps]) => {
        setGlobal(g ?? null);
        setPools(ps ?? []);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [workspaceID]);

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>;

  return (
    <div className="space-y-4 text-sm">
      <div className="text-slate-300 font-medium">Spending overview</div>

      {global && <GlobalSummary global={global} />}

      {pools.length > 0 && (
        <div>
          <div className="text-xs uppercase tracking-wide text-slate-500 mb-1">Per-pool breakdown</div>
          <div className="border border-slate-800 rounded p-2 bg-slate-950/40">
            {pools.map((p) => (
              <PoolProjectionRow key={p.pool_id} p={p} />
            ))}
          </div>
        </div>
      )}

      <div className="text-[10px] text-slate-600 space-y-0.5">
        <p>Color thresholds: green &lt; ${COLOR_THRESHOLDS.amber}/mo · amber ${COLOR_THRESHOLDS.amber}–${COLOR_THRESHOLDS.red}/mo · red &gt; ${COLOR_THRESHOLDS.red}/mo</p>
        <p>Local pools (Ollama, LM Studio, Codex) are always shown as $0 — no per-token API billing.</p>
        <p>Projection method: straight-line from 30-day rolling total. See docs/cost-expectations.md for typical workload examples.</p>
      </div>
    </div>
  );
}
