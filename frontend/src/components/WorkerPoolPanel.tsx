import { useEffect, useState } from "react";
import { GetWorkerPoolStatus, SetWorkerPoolCapacity } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export function WorkerPoolPanel() {
  const [capacity, setCapacity] = useState(2);
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);

  async function refresh() {
    try {
      const s = await GetWorkerPoolStatus();
      setCapacity(s.capacity);
      setActive(s.active);
    } catch {}
  }

  useEffect(() => {
    refresh();
    const offFreed = EventsOn("bus.worker_slot_freed", refresh);
    const offChanged = EventsOn("bus.agent_count_changed", refresh);
    const offState = EventsOn("session.state", refresh);
    return () => {
      if (typeof offFreed === "function") offFreed();
      if (typeof offChanged === "function") offChanged();
      if (typeof offState === "function") offState();
    };
  }, []);

  async function save(n: number) {
    setBusy(true);
    try {
      await SetWorkerPoolCapacity(n);
      await refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3 text-sm">
      <div className="flex items-center gap-3">
        <span className="text-slate-500">Active:</span>
        <span className="text-slate-200 font-mono">{active}</span>
        <span className="text-slate-500">/</span>
        <span className="text-slate-200 font-mono">{capacity}</span>
      </div>

      <div>
        <div className="text-xs text-slate-500 mb-1">Capacity (1–10)</div>
        <div className="flex items-center gap-3">
          <input
            type="range"
            min={1}
            max={10}
            value={capacity}
            onChange={(e) => setCapacity(parseInt(e.target.value, 10))}
            onMouseUp={() => save(capacity)}
            onTouchEnd={() => save(capacity)}
            disabled={busy}
            className="flex-1"
          />
          <span className="font-mono text-slate-200 w-6 text-right">{capacity}</span>
        </div>
      </div>

      <div className="text-xs text-slate-500">
        On increase, the orchestrator pulls the next unblocked TODO items into PLAN. On decrease,
        running workers are not killed; they finish naturally before the slot frees.
      </div>
    </div>
  );
}
