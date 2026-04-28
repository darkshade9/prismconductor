import { useEffect, useRef } from "react";
import { useSessionStore } from "../stores/sessionStore";

export function SessionDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sessions, activeId } = useSessionStore();
  const ref = useRef<HTMLDivElement>(null);
  const sess = activeId ? sessions[activeId] : null;

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [sess?.lines.length]);

  if (!open) return null;
  return (
    <div className="fixed right-0 top-0 h-full w-[480px] bg-slate-950 border-l border-slate-800 flex flex-col">
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800">
        <div className="text-sm text-slate-200">
          Session {activeId ? activeId.slice(0, 8) : "—"}
        </div>
        <button className="text-slate-400 hover:text-slate-200" onClick={onClose}>
          ✕
        </button>
      </div>
      <div ref={ref} className="flex-1 overflow-y-auto px-3 py-2 font-mono text-xs whitespace-pre-wrap">
        {sess?.lines.length ? sess.lines.join("\n") : <span className="text-slate-600">no output yet…</span>}
      </div>
      <div className="px-3 py-2 border-t border-slate-800 text-xs text-slate-500">
        Pause · Kill · Send input — Phase 1 Day 7
      </div>
    </div>
  );
}
