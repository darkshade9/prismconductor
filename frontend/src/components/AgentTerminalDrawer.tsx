import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { KillAgentSession, ResizeAgentTerm, WriteAgentInput } from "../../wailsjs/go/main/App";
import { useAgentTerminalStore } from "../stores/useAgentTerminalStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { AgentSettingsPane } from "./AgentSettingsPane";

function encodeInput(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let binary = "";
  bytes.forEach((b) => (binary += String.fromCharCode(b)));
  return btoa(binary);
}

export function AgentTerminalDrawer() {
  const drawerOpen = useAgentTerminalStore((s) => s.drawerOpen);
  const setDrawerOpen = useAgentTerminalStore((s) => s.setDrawerOpen);
  const sessions = useAgentTerminalStore((s) => s.sessions);
  const clearSession = useAgentTerminalStore((s) => s.clearSession);
  const workspaceID = useWorkspaceStore((s) => s.selectedID) ?? "";

  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [busy, setBusy] = useState(false);

  const session = sessions[workspaceID] ?? null;

  // (Re-)initialize the xterm instance whenever the drawer opens.
  useEffect(() => {
    if (!drawerOpen || !containerRef.current) return;

    const term = new Terminal({
      theme: {
        background: "#020617",
        foreground: "#cbd5e1",
        cursor: "#94a3b8",
        selectionBackground: "#334155",
      },
      fontFamily: "ui-monospace, 'Cascadia Code', 'Fira Code', monospace",
      fontSize: 13,
      lineHeight: 1.4,
      scrollback: 5000,
      cursorBlink: true,
      convertEol: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current);

    // Short delay so the container has real dimensions before fitting.
    requestAnimationFrame(() => fit.fit());

    term.onData((data) => {
      if (!workspaceID) return;
      WriteAgentInput(workspaceID, encodeInput(data)).catch(() => {});
    });

    termRef.current = term;
    fitRef.current = fit;

    return () => {
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [drawerOpen]); // intentionally omit workspaceID — terminal outlives workspace switch

  // Resize observer: keep PTY dimensions in sync with the DOM element.
  useEffect(() => {
    if (!drawerOpen || !containerRef.current) return;
    const el = containerRef.current;
    const observer = new ResizeObserver(() => {
      const fit = fitRef.current;
      const term = termRef.current;
      if (!fit || !term) return;
      fit.fit();
      if (workspaceID && term.cols > 0 && term.rows > 0) {
        ResizeAgentTerm(workspaceID, term.cols, term.rows).catch(() => {});
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [drawerOpen, workspaceID]);

  // Stream PTY output into the terminal.
  useEffect(() => {
    const off = EventsOn(
      "agentterm.output",
      (data: { workspace_id: string; data: string }) => {
        if (data.workspace_id !== workspaceID) return;
        const term = termRef.current;
        if (!term) return;
        const raw = Uint8Array.from(atob(data.data), (c) => c.charCodeAt(0));
        term.write(raw);
      },
    );
    return () => {
      if (typeof off === "function") off();
    };
  }, [workspaceID]);

  // Handle session exit.
  useEffect(() => {
    const off = EventsOn(
      "agentterm.exit",
      (data: { workspace_id: string; exit_code: number }) => {
        if (data.workspace_id !== workspaceID) return;
        clearSession(workspaceID);
        termRef.current?.writeln(`\r\n\x1b[2m[process exited: ${data.exit_code}]\x1b[0m`);
      },
    );
    return () => {
      if (typeof off === "function") off();
    };
  }, [workspaceID, clearSession]);

  async function handleKill() {
    if (!workspaceID || !session) return;
    setBusy(true);
    try {
      await KillAgentSession(workspaceID);
      clearSession(workspaceID);
    } finally {
      setBusy(false);
    }
  }

  if (!drawerOpen) return null;

  return (
    <div className="fixed bottom-0 left-0 right-0 h-72 bg-slate-950 border-t border-slate-700 flex flex-col z-40 shadow-2xl">
      {/* Header bar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-slate-800 shrink-0">
        <div className="flex items-center gap-3">
          <span className="text-xs font-semibold text-slate-300 tracking-wide">
            Agent Terminal
          </span>
          {session ? (
            <span className="flex items-center gap-1.5 text-xs text-emerald-400">
              <span className="inline-block w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
              {session.agent_bin}
              <span className="text-slate-500 font-mono text-[10px]">pid {session.pid}</span>
            </span>
          ) : (
            <span className="text-xs text-slate-600">no session</span>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          <button
            onClick={() => setShowSettings((v) => !v)}
            title="Spawn or configure an agent"
            className={
              "text-[11px] px-2 py-0.5 rounded border " +
              (showSettings
                ? "border-sky-600 bg-sky-950/50 text-sky-300"
                : "border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-200")
            }
          >
            + spawn
          </button>

          {session && (
            <button
              onClick={handleKill}
              disabled={busy}
              className="text-[11px] px-2 py-0.5 rounded border border-red-900 text-red-400 hover:bg-red-950/60 disabled:opacity-40"
            >
              kill
            </button>
          )}

          <button
            onClick={() => setDrawerOpen(false)}
            className="ml-1 text-slate-500 hover:text-slate-200 text-xs px-1.5 py-0.5 rounded hover:bg-slate-800"
          >
            ✕
          </button>
        </div>
      </div>

      {/* Agent settings pane (collapsible) */}
      {showSettings && (
        <AgentSettingsPane
          workspaceID={workspaceID}
          onClose={() => setShowSettings(false)}
        />
      )}

      {/* xterm.js viewport */}
      <div ref={containerRef} className="flex-1 overflow-hidden p-1 bg-[#020617]" />
    </div>
  );
}
