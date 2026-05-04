import { useEffect, useRef, useState } from "react";
import { RecentLogs } from "../../wailsjs/go/main/App";
import { logbuffer } from "../../wailsjs/go/models";
import { noAutoCorrect } from "../lib/inputs";

const LEVEL_COLOR: Record<string, string> = {
  info: "text-slate-300",
  stderr: "text-amber-300",
};

export function LogsPanel() {
  const [entries, setEntries] = useState<logbuffer.Entry[]>([]);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [filter, setFilter] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  async function refresh() {
    try {
      const e = await RecentLogs();
      setEntries(e ?? []);
    } catch {}
  }

  useEffect(() => {
    refresh();
    if (!autoRefresh) return;
    const t = setInterval(refresh, 1500);
    return () => clearInterval(t);
  }, [autoRefresh]);

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [entries.length]);

  const filtered = filter
    ? entries.filter((e) => `${e.source} ${e.text}`.toLowerCase().includes(filter.toLowerCase()))
    : entries;

  return (
    <div className="space-y-2 text-sm flex flex-col h-full min-h-0">
      <div className="flex items-center gap-2">
        <input
          {...noAutoCorrect}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter…"
          className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200"
        />
        <label className="text-xs text-slate-400 flex items-center gap-1">
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={(e) => setAutoRefresh(e.target.checked)}
          />
          auto
        </label>
        <button
          onClick={refresh}
          className="text-xs px-2 py-1 border border-slate-700 rounded hover:bg-slate-800"
        >
          ↻
        </button>
        <span className="text-xs text-slate-500">{filtered.length} / {entries.length}</span>
      </div>
      <div
        ref={ref}
        className="flex-1 min-h-0 overflow-y-auto rounded border border-slate-800 bg-slate-950 px-2 py-1 font-mono text-[11px] leading-relaxed"
      >
        {filtered.length === 0 ? (
          <div className="text-slate-600 px-2 py-3">no log entries yet</div>
        ) : (
          filtered.map((e, i) => (
            <div key={i} className="flex gap-2">
              <span className="shrink-0 text-slate-600">
                {new Date(e.ts).toLocaleTimeString()}
              </span>
              <span className="shrink-0 text-slate-500 w-16 truncate">{e.source}</span>
              <span className={LEVEL_COLOR[e.level] ?? "text-slate-300"}>{e.text}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
