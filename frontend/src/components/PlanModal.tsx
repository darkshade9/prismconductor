// Stub for Phase 4. Renders plan markdown + question form.
export function PlanModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[640px] max-h-[80vh] bg-slate-900 border border-slate-700 rounded-lg overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">Plan: #1130</div>
          <button onClick={onClose} className="text-slate-400">✕</button>
        </div>
        <div className="px-4 py-3 text-sm text-slate-400">
          Phase 4 stub — markdown + Q&A form lands with `/conductor-plan` JSON wiring.
        </div>
      </div>
    </div>
  );
}
