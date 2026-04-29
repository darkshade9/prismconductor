import { useEffect, useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { Replan } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useSessionStore, SessionActivity } from "../stores/sessionStore";
import { usePlanReadyStore } from "../stores/planReadyStore";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { getContrastText } from "../lib/contrast";
import { LabelManagePopover } from "./LabelManagePopover";
import { cn } from "../lib/cn";

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
  const { activeSession, activity, lastFailure } = (() => {
    let active: types.Session | null = null;
    let activeAct: SessionActivity | null = null;
    let lastFail: types.Session | null = null;
    for (const view of Object.values(allSessions)) {
      const m = view.meta;
      if (!m) continue;
      if (m.workspace_id !== issue.workspace_id || m.issue_number !== issue.number) continue;
      if (m.state === "running" || m.state === "waiting_for_input" || m.state === "blocked") {
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
    return { activeSession: active, activity: activeAct, lastFailure: lastFail };
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

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={(e) => {
        if (isDragging) return;
        onClick?.();
        e.stopPropagation();
      }}
      className={cn(
        "w-full text-left rounded-md border bg-slate-800/70 hover:bg-slate-800 px-3 py-2 mb-2",
        "shadow-sm transition-colors cursor-grab active:cursor-grabbing select-none",
        // Blocked beats mode color — failure signal must be unmistakable.
        activeSession && activeSession.state === "blocked"
          ? "border-red-500 card-glow-blocked"
          : !activeSession && lastFailure
          ? "border-red-500 card-glow-blocked"
          : activeSession && activeSession.mode === "plan"
          ? "border-sky-500 card-glow-plan"
          : activeSession && activeSession.mode === "execute"
          ? "border-purple-500 card-glow-execute"
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
          {/* PR chip stays visible across columns/session states (rev4 q3). */}
          {issue.pr_number != null && issue.pr_url && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                BrowserOpenURL(issue.pr_url!);
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-emerald-700/40 text-emerald-200 border border-emerald-700 hover:bg-emerald-700/60"
              title={`Open ${issue.pr_url}`}
            >
              ✓ PR #{issue.pr_number}
            </button>
          )}
          {issue.priority ? <span className="text-slate-500">P{issue.priority.toFixed(2)}</span> : null}
        </span>
      </div>
      <div className="text-sm text-slate-100 mt-1 line-clamp-2">{issue.title}</div>

      <StatusRow
        activeSession={activeSession}
        activity={activity}
        lastFailure={lastFailure}
        planReady={planReady}
        blocked={blocked}
        isPrimitive={isPrimitive}
        dependencies={issue.dependencies ?? []}
        labels={issue.labels ?? []}
        workspaceID={issue.workspace_id}
        issueNumber={issue.number}
      />
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

function StatusRow({
  activeSession,
  activity,
  lastFailure,
  planReady,
  blocked,
  isPrimitive,
  dependencies,
  labels,
  workspaceID,
  issueNumber,
}: {
  activeSession: types.Session | null;
  activity: SessionActivity | null;
  lastFailure: types.Session | null;
  planReady: { revision: number } | null;
  blocked: boolean;
  isPrimitive: boolean;
  dependencies: number[];
  labels: string[];
  workspaceID: string;
  issueNumber: number;
}) {
  if (activeSession) {
    const planMode = activeSession.mode === "plan";
    const isBlocked = activeSession.state === "blocked";
    const label = isBlocked
      ? "blocked"
      : activeSession.state === "waiting_for_input"
      ? planMode
        ? "needs your answer"
        : "needs input"
      : planMode
      ? "planning"
      : "working";
    const dotCls = isBlocked ? "bg-red-400" : planMode ? "bg-sky-400" : "bg-purple-400";
    const textCls = isBlocked ? "text-red-300" : planMode ? "text-sky-300" : "text-purple-300";
    return (
      <div className="text-[11px] mt-1.5 space-y-0.5">
        <div className="flex items-center gap-1.5">
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
    );
  }
  if (lastFailure) {
    const reason = lastFailure.blocked_reason || "session ended without success";
    return (
      <div className="text-[11px] mt-1.5 space-y-0.5" title={reason}>
        <div className="flex items-center gap-1.5">
          <Pulse className="bg-red-400" />
          <span className="text-red-300">blocked</span>
          <span className="text-slate-500">· {lastFailure.mode}</span>
        </div>
        <div className="text-slate-400 break-words">⚠ {reason}</div>
      </div>
    );
  }
  if (planReady) {
    return (
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
    );
  }
  return (
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
    </div>
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
