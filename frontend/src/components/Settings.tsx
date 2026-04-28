// Stub. Phase 1 Day 1: workspaces, agent count, Ollama URL, polling interval.
export function Settings({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[480px] bg-slate-900 border border-slate-700 rounded-lg p-4">
        <div className="flex items-center justify-between mb-3">
          <div className="text-slate-200">Settings</div>
          <button onClick={onClose} className="text-slate-400">✕</button>
        </div>
        <ul className="text-sm text-slate-400 space-y-2">
          <li>Workspaces — Phase 1 Day 2</li>
          <li>Worker pool capacity (1-5) — Phase 5</li>
          <li>Ollama URL / model — Phase 3</li>
          <li>Notification preferences — Phase 1 Day 7</li>
          <li>Polling interval — Phase 1 Day 3</li>
        </ul>
      </div>
    </div>
  );
}
