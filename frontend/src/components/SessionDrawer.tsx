import { useEffect, useRef, useState } from "react";
import { KillSession, ReadTranscript, SendInput } from "../../wailsjs/go/main/App";
import { useSessionStore } from "../stores/sessionStore";
import { CopyMenu, CopyAction } from "./CopyMenu";

const STATE_COLOR: Record<string, string> = {
  running: "text-emerald-400",
  waiting_for_input: "text-amber-300",
  blocked: "text-red-400",
  completed: "text-slate-400",
  failed: "text-red-500",
};

type Role = "user" | "asst" | "sys" | "tool" | "res" | "err" | "live" | "done" | "raw";

const ROLE_META: Record<Role, { label: string; cls: string; bg: string }> = {
  user: { label: "USER", cls: "text-sky-300", bg: "bg-sky-950/40" },
  asst: { label: "ASSIST", cls: "text-emerald-300", bg: "bg-emerald-950/30" },
  live: { label: "ASSIST", cls: "text-emerald-200", bg: "bg-emerald-950/40" },
  sys: { label: "SYSTEM", cls: "text-slate-400", bg: "bg-slate-900/60" },
  tool: { label: "TOOL", cls: "text-purple-300", bg: "bg-purple-950/30" },
  res: { label: "RESULT", cls: "text-slate-300", bg: "bg-slate-900/40" },
  err: { label: "ERROR", cls: "text-red-300", bg: "bg-red-950/40" },
  done: { label: "DONE", cls: "text-emerald-400", bg: "bg-emerald-950/30" },
  raw: { label: "", cls: "text-slate-300", bg: "" },
};

function parseLine(raw: string): { role: Role; text: string } {
  const m = raw.match(/^@(user|asst|sys|tool|res|err|live|done)\s+(.*)$/s);
  if (m) {
    return { role: m[1] as Role, text: m[2] };
  }
  return { role: "raw", text: raw };
}

export function SessionDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sessions, activeId, loadTranscript } = useSessionStore();
  const ref = useRef<HTMLDivElement>(null);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [verbose, setVerbose] = useState(false);
  const [lineMenu, setLineMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);
  const sess = activeId ? sessions[activeId] : null;
  const noisyRoles = new Set<Role>(["tool", "res", "sys"]);
  const visibleLines = (sess?.lines ?? []).filter((raw) => {
    if (verbose) return true;
    return !noisyRoles.has(parseLine(raw).role);
  });

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [sess?.lines.length, sess?.lines[sess.lines.length - 1]]);

  useEffect(() => {
    if (sess && sess.meta && sess.lines.length === 0) {
      ReadTranscript(sess.id)
        .then((body) => body && loadTranscript(sess.id, body))
        .catch(() => {});
    }
  }, [sess?.id]);

  if (!open) return null;

  const state = sess?.meta?.state;
  const isRunning = state === "running" || state === "waiting_for_input";

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
    <div className="fixed right-0 top-0 h-full w-[520px] bg-slate-950 border-l border-slate-800 flex flex-col">
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800">
        <div className="text-sm text-slate-200 flex items-center gap-2">
          Session {activeId ? activeId.slice(0, 8) : "—"}
          {sess?.meta?.issue_number ? <span className="text-slate-500">#{sess.meta.issue_number}</span> : null}
          {state && (
            <span className={`text-xs ${STATE_COLOR[state] ?? "text-slate-400"}`}>
              {isRunning ? <Spinner /> : "● "}
              {state}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setVerbose((v) => !v)}
            title={verbose ? "Hide tool calls + system events" : "Show all events"}
            className={
              "text-[10px] px-1.5 py-0.5 rounded border " +
              (verbose
                ? "border-slate-600 bg-slate-800 text-slate-200"
                : "border-slate-700 text-slate-400 hover:text-slate-200")
            }
          >
            {verbose ? "verbose" : "compact"}
          </button>
          <button className="text-slate-400 hover:text-slate-200" onClick={onClose}>
            ✕
          </button>
        </div>
      </div>

      <div ref={ref} className="flex-1 overflow-y-auto px-2 py-2 space-y-1 text-xs">
        {visibleLines.length ? (
          visibleLines.map((raw, i) => {
            const { role, text } = parseLine(raw);
            const meta = ROLE_META[role];
            return (
              <div
                key={i}
                className={`rounded px-2 py-1 ${meta.bg} flex gap-2`}
                onContextMenu={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setLineMenu({ x: e.clientX, y: e.clientY, actions: [{ label: "Copy line", text }] });
                }}
              >
                {meta.label && (
                  <span className={`shrink-0 font-mono text-[10px] uppercase tracking-wider ${meta.cls} pt-0.5`}>
                    {meta.label}
                    {role === "live" && <Cursor />}
                  </span>
                )}
                <span className={`whitespace-pre-wrap font-mono leading-relaxed ${meta.cls}`}>{text}</span>
              </div>
            );
          })
        ) : (
          <span className="text-slate-600 px-2">
            {sess?.lines.length ? "all output filtered — toggle verbose to show tool calls" : "no output yet…"}
          </span>
        )}
        {lineMenu && (
          <CopyMenu
            x={lineMenu.x}
            y={lineMenu.y}
            actions={lineMenu.actions}
            onClose={() => setLineMenu(null)}
          />
        )}
      </div>

      <div className="px-3 py-2 border-t border-slate-800 flex items-center gap-2">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && send()}
          placeholder={isRunning ? "Send input… (worker is in -p mode; sending may not affect it)" : "Worker not accepting input"}
          disabled={busy || !activeId || !isRunning}
          className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 disabled:opacity-50"
        />
        <button
          onClick={send}
          disabled={busy || !input || !activeId || !isRunning}
          className="text-xs px-2 py-1 bg-slate-800 border border-slate-700 rounded hover:bg-slate-700 disabled:opacity-50"
        >
          Send
        </button>
        <button
          onClick={kill}
          disabled={busy || !activeId || !isRunning}
          className="text-xs px-2 py-1 border border-red-900 text-red-400 hover:bg-red-950 rounded disabled:opacity-30"
        >
          Kill
        </button>
      </div>
    </div>
  );
}

function Spinner() {
  return (
    <span className="inline-block animate-spin mr-1">◐</span>
  );
}

function Cursor() {
  return <span className="inline-block animate-pulse ml-1">▌</span>;
}
