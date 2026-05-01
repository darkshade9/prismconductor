import { useEffect, useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { Replan } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useSessionStore, SessionActivity } from "../stores/sessionStore";
import { usePlanReadyStore } from "../stores/planReadyStore";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { usePoolsStore } from "../stores/usePoolsStore";
import { resolveProviderIcon } from "../lib/providerIcon";
import { getContrastText } from "../lib/contrast";
import { LabelManagePopover } from "./LabelManagePopover";
import { MidRunQuestionModal } from "./MidRunQuestionModal";
import { ContinueModal } from "./ContinueModal";
import { cn } from "../lib/cn";
import { CopyMenu, CopyAction, toQuotedBlock } from "./CopyMenu";

export type CardProps = {
  issue: types.Issue;
  workspaceColor?: string;
  workspaceLabel?: string;
  onClick?: () => void;
};

export function Card({ issue, workspaceColor, workspaceLabel, onClick }: CardProps) {
  const id = `${issue.workspace_id}#${issue.number}`;
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id,
    data: { number: issue.number, workspaceID: issue.workspace_id, column: issue.column },
  });
  const style = {
    transform: CSS.Translate.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };

  // Live state: subscribe to the *whole* sessions table so we re-render when any
  // session changes.
  const allSessions = useSessionStore((s) => s.sessions);
  const pools = usePoolsStore((s) => s.pools);
  const { activeSession, activity, lastFailure, pausedSession, mostRecentSession } = (() => {
    let active: types.Session | null = null;
    let activeAct: SessionActivity | null = null;
    let lastFail: types.Session | null = null;
    let paused: types.Session | null = null;
    let mostRecent: types.Session | null = null;
    for (const view of Object.values(allSessions)) {
      const m = view.meta;
      if (!m) continue;
      if (m.workspace_id !== issue.workspace_id || m.issue_number !== issue.number) continue;
      // Track most-recent session by start time for provider badge (issue #37).
      if (!mostRecent || String(m.started_at ?? "") > String(mostRecent.started_at ?? "")) {
        mostRecent = m;
      }
      if (m.state === "paused_for_question") {
        // Paused for a mid-run question (#17). Track separately so it overrides
        // both running/active and last-failure rendering.
        if (!paused || String(m.started_at ?? "") > String(paused.started_at ?? "")) {
          paused = m;
        }
      } else if (m.state === "running" || m.state === "waiting_for_input" || m.state === "blocked") {
        if (!active) {
          active = m;
          activeAct = view.activity ?? null;
        }
      } else if ((m.state === "failed" || m.state === "blocked") && m.blocked_reason) {
        // Newest failed-with-reason session, regardless of column.
        if (!lastFail || String(m.started_at ?? "") > String(lastFail.started_at ?? "")) {
          lastFail = m;
        }
      }
    }
    // Suppress lastFailure when the card has reached REVIEW or DONE — the
    // PR is up, the work demonstrably succeeded, and any earlier
    // failed/blocked session in this issue's history is stale. Without
    // this, a false-positive BLOCKED match (or a real planner blow-up
    // that the user later worked around manually) keeps the red glow on
    // the card forever after the PR opens. Same logic for a successful
    // EXECUTE session that came AFTER the most recent failure: the post-
    // failure success retires the failure.
    if (lastFail && (issue.column === "review" || issue.column === "done")) {
      lastFail = null;
    } else if (lastFail && mostRecent && mostRecent.state === "completed" &&
        String(mostRecent.started_at ?? "") > String(lastFail.started_at ?? "")) {
      lastFail = null;
    }
    return { activeSession: active, activity: activeAct, lastFailure: lastFail, pausedSession: paused, mostRecentSession: mostRecent };
  })();

  // Provider badge (issue #37): resolve from most-recent session's pool_id.
  const providerBadge = (() => {
    const poolId = mostRecentSession?.pool_id;
    if (!poolId) return null;
    const pool = pools[poolId];
    if (!pool) {
      // Pool was deleted — show generic icon with deleted tooltip.
      const icon = resolveProviderIcon("generic");
      return { icon, tooltipName: "(deleted pool)", deleted: true };
    }
    const icon = resolveProviderIcon(pool.provider);
    return { icon, tooltipName: pool.name, deleted: false };
  })();
  if (activeSession) {
    // Visible in DevTools (Cmd+Opt+I in the Wails window) — confirms the
    // session.state event reached the card render path.
    // eslint-disable-next-line no-console
    console.debug(
      `[card #${issue.number}] active session ${activeSession.id.slice(0, 8)} mode=${activeSession.mode} state=${activeSession.state}`,
    );
  }

  const planReady = usePlanReadyStore((s) => s.isReady(issue.workspace_id, issue.number));

  const blocked = (issue.dependencies ?? []).length > 0;
  const isPrimitive = !blocked && (issue.priority ?? 0) >= 0.7;

  const [midRunOpen, setMidRunOpen] = useState(false);
  const [continueOpen, setContinueOpen] = useState(false);
  const [cardMenu, setCardMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);

  function openCardMenu(e: React.MouseEvent, actions: CopyAction[]) {
    e.preventDefault();
    e.stopPropagation();
    setCardMenu({ x: e.clientX, y: e.clientY, actions });
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={(e) => {
        if (isDragging) return;
        if (pausedSession) {
          setMidRunOpen(true);
          e.stopPropagation();
          return;
        }
        onClick?.();
        e.stopPropagation();
      }}
      className={cn(
        "w-full text-left rounded-md border bg-slate-800/70 hover:bg-slate-800 px-3 py-2 mb-2",
        "shadow-sm transition-colors cursor-grab active:cursor-grabbing select-none",
        // Paused-for-question (#17) beats mode color so a pending question is
        // always visually distinct from a running execute.
        // waiting_for_pool sits above lastFailure: a card actively queued for
        // a slot is a *current* state — the prior failure is history. Without
        // this ordering the red blocked-glow shadows the pink waiting-glow on
        // any card that previously failed and got requeued.
        pausedSession
          ? "border-amber-500 card-glow-ready"
          : activeSession && activeSession.state === "blocked"
          ? "border-red-500 card-glow-blocked"
          : activeSession && activeSession.mode === "plan"
          ? "border-sky-500 card-glow-plan"
          : activeSession && activeSession.mode === "execute"
          ? "border-purple-500 card-glow-execute"
          : issue.waiting_for_pool
          ? "border-pink-500 card-glow-waiting"
          : !activeSession && lastFailure
          ? "border-red-500 card-glow-blocked"
          : planReady
          ? "border-amber-500 card-glow-ready"
          : "border-slate-700",
      )}
    >
      <div className="flex items-center justify-between text-xs text-slate-400">
        <span className="flex items-center gap-2 min-w-0">
          <span className="inline-block h-2 w-2 rounded-full shrink-0" style={{ backgroundColor: workspaceColor ?? "#64748b" }} />
          #{issue.number}
          <span className="text-slate-500 truncate">{workspaceLabel ?? issue.workspace_id}</span>
        </span>
        <span className="flex items-center gap-1.5 shrink-0">
          {/* Provider badge (issue #37): visible when any session has a known pool. */}
          {providerBadge && (
            <span
              className="text-slate-400 inline-flex items-center w-4 h-4 opacity-75"
              title={`${providerBadge.icon.label}${providerBadge.deleted ? " (deleted pool)" : `: ${providerBadge.tooltipName}`}`}
              aria-label={providerBadge.icon.label}
              dangerouslySetInnerHTML={{ __html: providerBadge.icon.svgContent }}
            />
          )}
          {/* PR chip stays visible across columns/session states (rev4 q3). */}
          {issue.pr_number != null && issue.pr_url && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                BrowserOpenURL(issue.pr_url!);
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              onContextMenu={(e) => openCardMenu(e, [{ label: "Copy PR URL", text: issue.pr_url! }])}
              className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-emerald-700/40 text-emerald-200 border border-emerald-700 hover:bg-emerald-700/60"
              title={`Open ${issue.pr_url}`}
            >
              ✓ PR #{issue.pr_number}
            </button>
          )}
          {issue.priority ? <span className="text-slate-500">P{issue.priority.toFixed(2)}</span> : null}
        </span>
      </div>
      <div
        className="text-sm text-slate-100 mt-1 line-clamp-2"
        onContextMenu={(e) => openCardMenu(e, [{ label: "Copy title", text: issue.title }])}
      >{issue.title}</div>

      <StatusRow
        activeSession={activeSession}
        activity={activity}
        lastFailure={lastFailure}
        pausedSession={pausedSession}
        planReady={planReady}
        blocked={blocked}
        isPrimitive={isPrimitive}
        dependencies={issue.dependencies ?? []}
        labels={issue.labels ?? []}
        workspaceID={issue.workspace_id}
        issueNumber={issue.number}
        waitingForPool={issue.waiting_for_pool ?? false}
        pools={pools}
        column={issue.column}
        prNumber={issue.pr_number ?? null}
        onContinue={() => setContinueOpen(true)}
      />
      <div className="flex justify-end mt-0.5">
        <WorkTimerChip
          baseSecs={issue.work_seconds ?? 0}
          planBaseSecs={issue.work_seconds_plan ?? 0}
          executeBaseSecs={issue.work_seconds_execute ?? 0}
          activeSince={activeSession ? new Date(activeSession.started_at as any) : null}
          activeMode={activeSession ? (activeSession.mode as "plan" | "execute") : null}
        />
      </div>
      {pausedSession && pausedSession.pending_question_id && (
        <MidRunQuestionModal
          open={midRunOpen}
          onClose={() => setMidRunOpen(false)}
          workspaceID={issue.workspace_id}
          issueNumber={issue.number}
          questionID={pausedSession.pending_question_id}
        />
      )}
      {issue.pr_number != null && (
        <ContinueModal
          open={continueOpen}
          onClose={() => setContinueOpen(false)}
          workspaceID={issue.workspace_id}
          issueNumber={issue.number}
          prNumber={issue.pr_number}
        />
      )}
      {cardMenu && (
        <CopyMenu
          x={cardMenu.x}
          y={cardMenu.y}
          actions={cardMenu.actions}
          onClose={() => setCardMenu(null)}
        />
      )}
    </div>
  );
}

function useNow(intervalMs: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

function formatWorkDuration(s: number): string {
  if (s < 60) return `${s}s`;
  if (s < 3600) {
    const m = Math.floor(s / 60);
    const sec = s % 60;
    return `${m}m${sec.toString().padStart(2, "0")}s`;
  }
  if (s < 86400) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return `${h}h${m.toString().padStart(2, "0")}m`;
  }
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  return `${d}d${h.toString().padStart(2, "0")}h`;
}

function WorkTimerChip({
  baseSecs,
  planBaseSecs,
  executeBaseSecs,
  activeSince,
  activeMode,
}: {
  baseSecs: number;
  planBaseSecs: number;
  executeBaseSecs: number;
  activeSince: Date | null;
  activeMode: "plan" | "execute" | null;
}) {
  const now = useNow(activeSince ? 1000 : 60000);
  const liveDelta = activeSince ? Math.max(0, Math.floor((now - activeSince.getTime()) / 1000)) : 0;
  const total = baseSecs + liveDelta;
  if (total <= 0) return null;

  const planTotal = planBaseSecs + (activeMode === "plan" ? liveDelta : 0);
  const execTotal = executeBaseSecs + (activeMode === "execute" ? liveDelta : 0);
  const tooltip = [
    planTotal > 0 ? `plan: ${formatWorkDuration(planTotal)}` : null,
    execTotal > 0 ? `execute: ${formatWorkDuration(execTotal)}` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <span
      className="text-[10px] text-slate-500 font-mono tabular-nums"
      title={tooltip || undefined}
    >
      ⏱ {formatWorkDuration(total)}
    </span>
  );
}

function StatusRow({
  activeSession,
  activity,
  lastFailure,
  pausedSession,
  planReady,
  blocked,
  isPrimitive,
  dependencies,
  labels,
  workspaceID,
  issueNumber,
  waitingForPool,
  pools,
  column,
  prNumber,
  onContinue,
}: {
  activeSession: types.Session | null;
  activity: SessionActivity | null;
  lastFailure: types.Session | null;
  pausedSession: types.Session | null;
  planReady: { revision: number } | null;
  blocked: boolean;
  isPrimitive: boolean;
  dependencies: number[];
  labels: string[];
  workspaceID: string;
  issueNumber: number;
  waitingForPool: boolean;
  pools: Record<string, import("../stores/usePoolsStore").PoolEntry>;
  column: string;
  prNumber: number | null;
  onContinue: () => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);

  function openMenu(e: React.MouseEvent, actions: CopyAction[]) {
    e.preventDefault();
    e.stopPropagation();
    setMenu({ x: e.clientX, y: e.clientY, actions });
  }

  const menuEl = menu ? (
    <CopyMenu x={menu.x} y={menu.y} actions={menu.actions} onClose={() => setMenu(null)} />
  ) : null;

  if (pausedSession) {
    return (
      <>
        <div className="text-[11px] mt-1.5 flex items-center gap-1.5 flex-wrap">
          <Pulse className="bg-amber-400" />
          <span className="text-amber-300">❓ awaiting answer</span>
          <span className="text-slate-500">— click to answer</span>
        </div>
        {menuEl}
      </>
    );
  }
  if (activeSession) {
    const planMode = activeSession.mode === "plan";
    const isBlocked = activeSession.state === "blocked";
    const blockedReason = isBlocked
      ? (activeSession.blocked_reason || "BLOCKED: (no reason given)")
      : null;
    // Disambiguate fresh-execute from continue-on-open-PR. ContinueWork moves
    // the card to in_progress AND the issue already has a PR number — that
    // tuple is unique to a Continue run because the original execute moves
    // the card to review on PR_OPENED. Without this label users see
    // identical "working" copy on both and can't tell which mode is active.
    const isContinue =
      !planMode &&
      activeSession.state === "running" &&
      prNumber != null &&
      column === "in_progress";
    const label = isBlocked
      ? "blocked"
      : activeSession.state === "waiting_for_input"
      ? planMode
        ? "needs your answer"
        : "needs input"
      : planMode
      ? "planning"
      : isContinue
      ? `continuing PR #${prNumber}`
      : "working";
    const dotCls = isBlocked ? "bg-red-400" : planMode ? "bg-sky-400" : "bg-purple-400";
    const textCls = isBlocked ? "text-red-300" : planMode ? "text-sky-300" : "text-purple-300";
    return (
      <>
        <div className="text-[11px] mt-1.5 space-y-0.5">
          <div
            className="flex items-center gap-1.5"
            title={blockedReason ? truncateForTooltip(blockedReason) : undefined}
            onContextMenu={blockedReason ? (e) => openMenu(e, [
              { label: "Copy reason", text: blockedReason },
              { label: "Copy as quoted block", text: toQuotedBlock(blockedReason) },
            ]) : undefined}
          >
            <Pulse className={dotCls} />
            <span className={textCls}>{label}</span>
            {activity && activity.tool_count > 0 && (
              <span className="text-slate-500 ml-1">· {activity.tool_count} actions</span>
            )}
          </div>
          {activity && activity.last_action && (
            <ActivityHint activity={activity} />
          )}
        </div>
        {menuEl}
      </>
    );
  }
  if (!activeSession && waitingForPool) {
    // Build tooltip: "waiting for an available work pool — 0 of 7 slots free (Opus, Sonnet)"
    const poolNames = Object.values(pools).map((p) => p.name);
    const tooltip =
      poolNames.length > 0
        ? `waiting for an available agent pool — ${poolNames.join(", ")}`
        : "no agent pools configured — add one in Settings → Pools";
    return (
      <>
        <div className="text-[11px] mt-1.5 flex items-center gap-1.5 flex-wrap" title={tooltip}>
          <Pulse className="bg-pink-400" />
          <span className="text-pink-300">waiting for available agent pool…</span>
        </div>
        {menuEl}
      </>
    );
  }
  if (lastFailure) {
    const reason = lastFailure.blocked_reason || "session ended without success";
    return (
      <>
        <div
          className="text-[11px] mt-1.5 space-y-0.5"
          onContextMenu={(e) => openMenu(e, [
            { label: "Copy reason", text: reason },
            { label: "Copy as quoted block", text: toQuotedBlock(reason) },
          ])}
        >
          <div className="flex items-center gap-1.5">
            <Pulse className="bg-red-400" />
            <span className="text-red-300">blocked</span>
            <span className="text-slate-500">· {lastFailure.mode}</span>
          </div>
          <div className="text-slate-400 break-words" title={reason}>⚠ {reason}</div>
        </div>
        {menuEl}
      </>
    );
  }
  if (planReady) {
    return (
      <>
        <div className="text-[11px] mt-1.5 flex items-center gap-1.5 flex-wrap">
          <span className="text-amber-300">⏸ Plan ready (rev {planReady.revision})</span>
          <span className="text-slate-500">— click to review</span>
          <button
            onClick={(e) => {
              e.stopPropagation();
              Replan(workspaceID, issueNumber).catch((err: any) => alert(String(err?.message ?? err)));
            }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
            className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500"
            title="Discard the current plan and start a fresh planning pass"
          >
            ↻ Re-plan
          </button>
        </div>
        {menuEl}
      </>
    );
  }
  const showContinue = column === "review" && prNumber != null && !activeSession && !pausedSession && !waitingForPool;
  return (
    <>
      <div className="text-[11px] text-slate-400 mt-1 flex items-center gap-1.5 flex-wrap">
        {isPrimitive && <span className="text-emerald-400">🔴 primitive</span>}
        {blocked && (
          <span className="text-amber-300">
            🚫 blocked by {dependencies.map((n) => `#${n}`).join(", ")}
          </span>
        )}
        <LabelChips
          workspaceID={workspaceID}
          issueNumber={issueNumber}
          labels={labels}
        />
        {showContinue && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onContinue();
            }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
            className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-purple-700 text-purple-300 hover:border-purple-500 hover:text-purple-200"
            title="Continue work on this PR branch"
          >
            ↻ Continue
          </button>
        )}
      </div>
      {menuEl}
    </>
  );
}

function LabelChips({
  workspaceID,
  issueNumber,
  labels,
}: {
  workspaceID: string;
  issueNumber: number;
  labels: string[];
}) {
  const byName = useLabelsStore((s) => s.byName);
  const knownLabels = useLabelsStore((s) => s.byWorkspace[workspaceID] ?? EMPTY_LABELS);
  const refresh = useLabelsStore((s) => s.refresh);
  const [popoverAnchor, setPopoverAnchor] = useState<{ x: number; y: number } | null>(null);

  // Fetch on first card render so chips have colors. Cheap (cached).
  useEffect(() => {
    if (workspaceID && knownLabels.length === 0) {
      refresh(workspaceID);
    }
    // We deliberately depend on workspaceID + the cache emptiness — re-renders
    // shouldn't re-fetch on every label-bus tick.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceID]);

  return (
    <>
      {labels.map((name) => {
        const lab = byName(workspaceID, name);
        const color = lab?.color ?? "475569"; // slate-600
        const fg = getContrastText(color);
        return (
          <span
            key={name}
            className="px-1.5 py-0.5 rounded text-[10px] font-medium"
            style={{ backgroundColor: `#${color}`, color: fg }}
            title={lab?.description || name}
          >
            {name}
          </span>
        );
      })}
      <button
        onClick={(e) => {
          e.stopPropagation();
          const rect = (e.target as HTMLElement).getBoundingClientRect();
          setPopoverAnchor({ x: rect.left, y: rect.bottom });
        }}
        onMouseDown={(e) => e.stopPropagation()}
        onPointerDown={(e) => e.stopPropagation()}
        className="px-1.5 py-0.5 rounded text-[10px] border border-dashed border-slate-600 text-slate-400 hover:border-slate-400 hover:text-slate-200"
        title="Add label"
      >
        + Label
      </button>
      <LabelManagePopover
        open={popoverAnchor !== null}
        onClose={() => setPopoverAnchor(null)}
        workspaceID={workspaceID}
        issueNumber={issueNumber}
        currentLabels={labels}
        anchor={popoverAnchor}
      />
    </>
  );
}

function ActivityHint({ activity }: { activity: SessionActivity }) {
  // Re-render every second so the "Xs ago" stays current.
  const now = useNow(1000);
  const sinceMs = now - new Date(activity.last_action_at).getTime();
  const seconds = Math.max(0, Math.floor(sinceMs / 1000));
  const stale = seconds >= 30;
  return (
    <div className={cn("flex items-center gap-1 truncate", stale ? "text-amber-400" : "text-slate-500")}>
      <span className="font-mono">⚙</span>
      <span className="truncate">{activity.last_action}</span>
      <span className="shrink-0">· {formatElapsed(seconds)}</span>
    </div>
  );
}

function truncateForTooltip(s: string): string {
  const trimmed = s.trim();
  return trimmed.length > 120 ? trimmed.slice(0, 117) + "…" : trimmed;
}

function formatElapsed(s: number): string {
  if (s < 1) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  const r = s % 60;
  return r > 0 ? `${m}m${r}s ago` : `${m}m ago`;
}

function Pulse({ className }: { className: string }) {
  return (
    <span className="relative inline-flex h-2 w-2">
      <span className={cn("absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping", className)} />
      <span className={cn("relative inline-flex h-2 w-2 rounded-full", className)} />
    </span>
  );
}
