import { useEffect, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { SpawnDemo } from "../wailsjs/go/main/App";
import { useSessionStore } from "./stores/sessionStore";
import { useWorkspaceStore } from "./stores/workspaceStore";
import { Board } from "./components/Board";
import { GoalPane } from "./components/GoalPane";
import { WorkspaceSwitcher } from "./components/WorkspaceSwitcher";
import { SessionDrawer } from "./components/SessionDrawer";
import { Settings } from "./components/Settings";
import { PlanModal } from "./components/PlanModal";
import { LoginButton } from "./components/LoginButton";

function App() {
  const appendLine = useSessionStore((s) => s.appendLine);
  const setActive = useSessionStore((s) => s.setActive);
  const refreshWorkspaces = useWorkspaceStore((s) => s.refresh);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    refreshWorkspaces();
    const off = EventsOn("session.line", (data: { session_id: string; line: string }) => {
      appendLine(data.session_id, data.line);
    });
    return () => {
      if (typeof off === "function") off();
    };
  }, [appendLine, refreshWorkspaces]);

  async function spawnDemo() {
    setBusy(true);
    try {
      const sess = await SpawnDemo();
      if (sess?.id) setActive(sess.id);
      setDrawerOpen(true);
    } catch (e) {
      appendLine("error", String(e));
      setActive("error");
      setDrawerOpen(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="h-screen flex flex-col bg-slate-950 text-slate-200">
      <header className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
        <div className="font-semibold">PrismConductor</div>
        <div className="flex items-center gap-2">
          <button
            onClick={spawnDemo}
            disabled={busy}
            className="text-xs bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 px-2 py-1 rounded"
          >
            {busy ? "Spawning…" : "Spawn `claude --version`"}
          </button>
          <button
            onClick={() => setPlanOpen(true)}
            className="text-xs border border-slate-700 hover:bg-slate-800 px-2 py-1 rounded"
          >
            Plan modal (stub)
          </button>
          <button
            onClick={() => setSettingsOpen(true)}
            className="text-xs border border-slate-700 hover:bg-slate-800 px-2 py-1 rounded"
          >
            Settings
          </button>
          <LoginButton />
        </div>
      </header>
      <WorkspaceSwitcher />
      <GoalPane />
      <main className="flex-1 overflow-hidden pt-3">
        <Board onCardClick={() => setPlanOpen(true)} />
      </main>
      <footer className="px-4 py-2 border-t border-slate-800 text-xs text-slate-500">
        Worker pool: 0/2 active
      </footer>

      <SessionDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
      <Settings open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <PlanModal open={planOpen} onClose={() => setPlanOpen(false)} />
    </div>
  );
}

export default App;
