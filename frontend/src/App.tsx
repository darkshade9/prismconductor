import { useEffect, useMemo, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { GetWorkerPoolStatus, ListSessions, RefreshIssuesNow, SpawnDemo, SpawnPlanForIssue } from "../wailsjs/go/main/App";
import { types } from "../wailsjs/go/models";
import { useSessionStore } from "./stores/sessionStore";
import { useWorkspaceStore } from "./stores/workspaceStore";
import { Board } from "./components/Board";
import { GoalPane } from "./components/GoalPane";
import { WorkspaceSwitcher } from "./components/WorkspaceSwitcher";
import { SessionDrawer } from "./components/SessionDrawer";
import { Settings } from "./components/Settings";
import { PlanModal } from "./components/PlanModal";
import { LoginButton } from "./components/LoginButton";
import { AddIssueQuick } from "./components/AddIssueQuick";
import { useIssueStore } from "./stores/issueStore";
import { useGoalStore } from "./stores/goalStore";

type PlanTarget = { workspace_id: string; number: number } | null;

function App() {
  const appendLine = useSessionStore((s) => s.appendLine);
  const setMeta = useSessionStore((s) => s.setMeta);
  const setActive = useSessionStore((s) => s.setActive);
  const refreshWorkspaces = useWorkspaceStore((s) => s.refresh);
  const selectedWorkspace = useWorkspaceStore((s) => s.selectedID);
  const refreshIssues = useIssueStore((s) => s.refresh);
  const refreshGoals = useGoalStore((s) => s.refresh);
  const issues = useIssueStore((s) => s.issues);
  const [poolStatus, setPoolStatus] = useState({ active: 0, capacity: 2 });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [planTarget, setPlanTarget] = useState<PlanTarget>(null);
  const planIssue = useMemo(() => {
    if (!planTarget) return null;
    return (
      issues.find(
        (i) => i.workspace_id === planTarget.workspace_id && i.number === planTarget.number,
      ) ?? null
    );
  }, [issues, planTarget]);
  const [busy, setBusy] = useState(false);
  const [issueInput, setIssueInput] = useState("");

  useEffect(() => {
    refreshWorkspaces();
    refreshGoals();
    ListSessions().then((all) => {
      (all ?? []).forEach((s) => setMeta(s));
    });
    const offLine = EventsOn("session.line", (data: { session_id: string; line: string }) => {
      appendLine(data.session_id, data.line);
    });
    const offState = EventsOn("session.state", (sess: types.Session) => {
      setMeta(sess);
    });
    const offGoal = EventsOn("bus.goal_activated", () => {
      refreshGoals();
      refreshIssues(selectedWorkspace ?? "");
    });
    const offGoalUpd = EventsOn("bus.goal_updated", () => refreshGoals());
    const offIssue = EventsOn("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    const offPlanReady = EventsOn(
      "bus.plan_ready",
      (data: { workspace_id: string; issue_number: number; revision: number }) => {
        refreshIssues(selectedWorkspace ?? "");
        setPlanTarget({ workspace_id: data.workspace_id, number: data.issue_number });
      },
    );
    const offPlanApproved = EventsOn("bus.plan_approved", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhPoll = EventsOn("bus.github_poll_done", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhIssue = EventsOn("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhClosed = EventsOn("bus.issue_closed", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhLabel = EventsOn("bus.issue_label_changed", () => refreshIssues(selectedWorkspace ?? ""));
    const refreshPool = () => GetWorkerPoolStatus().then(setPoolStatus).catch(() => {});
    refreshPool();
    const offPoolFreed = EventsOn("bus.worker_slot_freed", refreshPool);
    const offPoolChanged = EventsOn("bus.agent_count_changed", refreshPool);
    const offPoolState = EventsOn("session.state", refreshPool);
    return () => {
      if (typeof offLine === "function") offLine();
      if (typeof offState === "function") offState();
      if (typeof offGoal === "function") offGoal();
      if (typeof offGoalUpd === "function") offGoalUpd();
      if (typeof offIssue === "function") offIssue();
      if (typeof offPlanReady === "function") offPlanReady();
      if (typeof offPlanApproved === "function") offPlanApproved();
      if (typeof offPoolFreed === "function") offPoolFreed();
      if (typeof offPoolChanged === "function") offPoolChanged();
      if (typeof offPoolState === "function") offPoolState();
      if (typeof offGhPoll === "function") offGhPoll();
      if (typeof offGhIssue === "function") offGhIssue();
      if (typeof offGhClosed === "function") offGhClosed();
      if (typeof offGhLabel === "function") offGhLabel();
    };
  }, [appendLine, setMeta, refreshWorkspaces, refreshGoals, refreshIssues, selectedWorkspace]);

  async function spawnDemo() {
    setBusy(true);
    try {
      const sess = await SpawnDemo();
      if (sess?.id) {
        setMeta(sess);
        setActive(sess.id);
      }
      setDrawerOpen(true);
    } catch (e) {
      appendLine("error", String(e));
      setActive("error");
      setDrawerOpen(true);
    } finally {
      setBusy(false);
    }
  }

  async function spawnIssue() {
    const workspaces = useWorkspaceStore.getState().workspaces;
    const target = selectedWorkspace ?? workspaces[0]?.id;
    if (!target) {
      alert("Add a workspace in Settings first.");
      return;
    }
    const num = parseInt(issueInput, 10);
    if (!num) return;
    setBusy(true);
    try {
      const sess = await SpawnPlanForIssue(target, num);
      if (sess?.id) {
        setMeta(sess);
        setActive(sess.id);
      }
      setDrawerOpen(true);
      setIssueInput("");
    } catch (e: any) {
      alert(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="h-screen flex flex-col bg-slate-950 text-slate-200">
      <header className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
        <div className="font-semibold">PrismConductor</div>
        <div className="flex items-center gap-2">
          {selectedWorkspace && (
            <>
              <input
                type="number"
                min={1}
                value={issueInput}
                onChange={(e) => setIssueInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && spawnIssue()}
                placeholder="issue #"
                className="w-20 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs"
              />
              <button
                onClick={spawnIssue}
                disabled={busy || !issueInput}
                className="text-xs bg-sky-700 hover:bg-sky-600 disabled:opacity-40 px-2 py-1 rounded"
                title={issueInput ? "" : "type an issue number first"}
              >
                Plan issue
              </button>
            </>
          )}
          <button
            onClick={spawnDemo}
            disabled={busy}
            className="text-xs bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 px-2 py-1 rounded"
          >
            {busy ? "…" : "Spawn `claude --version`"}
          </button>
          <button
            onClick={() => RefreshIssuesNow().catch((e) => alert(String(e?.message ?? e)))}
            className="text-xs border border-slate-700 hover:bg-slate-800 px-2 py-1 rounded"
            title="Re-fetch GitHub issues for every workspace now"
          >
            ↻ Refresh
          </button>
          <button
            onClick={() => setDrawerOpen((v) => !v)}
            className="text-xs border border-slate-700 hover:bg-slate-800 px-2 py-1 rounded"
          >
            Drawer
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
      <div className="px-4 py-1 border-b border-slate-800 flex items-center gap-3">
        <span className="text-xs text-slate-500">+ test issue:</span>
        <AddIssueQuick />
      </div>
      <main className="flex-1 overflow-hidden pt-3">
        <Board
          onCardClick={(iss) =>
            setPlanTarget({ workspace_id: iss.workspace_id, number: iss.number })
          }
        />
      </main>
      <footer className="px-4 py-2 border-t border-slate-800 text-xs text-slate-500">
        Worker pool: {poolStatus.active}/{poolStatus.capacity} active
      </footer>

      <SessionDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
      <Settings open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <PlanModal
        open={planTarget !== null}
        onClose={() => setPlanTarget(null)}
        issue={planIssue}
      />
    </div>
  );
}

export default App;
