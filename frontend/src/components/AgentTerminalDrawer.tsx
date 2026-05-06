import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { EventsOnWrapped } from "../lib/eventBus";
import { KillAgentSession, ResizeAgentTerm, WriteAgentInput } from "../../wailsjs/go/main/App";
import { useAgentTerminalStore } from "../stores/useAgentTerminalStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { AgentSettingsPane } from "./AgentSettingsPane";

const COLLAPSED_HEIGHT = 32;
const MIN_HEIGHT = 120;

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
  const height = useAgentTerminalStore((s) => s.height);
  const collapsed = useAgentTerminalStore((s) => s.collapsed);
  const setHeight = useAgentTerminalStore((s) => s.setHeight);
  const setCollapsed = useAgentTerminalStore((s) => s.setCollapsed);
  const workspaceID = useWorkspaceStore((s) => s.selectedID) ?? "";

  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [busy, setBusy] = useState(false);

  const session = sessions[workspaceID] ?? null;
  const cwd = session?.cwd ?? "";

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
    const off = EventsOnWrapped(
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
    const off = EventsOnWrapped(
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

  async function handleClose() {
    if (workspaceID && session) {
      setBusy(true);
      try {
        await KillAgentSession(workspaceID);
        clearSession(workspaceID);
      } finally {
        setBusy(false);
      }
    }
    setDrawerOpen(false);
  }

  function handleResizeStart(e: React.PointerEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startH = collapsed ? height : height;

    function onMove(ev: PointerEvent) {
      const delta = startY - ev.clientY;
      const next = Math.min(
        Math.max(startH + delta, MIN_HEIGHT),
        window.innerHeight - 80,
      );
      // Update store without persisting on every frame — persist on pointer up.
      useAgentTerminalStore.setState({ height: next, expandedHeight: next });
    }

    function onUp(ev: PointerEvent) {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      const delta = startY - ev.clientY;
      const next = Math.min(
        Math.max(startH + delta, MIN_HEIGHT),
        window.innerHeight - 80,
      );
      setHeight(next);
    }

    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  if (!drawerOpen) return null;

  const drawerHeight = collapsed ? COLLAPSED_HEIGHT : height;

  return (
    <div
      className="fixed bottom-0 left-0 right-0 bg-slate-950 border-t border-slate-700 flex flex-col z-40 shadow-2xl"
      style={{ height: drawerHeight }}
    >
      {/* Resize handle — only shown when not collapsed */}
      {!collapsed && (
        <div
          className="absolute top-0 left-0 right-0 h-1.5 cursor-row-resize hover:bg-sky-600/40 active:bg-sky-600/60 transition-colors"
          onPointerDown={handleResizeStart}
          title="Drag to resize"
        />
      )}

      {/* Header bar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-slate-800 shrink-0">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <span className="text-xs font-semibold text-slate-300 tracking-wide shrink-0">
            Agent Terminal
          </span>
          {session ? (
            <span className="flex items-center gap-1.5 text-xs text-emerald-400 shrink-0">
              <span className="inline-block w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
              {session.agent_bin}
              <span className="text-slate-500 font-mono text-[10px]">pid {session.pid}</span>
            </span>
          ) : (
            <span className="text-xs text-slate-600 shrink-0">no session</span>
          )}
          {cwd && (
            <span
              className="text-[10px] text-slate-500 font-mono min-w-0 flex-1 overflow-hidden"
              style={{
                direction: "rtl",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                unicodeBidi: "plaintext",
              }}
              title={cwd}
            >
              {cwd}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
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
            onClick={() => setCollapsed(!collapsed)}
            aria-label="Minimize agent terminal"
            title={collapsed ? "Expand agent terminal" : "Minimize agent terminal"}
            className="ml-1 text-slate-500 hover:text-slate-200 text-xs px-1.5 py-0.5 rounded hover:bg-slate-800"
          >
            {collapsed ? "▲" : "▼"}
          </button>

          <button
            onClick={handleClose}
            disabled={busy}
            aria-label="Close agent terminal"
            className="ml-1 text-slate-500 hover:text-slate-200 text-xs px-1.5 py-0.5 rounded hover:bg-slate-800 disabled:opacity-40"
          >
            ✕
          </button>
        </div>
      </div>

      {/* Agent settings pane (collapsible) */}
      {showSettings && !collapsed && (
        <AgentSettingsPane
          workspaceID={workspaceID}
          onClose={() => setShowSettings(false)}
        />
      )}

      {/* xterm.js viewport — kept mounted when collapsed to preserve PTY */}
      <div
        ref={containerRef}
        className="flex-1 overflow-hidden p-1 bg-[#020617]"
        style={{ display: collapsed ? "none" : undefined }}
      />
    </div>
  );
}
