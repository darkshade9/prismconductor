import { useCallback, useEffect, useMemo, useState } from "react";
import { EventsOnWrapped } from "./lib/eventBus";
import {
  GetAutoPullPaused,
  ListArchivedIssues,
  ListPendingPlans,
  ListSessions,
  RefreshIssuesNow,
  SetAutoPullPaused,
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
import { LabelFilterStrip } from "./components/LabelFilterStrip";
import { useArchivedStore, useIssueStore } from "./stores/issueStore";
import { useGoalStore } from "./stores/goalStore";
import { usePlanReadyStore } from "./stores/planReadyStore";
import { useLabelsStore } from "./stores/labelsStore";
import { usePoolsStore } from "./stores/usePoolsStore";
import { Toast } from "./components/Toast";
import { useToastStore, type Toast as ToastT } from "./stores/toastStore";
import { TitleSearch } from "./components/Header";
import { PoolsHeader } from "./components/PoolsHeader";
import { AgentTerminalDrawer } from "./components/AgentTerminalDrawer";
import { useAgentTerminalStore } from "./stores/useAgentTerminalStore";
import { useDiagnosticUnlock } from "./hooks/useDiagnosticUnlock";

type PlanRef = { workspace_id: string; number: number };
type PlanTarget = PlanRef | null;

function App() {
  useDiagnosticUnlock();
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
  const [settingsInitialTab, setSettingsInitialTab] = useState<"workspaces" | "pools" | "skills" | "notify" | "logs">("workspaces");
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
  const toggleAgentDrawer = useAgentTerminalStore((s) => s.toggleDrawer);
  const agentDrawerOpen = useAgentTerminalStore((s) => s.drawerOpen);
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
    const offLine = EventsOnWrapped("session.line", (data: { session_id: string; line: string }) => {
      appendLine(data.session_id, data.line);
    });
    const offState = EventsOnWrapped("session.state", (sess: types.Session) => {
      // eslint-disable-next-line no-console
      console.debug(
        `[bus] session.state id=${sess.id?.slice(0, 8)} ws=${sess.workspace_id} #${sess.issue_number} mode=${sess.mode} state=${sess.state}`,
      );
      setMeta(sess);
    });
    const offActivity = EventsOnWrapped("session.activity", (act: SessionActivity) => {
      useSessionStore.getState().setActivity(act);
    });
    const offGoal = EventsOnWrapped("bus.goal_activated", () => {
      refreshGoals();
      refreshIssues(selectedWorkspace ?? "");
    });
    const offGoalUpd = EventsOnWrapped("bus.goal_updated", () => refreshGoals());
    const offIssue = EventsOnWrapped("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    // pending_pool_* events flip Issue.WaitingForPool on the server side.
    // Without re-fetching here, the card stays in the pink "waiting for
    // available agent pool" state visually even after the orchestrator has
    // drained the queue, spawned the worker, and moved the card to
    // IN_PROGRESS — the pool counter will (correctly) show the slot taken
    // while the card display is stale.
    const offPendingEnq = EventsOnWrapped(
      "bus.pending_pool_enqueued",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    const offPendingDeq = EventsOnWrapped(
      "bus.pending_pool_dequeued",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    // Don't hijack an open PlanModal when a different card's plan arrives.
    // Card glow + toast carry the signal; queue the arrival and drain on close.
    // Any future auto-open path (bus.plan_revised, etc.) must use the same
    // functional-setPlanTarget guard, never an unconditional set.
    const offPlanReady = EventsOnWrapped(
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
    const offPROpened = EventsOnWrapped(
      "bus.pr_opened",
      (_data: { workspace_id: string; issue_number: number; pr_number: number; pr_url: string }) => {
        refreshIssues(selectedWorkspace ?? "");
      },
    );
    const offPRMerged = EventsOnWrapped("bus.pr_merged", () => refreshIssues(selectedWorkspace ?? ""));
    const offPRClosedUnmerged = EventsOnWrapped(
      "bus.pr_closed_unmerged",
      () => refreshIssues(selectedWorkspace ?? ""),
    );
    const offPlanApproved = EventsOnWrapped(
      "bus.plan_approved",
      (data: { workspace_id: string; issue_number: number }) => {
        refreshIssues(selectedWorkspace ?? "");
        if (data) clearPlanReady(data.workspace_id, data.issue_number);
      },
    );
    const offPlanRejected = EventsOnWrapped(
      "bus.plan_rejected",
      (data: { workspace_id: string; issue_number: number }) => {
        if (data) clearPlanReady(data.workspace_id, data.issue_number);
      },
    );
    const offGhPoll = EventsOnWrapped("bus.github_poll_done", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhIssue = EventsOnWrapped("bus.issue_added", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhClosed = EventsOnWrapped("bus.issue_closed", () => refreshIssues(selectedWorkspace ?? ""));
    const offGhLabel = EventsOnWrapped("bus.issue_label_changed", () => refreshIssues(selectedWorkspace ?? ""));
    const offLabelsUpdated = EventsOnWrapped(
      "bus.labels_updated",
      (data: { workspace_id: string }) => {
        if (data?.workspace_id) {
          useLabelsStore.getState().refresh(data.workspace_id);
        }
      },
    );
    usePoolsStore.getState().refresh();
    const offPoolChanged = EventsOnWrapped("bus.agent_count_changed", () => {
      usePoolsStore.getState().refresh();
    });
    GetAutoPullPaused().then(setAutoPaused).catch(() => {});
    const offPause = EventsOnWrapped(
      "bus.auto_pull_paused_changed",
      (data: { paused: boolean }) => setAutoPaused(!!data?.paused),
    );
    const offArchived = EventsOnWrapped("bus.issues_archived", () => {
      refreshIssues(selectedWorkspace ?? "");
      refreshArchived(selectedWorkspace ?? "");
      refreshArchivedCount();
    });
    const pushToast = useToastStore.getState().push;
    const dismissToast = useToastStore.getState().dismiss;
    const offToast = EventsOnWrapped("toast", (t: ToastT) => pushToast(t));

    const offWsAdded = EventsOnWrapped(
      "bus.workspace_added",
      async (data: { workspace_id: string; workspace_name: string }) => {
        await refreshWorkspaces();
        useWorkspaceStore.getState().setSelected(data.workspace_id);
        pushToast({
          id: `ws-fetch-${data.workspace_id}`,
          level: "info",
          title: data.workspace_name || data.workspace_id,
          body: "Fetching issues…",
          workspace_id: data.workspace_id,
          issue_number: 0,
          action: "none",
          persist: true,
        });
      },
    );

    const offWsFetchDone = EventsOnWrapped(
      "bus.workspace_fetch_complete",
      (data: { workspace_id: string; workspace_name: string; success: boolean; issue_count: number; error?: string }) => {
        dismissToast(`ws-fetch-${data.workspace_id}`);
        refreshIssues(data.workspace_id);
        if (data.success) {
          pushToast({
            id: `ws-fetch-ok-${data.workspace_id}-${Date.now()}`,
            level: "success",
            title: data.workspace_name || data.workspace_id,
            body: `Fetched ${data.issue_count} issue${data.issue_count === 1 ? "" : "s"}`,
            workspace_id: data.workspace_id,
            issue_number: 0,
            action: "none",
          });
        } else {
          pushToast({
            id: `ws-fetch-err-${data.workspace_id}-${Date.now()}`,
            level: "error",
            title: data.workspace_name || data.workspace_id,
            body: data.error ?? "Failed to fetch issues",
            workspace_id: data.workspace_id,
            issue_number: 0,
            action: "none",
          });
        }
      },
    );

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
      if (typeof offWsAdded === "function") offWsAdded();
      if (typeof offWsFetchDone === "function") offWsFetchDone();
    };
  }, [appendLine, setMeta, refreshWorkspaces, refreshGoals, refreshIssues, markPlanReady, clearPlanReady, selectedWorkspace, refreshArchived, refreshArchivedCount]);

  return (
    <div className="h-screen flex flex-col bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-200">
      <header className="flex items-center justify-between px-4 py-2 border-b border-slate-200 dark:border-slate-800">
        <div className="font-semibold text-slate-900 dark:text-slate-200">PrismConductor</div>
        <TitleSearch />
        <div className="flex items-center gap-2">
          <button
            onClick={() => RefreshIssuesNow().catch((e) => alert(String(e?.message ?? e)))}
            className="text-xs border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 px-2 py-1 rounded"
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
            className="text-xs border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 px-2 py-1 rounded"
          >
            Drawer
          </button>
          <button
            onClick={toggleAgentDrawer}
            className={
              "text-xs border px-2 py-1 rounded " +
              (agentDrawerOpen
                ? "border-emerald-700 bg-emerald-950/40 text-emerald-300 hover:bg-emerald-900/50"
                : "border-slate-700 hover:bg-slate-800 text-slate-300")
            }
            title="Toggle agent terminal panel"
          >
            ⌨ Terminal
          </button>
          <button
            onClick={() => setSettingsOpen(true)}
            className="text-xs border border-slate-300 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 px-2 py-1 rounded"
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
      <LabelFilterStrip />
      <main className="flex-1 overflow-hidden pt-3 pb-7">
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
      <AgentTerminalDrawer />
    </div>
  );
}

export default App;
