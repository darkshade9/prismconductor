import { useEffect, useRef, useState } from "react";
import { KillSession, ReadTranscript, SendInput } from "../../wailsjs/go/main/App";
import { useSessionStore } from "../stores/sessionStore";

const STATE_COLOR: Record<string, string> = {
  running: "text-emerald-400",
  waiting_for_input: "text-amber-300",
  blocked: "text-red-400",
  completed: "text-slate-400",
  failed: "text-red-500",
};

export function SessionDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sessions, activeId, loadTranscript } = useSessionStore();
  const ref = useRef<HTMLDivElement>(null);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const sess = activeId ? sessions[activeId] : null;

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [sess?.lines.length]);

  // If we have a session record but zero lines (e.g., re-attached after restart),
  // pull the transcript from disk.
  useEffect(() => {
    if (sess && sess.meta && sess.lines.length === 0) {
      ReadTranscript(sess.id)
        .then((body) => body && loadTranscript(sess.id, body))
        .catch(() => {});
    }
  }, [sess?.id]);

  if (!open) return null;

  const state = sess?.meta?.state;

  async function send() {
    if (!activeId || !input) return;
    setBusy(true);
    try {
      await SendInput(activeId, input + "\n");
      setInput("");
    } finally {
      setBusy(false);
    }
  }

  async function kill() {
    if (!activeId) return;
    setBusy(true);
    try {
      await KillSession(activeId);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed right-0 top-0 h-full w-[480px] bg-slate-950 border-l border-slate-800 flex flex-col">
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800">
        <div className="text-sm text-slate-200 flex items-center gap-2">
          Session {activeId ? activeId.slice(0, 8) : "—"}
          {sess?.meta?.issue_number ? <span className="text-slate-500">#{sess.meta.issue_number}</span> : null}
          {state && <span className={`text-xs ${STATE_COLOR[state] ?? "text-slate-400"}`}>● {state}</span>}
        </div>
        <button className="text-slate-400 hover:text-slate-200" onClick={onClose}>
          ✕
        </button>
      </div>
      <div ref={ref} className="flex-1 overflow-y-auto px-3 py-2 font-mono text-xs whitespace-pre-wrap">
        {sess?.lines.length ? sess.lines.join("\n") : <span className="text-slate-600">no output yet…</span>}
      </div>
      <div className="px-3 py-2 border-t border-slate-800 flex items-center gap-2">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && send()}
          placeholder="Send input…"
          disabled={busy || !activeId || state === "completed" || state === "failed"}
          className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 disabled:opacity-50"
        />
        <button
          onClick={send}
          disabled={busy || !input || !activeId}
          className="text-xs px-2 py-1 bg-slate-800 border border-slate-700 rounded hover:bg-slate-700 disabled:opacity-50"
        >
          Send
        </button>
        <button
          onClick={kill}
          disabled={busy || !activeId || state === "completed" || state === "failed"}
          className="text-xs px-2 py-1 border border-red-900 text-red-400 hover:bg-red-950 rounded disabled:opacity-30"
        >
          Kill
        </button>
      </div>
    </div>
  );
}
