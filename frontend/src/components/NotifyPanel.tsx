import { useEffect, useState } from "react";
import { GetNotifyPrefs, SetNotifyPrefs } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";

export function NotifyPanel() {
  const [muted, setMuted] = useState(false);
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  useEffect(() => {
    GetNotifyPrefs().then((p) => {
      setMuted(p.muted);
      setStart(p.quiet_start);
      setEnd(p.quiet_end);
    });
  }, []);

  async function save() {
    setBusy(true);
    setStatus(null);
    try {
      await SetNotifyPrefs(new main.NotifyPrefs({ muted, quiet_start: start, quiet_end: end }));
      setStatus("Saved.");
    } catch (e: any) {
      setStatus(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3 text-sm">
      <label className="flex items-center gap-2">
        <input type="checkbox" checked={muted} onChange={(e) => setMuted(e.target.checked)} />
        <span className="text-slate-300">Mute all OS notifications</span>
      </label>

      <div>
        <div className="text-xs text-slate-500 mb-1">Quiet hours (24h, leave blank to disable)</div>
        <div className="flex items-center gap-2">
          <input
            type="time"
            value={start}
            onChange={(e) => setStart(e.target.value)}
            className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          />
          <span className="text-slate-500">→</span>
          <input
            type="time"
            value={end}
            onChange={(e) => setEnd(e.target.value)}
            className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          />
        </div>
        <div className="text-xs text-slate-500 mt-1">
          Wraps midnight is fine — e.g. 22:00 → 07:00.
        </div>
      </div>

      <div className="flex items-center gap-2 pt-2 border-t border-slate-800">
        <button
          onClick={save}
          disabled={busy}
          className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
        >
          Save
        </button>
        {status && <span className="text-xs text-slate-400">{status}</span>}
      </div>
    </div>
  );
}
