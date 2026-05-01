import { useCallback, useEffect, useMemo, useState } from "react";
import { EventsOn } from "../wailsjs/runtime/runtime";
import {
  GetAutoPullPaused,
  ListArchivedIssues,
  ListPendingPlans,
  ListSessions,
  RefreshIssuesNow,
  SetAutoPullPaused,
  SpawnDemo,
  SpawnPlanForIssue,
} from "../wailsjs/go/main/App";
import { types } from "../wailsjs/go/models";
import { useSessionStore, SessionActivity } from "./stores/sessionStore";
// useSessionStore.getState() is also used directly inside the activity handler.
import { useWorkspaceStore } from "./stores/workspaceStore";
import { Board } from "./components/Board";
import { GoalPane } from "./components/GoalPane";
import { WorkspaceSwitcher } from "./components/WorkspaceSwitcher";
import { SessionDrawer } from "./components/SessionDrawer";
import { Settings } from "./components/Settings";
import { PlanModal } from "./components/PlanModal";
import { ArchivedDrawer } from "./components/ArchivedDrawer";
import { AddIssueQuick } from "./components/AddIssueQuick";
import { useArchivedStore, useIssueStore } from "./stores/issueStore";
import { useGoalStore } from "./stores/goalStore";
import { usePlanReadyStore } from "./stores/planReadyStore";
import { useLabelsStore } from "./stores/labelsStore";
import { usePoolsStore } from "./stores/usePoolsStore";
import { Toast } from "./components/Toast";
import { useToastStore, type Toast as ToastT } from "./stores/toastStore";
import { TitleSearch } from "./components/Header";
import { PoolsHeader } from "./components/PoolsHeader";

type PlanRef = { workspace_id: string; number: number };
type PlanTarget = PlanRef | null;

function App() {
  const appendLine = useSessionStore((s) => s.appendLine);
  const setMeta = useSessionStore((s) => s.setMeta);
  const setActive = useSessionStore((s) => s.setActive);
  const refreshWorkspaces = useWorkspaceStore((s) => s.refresh);
  const selectedWorkspace = useWorkspaceStore((s) => s.selectedID);
  const refreshIssues = useIssueStore((s) => s.refresh);
  const refreshGoals = useGoalStore((s) => s.refresh);
  const markPlanReady = usePlanReadyStore((s) => s.markReady);
  const clearPlanReady = usePlanReadyStore((s) => s.clearReady);
  const issues = useIssueStore((s) => s.issues);
  const [autoPaused, setAutoPaused] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsInitialTab, setSettingsInitialTab] = useState<"workspaces" | "pools" | "skills" | "labels" | "notify" | "logs">("workspaces");
  const [planTarget, setPlanTarget] = useState<PlanTarget>(null);
  const [planQueue, setPlanQueue] = useState<PlanRef[]>([]);
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
  const [archivedOpen, setArchivedOpen] = useState(false);
  const [archivedCount, setArchivedCount] = useState(0);
  const refreshArchived = useArchivedStore((s) => s.refresh);

  const refreshArchivedCount = useCallback(async () => {
    try {
      const list = await ListArchivedIssues(selectedWorkspace ?? "");
      setArchivedCount(list?.length ?? 0);
    } catch {
      setArchivedCount(0);
    }
  }, [selectedWorkspace]);

  useEffect(() => {
    refreshArchivedCount();
  }, [refreshArchivedCount]);

  useEffect(() => {
    refreshWorkspaces();
    refreshGoals();
    ListSessions().then((all) => {
      (all ?? []).forEach((s) => setMeta(s));
    });
    // Rehydrate plan-ready glow on cards whose latest plan revision was
    // never approved/rejected — survives app restarts.
    ListPendingPlans()
      .then((pending) => {
        (pending ?? []).forEach((p) =>
          markPlanReady({
            workspace_id: p.workspace_id,
            issue_number: p.issue_number,
            revision: p.revision,
          }),
        );
      })
      .catch(() => {});
    const offLine = EventsOn("session.line", (data: { session_id: string; line: string }) => {
      appendLine(data.session_id, data.line);
    });
    const offState = EventsOn("session.state", (sess: types.Session) => {
      // eslint-disable-next-line no-console
      console.debug(
        `[bus] session.state id=${sess.id?.slice(0, 8)} ws=${sess.workspace_id} #${sess.issue_number} mode=${sess.mode} state=${sess.state}`,
      );
      setMeta(sess);
    });
    const offActivity = EventsOn("session.activity", (act: SessionActivity) => {
      useSessionStore.getState().setActivity(act);
    });
    const offGoal = EventsOn("bus.goal_activated", () => {
      refreshGoals();
      refreshIssues(selectedWorkspace ?? "");
    });
    const offGoalUpd = EventsOn("bus.goal_updated", () => refreshGoals());
    const offIssue = EventsOn("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    // pending_pool_* events flip Issue.WaitingForPool on the server side.
    // Without re-fetching here, the card stays in the pink "waiting for
    // available agent pool" state visually even after the orchestrator has
    // drained the queue, spawned the worker, and moved the card to
    // IN_PROGRESS — the pool counter will (correctly) show the slot taken
    // while the card display is stale.
    const offPendingEnq = EventsOn(
      "bus.pending_pool_enqueued",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    const offPendingDeq = EventsOn(
      "bus.pending_pool_dequeued",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    // Don't hijack an open PlanModal when a different card's plan arrives.
    // Card glow + toast carry the signal; queue the arrival and drain on close.
    // Any future auto-open path (bus.plan_revised, etc.) must use the same
    // functional-setPlanTarget guard, never an unconditional set.
    const offPlanReady = EventsOn(
      "bus.plan_ready",
      (data: { workspace_id: string; issue_number: number; revision: number }) => {
        refreshIssues(selectedWorkspace ?? "");
        markPlanReady(data);
        setPlanTarget((current) => {
          if (current === null) {
            return { workspace_id: data.workspace_id, number: data.issue_number };
          }
          if (
            current.workspace_id === data.workspace_id &&
            current.number === data.issue_number
          ) {
            return current;
          }
          setPlanQueue((q) => {
            if (
              q.some(
                (e) =>
                  e.workspace_id === data.workspace_id &&
                  e.number === data.issue_number,
              )
            ) {
              return q;
            }
            return [...q, { workspace_id: data.workspace_id, number: data.issue_number }];
          });
          return current;
        });
      },
    );
    const offPROpened = EventsOn(
      "bus.pr_opened",
      (_data: { workspace_id: string; issue_number: number; pr_number: number; pr_url: string }) => {
        refreshIssues(selectedWorkspace ?? "");
      },
    );
    const offPRMerged = EventsOn("bus.pr_merged", () => refreshIssues(selectedWorkspace ?? ""));
    const offPRClosedUnmerged = EventsOn(
      "bus.pr_closed_unmerged",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    const offPlanApproved = EventsOn(
      "bus.plan_approved",
      (data: { workspace_id: string; issue_number: number }) => {
        refreshIssues(selectedWorkspace ?? "");
        if (data) clearPlanReady(data.workspace_id, data.issue_number);
      },
    );
    const offPlanRejected = EventsOn(
      "bus.plan_rejected",
      (data: { workspace_id: string; issue_number: number }) => {
        if (data) clearPlanReady(data.workspace_id, data.issue_number);
      },
    );
    const offGhPoll = EventsOn("bus.github_poll_done", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhIssue = EventsOn("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhClosed = EventsOn("bus.issue_closed", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhLabel = EventsOn("bus.issue_label_changed", () => refreshIssues(selectedWorkspace ?? ""));
    const offLabelsUpdated = EventsOn(
      "bus.labels_updated",
      (data: { workspace_id: string }) => {
        if (data?.workspace_id) {
          useLabelsStore.getState().refresh(data.workspace_id);
        }
      },
    );
    usePoolsStore.getState().refresh();
    const offPoolChanged = EventsOn("bus.agent_count_changed", () => {
      usePoolsStore.getState().refresh();
    });
    GetAutoPullPaused().then(setAutoPaused).catch(() => {});
    const offPause = EventsOn(
      "bus.auto_pull_paused_changed",
      (data: { paused: boolean }) => setAutoPaused(!!data?.paused),
    );
    const offArchived = EventsOn("bus.issues_archived", () => {
      refreshIssues(selectedWorkspace ?? "");
      refreshArchived(selectedWorkspace ?? "");
      refreshArchivedCount();
    });
    const pushToast = useToastStore.getState().push;
    const offToast = EventsOn("toast", (t: ToastT) => pushToast(t));
    return () => {
      if (typeof offLine === "function") offLine();
      if (typeof offState === "function") offState();
      if (typeof offActivity === "function") offActivity();
      if (typeof offGoal === "function") offGoal();
      if (typeof offGoalUpd === "function") offGoalUpd();
      if (typeof offIssue === "function") offIssue();
      if (typeof offPendingEnq === "function") offPendingEnq();
      if (typeof offPendingDeq === "function") offPendingDeq();
      if (typeof offPlanReady === "function") offPlanReady();
      if (typeof offPROpened === "function") offPROpened();
      if (typeof offPRMerged === "function") offPRMerged();
      if (typeof offPRClosedUnmerged === "function") offPRClosedUnmerged();
      if (typeof offPlanApproved === "function") offPlanApproved();
      if (typeof offPlanRejected === "function") offPlanRejected();
      if (typeof offPoolChanged === "function") offPoolChanged();
      if (typeof offGhPoll === "function") offGhPoll();
      if (typeof offGhIssue === "function") offGhIssue();
      if (typeof offGhClosed === "function") offGhClosed();
      if (typeof offGhLabel === "function") offGhLabel();
      if (typeof offLabelsUpdated === "function") offLabelsUpdated();
      if (typeof offPause === "function") offPause();
      if (typeof offArchived === "function") offArchived();
      if (typeof offToast === "function") offToast();
    };
  }, [appendLine, setMeta, refreshWorkspaces, refreshGoals, refreshIssues, markPlanReady, clearPlanReady, selectedWorkspace, refreshArchived, refreshArchivedCount]);

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
        <TitleSearch />
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
            onClick={async () => {
              const next = !autoPaused;
              setAutoPaused(next);
              try {
                await SetAutoPullPaused(next);
              } catch (e: any) {
                setAutoPaused(!next);
                alert(String(e?.message ?? e));
              }
            }}
            className={
              autoPaused
                ? "text-xs border border-amber-700 bg-amber-950/40 hover:bg-amber-900/60 px-2 py-1 rounded text-amber-300"
                : "text-xs border border-slate-700 hover:bg-slate-800 px-2 py-1 rounded text-slate-300"
            }
            title={
              autoPaused
                ? "Auto-pull paused — manual drag-to-PLAN still works; goal-scoped TODO filter unaffected. Click to resume."
                : "Pause orchestrator's auto-pull. Manual drag-to-PLAN still works; goal-scoped TODO filter unaffected."
            }
          >
            {autoPaused ? "⏸ auto-pull" : "▶ auto-pull"}
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
        </div>
      </header>
      <WorkspaceSwitcher
        archivedCount={archivedCount}
        onOpenArchived={() => setArchivedOpen(true)}
      />
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
      <PoolsHeader
        onOpenPools={() => {
          setSettingsInitialTab("pools");
          setSettingsOpen(true);
        }}
      />

      <SessionDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
      <Settings
        open={settingsOpen}
        onClose={() => {
          setSettingsOpen(false);
          setSettingsInitialTab("workspaces");
        }}
        initialTab={settingsInitialTab}
      />
      <PlanModal
        open={planTarget !== null}
        onClose={() => {
          setPlanQueue((q) => {
            if (q.length === 0) {
              setPlanTarget(null);
              return q;
            }
            const [next, ...rest] = q;
            setPlanTarget(next);
            return rest;
          });
        }}
        issue={planIssue}
        queueDepth={planQueue.length}
        onPopNext={() => {
          setPlanQueue((q) => {
            if (q.length === 0) return q;
            const [next, ...rest] = q;
            setPlanTarget(next);
            return rest;
          });
        }}
      />
      <Toast
        onOpenCard={(wsID, num) =>
          setPlanTarget({ workspace_id: wsID, number: num })
        }
      />
      <ArchivedDrawer
        open={archivedOpen}
        onClose={() => setArchivedOpen(false)}
        workspaceID={selectedWorkspace ?? ""}
      />
    </div>
  );
}

export default App;
