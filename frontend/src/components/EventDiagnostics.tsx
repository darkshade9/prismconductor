import { useEffect, useState, useCallback } from "react";
import { GetEventBusDebugEmits, ReplayEventBusEmit } from "../../wailsjs/go/main/App";
import { wailsbus } from "../../wailsjs/go/models";
import type { EventRecord } from "../lib/eventBus";

type View = "receives" | "sends";

function fmtTime(ts: number | string): string {
  const d = typeof ts === "number" ? new Date(ts) : new Date(ts);
  return d.toLocaleTimeString(undefined, { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3 } as Intl.DateTimeFormatOptions);
}

function RowReceive({ rec }: { rec: EventRecord }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-b border-slate-800 text-[11px]">
      <button
        className="w-full text-left flex items-center gap-2 px-2 py-1 hover:bg-slate-800/50"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="text-slate-500 font-mono shrink-0">{fmtTime(rec.receivedAt)}</span>
        <span className="text-emerald-400 font-mono">{rec.name}</span>
        <span className="text-slate-600 ml-auto">{open ? "▲" : "▼"}</span>
      </button>
      {open && (
        <pre className="px-4 py-2 text-slate-300 overflow-x-auto bg-slate-950/40 text-[10px]">
          {JSON.stringify(rec.payload, null, 2)}
        </pre>
      )}
    </div>
  );
}

function RowSend({ rec, onReplay }: { rec: wailsbus.EmitRecord; onReplay: (name: string) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-b border-slate-800 text-[11px]">
      <button
        className="w-full text-left flex items-center gap-2 px-2 py-1 hover:bg-slate-800/50"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="text-slate-500 font-mono shrink-0">{typeof rec.sent_at === "string" ? fmtTime(rec.sent_at) : "—"}</span>
        <span className="text-sky-400 font-mono">{rec.name}</span>
        <button
          className="ml-auto text-[10px] px-1.5 py-0.5 border border-slate-700 rounded hover:bg-slate-700 text-slate-400"
          onClick={(e) => { e.stopPropagation(); onReplay(rec.name); }}
        >
          replay
        </button>
        <span className="text-slate-600">{open ? "▲" : "▼"}</span>
      </button>
      {open && (
        <pre className="px-4 py-2 text-slate-300 overflow-x-auto bg-slate-950/40 text-[10px]">
          {JSON.stringify(rec.payload, null, 2)}
        </pre>
      )}
    </div>
  );
}

export function EventDiagnostics() {
  const [view, setView] = useState<View>("receives");
  const [filter, setFilter] = useState("");
  const [receives, setReceives] = useState<EventRecord[]>([]);
  const [sends, setSends] = useState<wailsbus.EmitRecord[]>([]);
  const [replayStatus, setReplayStatus] = useState("");

  const refresh = useCallback(() => {
    setReceives(window.__pcEventBuffer?.receives ?? []);
    GetEventBusDebugEmits()
      .then((r) => setSends(r ?? []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 1000);
    return () => clearInterval(id);
  }, [refresh]);

  const handleReplay = useCallback(async (name: string) => {
    try {
      await ReplayEventBusEmit(name);
      setReplayStatus(`replayed: ${name}`);
      setTimeout(() => setReplayStatus(""), 2000);
    } catch (e: unknown) {
      setReplayStatus(`error: ${e instanceof Error ? e.message : String(e)}`);
      setTimeout(() => setReplayStatus(""), 3000);
    }
  }, []);

  const filteredReceives = filter
    ? receives.filter((r) => r.name.includes(filter))
    : receives;

  const filteredSends = filter
    ? sends.filter((s) => s.name.includes(filter))
    : sends;

  // Compute names present in sends but not in receives for the last 500 events,
  // to highlight potential send-without-receive mismatches.
  const receivedNames = new Set(receives.map((r) => r.name));
  const orphanSends = new Set(sends.map((s) => s.name).filter((n) => !receivedNames.has(n)));

  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex items-center gap-2 text-xs text-slate-400">
        <span>Event Bus Diagnostics</span>
        <span className="text-slate-600">·</span>
        <span className="text-emerald-500">{receives.length} received</span>
        <span className="text-slate-600">·</span>
        <span className="text-sky-500">{sends.length} sent</span>
        {orphanSends.size > 0 && (
          <>
            <span className="text-slate-600">·</span>
            <span className="text-amber-400" title="Events sent but no matching receive recorded">
              ⚠ {orphanSends.size} unmatched send{orphanSends.size > 1 ? "s" : ""}
            </span>
          </>
        )}
        {replayStatus && (
          <span className="text-violet-400 ml-2">{replayStatus}</span>
        )}
        <button
          className="ml-auto text-[11px] px-2 py-0.5 border border-slate-700 rounded hover:bg-slate-800 text-slate-400"
          onClick={refresh}
        >
          ↻ refresh
        </button>
      </div>

      <div className="flex items-center gap-2">
        <div className="flex gap-1 text-xs">
          <button
            onClick={() => setView("receives")}
            className={
              "px-2 py-1 rounded " +
              (view === "receives" ? "bg-emerald-900/40 text-emerald-300 border border-emerald-800" : "text-slate-400 hover:text-slate-200")
            }
          >
            Received ({receives.length})
          </button>
          <button
            onClick={() => setView("sends")}
            className={
              "px-2 py-1 rounded " +
              (view === "sends" ? "bg-sky-900/40 text-sky-300 border border-sky-800" : "text-slate-400 hover:text-slate-200")
            }
          >
            Sent ({sends.length})
          </button>
        </div>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter by name…"
          className="flex-1 text-xs bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 placeholder-slate-600 outline-none focus:border-slate-500"
        />
      </div>

      {orphanSends.size > 0 && (
        <div className="text-xs bg-amber-950/30 border border-amber-800/50 rounded px-3 py-2 text-amber-300">
          Send-without-receive: {[...orphanSends].join(", ")}
        </div>
      )}

      <div className="border border-slate-800 rounded overflow-y-auto" style={{ maxHeight: "420px" }}>
        {view === "receives" && (
          filteredReceives.length === 0
            ? <div className="px-3 py-4 text-xs text-slate-500 text-center">No receives yet{filter ? " matching filter" : ""}</div>
            : filteredReceives.map((r, i) => <RowReceive key={i} rec={r} />)
        )}
        {view === "sends" && (
          filteredSends.length === 0
            ? <div className="px-3 py-4 text-xs text-slate-500 text-center">No sends yet{filter ? " matching filter" : ""}</div>
            : filteredSends.map((s, i) => <RowSend key={i} rec={s} onReplay={handleReplay} />)
        )}
      </div>
    </div>
  );
}
