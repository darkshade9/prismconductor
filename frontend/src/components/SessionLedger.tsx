import { useEffect, useRef, useState } from "react";
import { GetSessionLedger, ExportSessionLedgerCSV } from "../../wailsjs/go/main/App";

// SessionLedgerRow mirrors types.SessionLedgerRow on the backend (issue #298).
type SessionLedgerRow = {
  session_id: string;
  role: string;
  pool_id: string;
  pool_name: string;
  provider: string;
  model: string;
  started_at: string;
  duration_s: number;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  outcome: string;
  failure_cause?: { kind: string; reason?: string } | null;
};

// Rows ≤ this threshold use a popover; larger histories use a modal.
const POPOVER_THRESHOLD = 8;

type SortKey = "role" | "pool_name" | "model" | "started_at" | "duration_s" | "cost_usd" | "input_tokens" | "outcome";
type SortDir = "asc" | "desc";

type Props = {
  workspaceID: string;
  issueNumber: number;
  anchor: { x: number; y: number } | null;
  onClose: () => void;
};

export function SessionLedger({ workspaceID, issueNumber, anchor, onClose }: Props) {
  const [rows, setRows] = useState<SessionLedgerRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>("started_at");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const [roleFolded, setRoleFolded] = useState(true);
  const [exporting, setExporting] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    GetSessionLedger(workspaceID, issueNumber)
      .then((data: any[]) => setRows(data ?? []))
      .catch((e: any) => setErr(String(e?.message ?? e)))
      .finally(() => setLoading(false));
  }, [workspaceID, issueNumber]);

  // Close on outside click.
  useEffect(() => {
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [onClose]);

  // Close on Escape.
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onClose]);

  const sorted = [...rows].sort((a, b) => {
    let cmp = 0;
    switch (sortKey) {
      case "role":        cmp = a.role.localeCompare(b.role); break;
      case "pool_name":   cmp = a.pool_name.localeCompare(b.pool_name); break;
      case "model":       cmp = a.model.localeCompare(b.model); break;
      case "started_at":  cmp = a.started_at.localeCompare(b.started_at); break;
      case "duration_s":  cmp = a.duration_s - b.duration_s; break;
      case "cost_usd":    cmp = a.cost_usd - b.cost_usd; break;
      case "input_tokens":cmp = a.input_tokens + a.output_tokens - (b.input_tokens + b.output_tokens); break;
      case "outcome":     cmp = a.outcome.localeCompare(b.outcome); break;
    }
    return sortDir === "asc" ? cmp : -cmp;
  });

  function toggleSort(key: SortKey) {
    if (sortKey === key) setSortDir(d => d === "asc" ? "desc" : "asc");
    else { setSortKey(key); setSortDir("desc"); }
  }

  const totalCost = rows.reduce((s, r) => s + r.cost_usd, 0);
  const totalIn   = rows.reduce((s, r) => s + r.input_tokens, 0);
  const totalOut  = rows.reduce((s, r) => s + r.output_tokens, 0);
  const totalSecs = rows.reduce((s, r) => s + r.duration_s, 0);

  // Role roll-up aggregates.
  const roleMap: Record<string, { count: number; cost: number; secs: number }> = {};
  for (const r of rows) {
    const e = roleMap[r.role] ?? { count: 0, cost: 0, secs: 0 };
    e.count++;
    e.cost += r.cost_usd;
    e.secs += r.duration_s;
    roleMap[r.role] = e;
  }

  async function doExport() {
    setExporting(true);
    try {
      await ExportSessionLedgerCSV(workspaceID, issueNumber);
    } catch (e: any) {
      alert(String(e?.message ?? e));
    } finally {
      setExporting(false);
    }
  }

  const useModal = !loading && rows.length > POPOVER_THRESHOLD;

  const headerRow = (
    <div className="flex items-center justify-between px-3 py-2 border-b border-slate-700 bg-slate-800/50">
      <span className="text-slate-200 text-xs font-semibold">
        Session Ledger <span className="text-slate-500 font-normal ml-1">#{issueNumber}</span>
        {!loading && <span className="ml-2 text-slate-500 font-normal">{rows.length} session{rows.length !== 1 ? "s" : ""}</span>}
      </span>
      <div className="flex items-center gap-2">
        <button
          onClick={doExport}
          disabled={exporting || loading || rows.length === 0}
          className="text-[10px] text-slate-400 hover:text-slate-200 disabled:opacity-40 border border-slate-700 rounded px-1.5 py-0.5"
        >
          {exporting ? "Saving…" : "Export CSV"}
        </button>
        <button onClick={onClose} className="text-slate-400 hover:text-slate-200 text-xs leading-none">✕</button>
      </div>
    </div>
  );

  const Th = ({ label, col, className = "" }: { label: string; col: SortKey; className?: string }) => (
    <th
      className={`px-2 py-1 text-left text-[10px] text-slate-400 font-medium cursor-pointer select-none hover:text-slate-200 ${className}`}
      onClick={() => toggleSort(col)}
    >
      {label}{sortKey === col ? (sortDir === "asc" ? " ↑" : " ↓") : ""}
    </th>
  );

  const tableContent = (
    <div className="overflow-auto max-h-[400px]">
      {loading ? (
        <div className="px-3 py-4 text-xs text-slate-500">Loading…</div>
      ) : err ? (
        <div className="px-3 py-4 text-xs text-red-400">{err}</div>
      ) : rows.length === 0 ? (
        <div className="px-3 py-4 text-xs text-slate-500">No sessions recorded for this issue.</div>
      ) : (
        <table className="w-full text-[10px] font-mono border-collapse">
          <thead className="sticky top-0 bg-slate-900 z-10">
            <tr>
              <Th label="Role"    col="role"         className="w-20" />
              <Th label="Pool"    col="pool_name"    className="w-20" />
              <Th label="Model"   col="model"        className="w-24" />
              <Th label="Started" col="started_at"   className="w-28" />
              <Th label="Time"    col="duration_s"   className="w-16 text-right" />
              <Th label="Cost"    col="cost_usd"     className="w-16 text-right" />
              <Th label="Tokens"  col="input_tokens" className="w-20 text-right" />
              <Th label="Outcome" col="outcome"      className="w-20" />
            </tr>
          </thead>
          <tbody>
            {sorted.map((r) => (
              <tr key={r.session_id} className="border-t border-slate-800 hover:bg-slate-800/40">
                <td className="px-2 py-1 text-slate-300">{r.role}</td>
                <td className="px-2 py-1 text-slate-400 truncate max-w-[80px]" title={r.pool_name}>{r.pool_name}</td>
                <td className="px-2 py-1 text-slate-400 truncate max-w-[96px]" title={r.model}>{r.model || "—"}</td>
                <td className="px-2 py-1 text-slate-400">{formatDate(r.started_at)}</td>
                <td className="px-2 py-1 text-right text-slate-300">{formatDuration(r.duration_s)}</td>
                <td className="px-2 py-1 text-right text-emerald-400">{formatCost(r.cost_usd)}</td>
                <td className="px-2 py-1 text-right text-slate-400">{formatTokens(r.input_tokens + r.output_tokens)}</td>
                <td className="px-2 py-1">
                  <OutcomeChip outcome={r.outcome} cause={r.failure_cause} />
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr className="border-t-2 border-slate-700 bg-slate-900/60 font-semibold">
              <td className="px-2 py-1 text-slate-400 text-[10px]" colSpan={4}>Totals ({rows.length})</td>
              <td className="px-2 py-1 text-right text-slate-300">{formatDuration(totalSecs)}</td>
              <td className="px-2 py-1 text-right text-emerald-400">{formatCost(totalCost)}</td>
              <td className="px-2 py-1 text-right text-slate-400">{formatTokens(totalIn + totalOut)}</td>
              <td />
            </tr>
          </tfoot>
        </table>
      )}
    </div>
  );

  const roleRollup = !loading && !err && rows.length > 0 && (
    <div className="border-t border-slate-700">
      <button
        className="w-full flex items-center justify-between px-3 py-1.5 text-[10px] text-slate-400 hover:text-slate-200"
        onClick={() => setRoleFolded(f => !f)}
      >
        <span>By role</span>
        <span>{roleFolded ? "▸" : "▾"}</span>
      </button>
      {!roleFolded && (
        <table className="w-full text-[10px] font-mono px-3 pb-2">
          <thead>
            <tr>
              <th className="px-2 text-left text-slate-500 font-normal">Role</th>
              <th className="px-2 text-right text-slate-500 font-normal">Sessions</th>
              <th className="px-2 text-right text-slate-500 font-normal">Time</th>
              <th className="px-2 text-right text-slate-500 font-normal">Cost</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(roleMap).sort(([a], [b]) => a.localeCompare(b)).map(([role, agg]) => (
              <tr key={role} className="border-t border-slate-800">
                <td className="px-2 py-0.5 text-slate-300">{role}</td>
                <td className="px-2 py-0.5 text-right text-slate-400">{agg.count}</td>
                <td className="px-2 py-0.5 text-right text-slate-300">{formatDuration(agg.secs)}</td>
                <td className="px-2 py-0.5 text-right text-emerald-400">{formatCost(agg.cost)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );

  // Note: model reflects pool's current config — pools edited after a session
  // ran will show their new model here (resolve-on-read, no migration needed).
  const caveat = !loading && rows.length > 0 && (
    <div className="px-3 py-1.5 text-[9px] text-slate-600 border-t border-slate-800">
      Model reflects pool's current config. Edited pools show new model values.
    </div>
  );

  const inner = (
    <div ref={ref} className="flex flex-col bg-slate-900 border border-slate-700 rounded-lg shadow-2xl overflow-hidden min-w-[620px]">
      {headerRow}
      {tableContent}
      {roleRollup}
      {caveat}
    </div>
  );

  if (useModal) {
    return (
      <div
        className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]"
        onClick={(e) => e.stopPropagation()}
        onMouseDown={(e) => e.stopPropagation()}
        onPointerDown={(e) => e.stopPropagation()}
      >
        <div className="max-w-[800px] w-full mx-4">
          {inner}
        </div>
      </div>
    );
  }

  // Popover positioning.
  const left = anchor ? Math.min(anchor.x, window.innerWidth - 640) : 100;
  const top  = anchor ? anchor.y + 8 : 100;
  const style: React.CSSProperties = { position: "fixed", zIndex: 100, left, top };

  return (
    <div style={style} onClick={(e) => e.stopPropagation()} onMouseDown={(e) => e.stopPropagation()}>
      {inner}
    </div>
  );
}

// --- helpers ---

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "—";
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function formatDuration(secs: number): string {
  if (!secs || secs <= 0) return "—";
  if (secs < 60) return `${Math.round(secs)}s`;
  if (secs < 3600) {
    const m = Math.floor(secs / 60);
    const s = Math.round(secs % 60);
    return `${m}m${String(s).padStart(2, "0")}s`;
  }
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  return `${h}h${String(m).padStart(2, "0")}m`;
}

function formatCost(usd: number): string {
  if (usd <= 0) return "$0.00";
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

function formatTokens(n: number): string {
  if (!n || n <= 0) return "—";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

type FailureCause = { kind: string; reason?: string } | null | undefined;

function OutcomeChip({ outcome, cause }: { outcome: string; cause: FailureCause }) {
  const label = outcome === "completed" ? "done"
    : outcome === "failed" ? "failed"
    : outcome === "blocked" ? "blocked"
    : outcome === "running" ? "running"
    : outcome === "waiting_for_input" ? "waiting"
    : outcome;

  const cls = outcome === "completed" ? "text-emerald-400"
    : outcome === "failed" || outcome === "blocked" ? "text-red-400"
    : outcome === "running" || outcome === "waiting_for_input" ? "text-blue-400"
    : "text-slate-400";

  const title = cause ? (cause.reason || cause.kind || "") : undefined;

  return (
    <span className={cls} title={title}>
      {label}
    </span>
  );
}
