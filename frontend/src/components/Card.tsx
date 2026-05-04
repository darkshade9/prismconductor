import { useEffect, useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { Replan, ReplanForce, ClearIssueFailure, SelfHeal, CancelSession, SpawnPlanForIssue, ResolveConflicts } from "../../wailsjs/go/main/App";
import { RecoverButton } from "./RecoverButton";
import { types } from "../../wailsjs/go/models";
import { useSessionStore, SessionActivity } from "../stores/sessionStore";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { useIssueViewStore } from "../stores/useIssueViewStore";
import { resolveProviderIcon } from "../lib/providerIcon";
import { getContrastText } from "../lib/contrast";
import { LabelManagePopover } from "./LabelManagePopover";
import { MidRunQuestionModal } from "./MidRunQuestionModal";
import { ContinueModal } from "./ContinueModal";
import { AgentActivityStrip } from "./AgentActivityStrip";
import { DiagnosticPopover } from "./DiagnosticPopover";
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

  // Canonical IssueView from the backend assembler (#98). Provides
  // pre-derived session state, pool badge, and plan-ready flag without
  // requiring the card to reconcile multiple stores.
  const view = useIssueViewStore((s) => s.get(issue.workspace_id, issue.number));
  const activeSession: types.Session | null = view?.active_session ?? null;
  const pausedSession: types.Session | null = view?.paused_session ?? null;
  const lastFailure: types.Session | null = view?.last_failure ?? null;
  const testsFailingInfo = view?.tests_failing_info ?? null;
  const conflictsInfo = view?.conflicts_info ?? null;
  const orphanQuestion = view?.orphan_question ?? null;
  // Activity is live-streaming data not captured in the view — still from sessionStore.
  const activity: SessionActivity | null = useSessionStore((s) =>
    activeSession ? (s.sessions[activeSession.id]?.activity ?? null) : null
  );

  // Provider badge (#37): resolved by the assembler from the most-recent session's pool_id.
  const providerBadge = (() => {
    const badge = view?.pool_badge;
    if (!badge) return null;
    const icon = resolveProviderIcon(badge.provider);
    return { icon, tooltipName: badge.name, deleted: false };
  })();
  if (activeSession) {
    // Visible in DevTools (Cmd+Opt+I in the Wails window) — confirms the
    // session.state event reached the card render path.
    // eslint-disable-next-line no-console
    console.debug(
      `[card #${issue.number}] active session ${activeSession.id.slice(0, 8)} mode=${activeSession.mode} state=${activeSession.state}`,
    );
  }

  // Plan-ready state derived from the canonical view's LatestPlan.
  // Suppress on REVIEW/DONE: by the time work has shipped, an unapproved
  // newer plan revision is leftover noise (e.g. the user kicked a re-plan
  // before merging the original PR, then never approved it). Mirrors the
  // assembler's lastFail suppression for the same columns.
  const planReady = (() => {
    const p = view?.latest_plan;
    if (!p || !p.ready_to_execute || p.approved_at) return null;
    if (issue.column === "review" || issue.column === "done") return null;
    return { revision: p.revision };
  })();

  const blocked = (issue.dependencies ?? []).length > 0;
  const isPrimitive = !blocked && (issue.priority ?? 0) >= 0.7;

  const [midRunOpen, setMidRunOpen] = useState(false);
  const [continueOpen, setContinueOpen] = useState(false);
  const [cardMenu, setCardMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);
  const [diagAnchor, setDiagAnchor] = useState<{ x: number; y: number } | null>(null);

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
          : !activeSession && testsFailingInfo
          ? "border-red-500 card-glow-blocked"
          : !activeSession && conflictsInfo
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
          {/* Diagnostic info button (issue #100). */}
          <button
            onClick={(e) => {
              e.stopPropagation();
              setDiagAnchor({ x: e.clientX, y: e.clientY });
            }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
            className="text-slate-600 hover:text-slate-300 text-[11px] leading-none"
            title="Tell me about X? — show system diagnostic"
          >
            ⓘ
          </button>
          {/* Provider badge (issue #37): visible when any session has a known pool. */}
          {providerBadge && (
            <span
              className="text-slate-400 inline-flex items-center w-4 h-4 opacity-75"
              title={`${providerBadge.icon.label}${providerBadge.deleted ? " (deleted pool)" : `: ${providerBadge.tooltipName}`}`}
              aria-label={providerBadge.icon.label}
              dangerouslySetInnerHTML={{ __html: providerBadge.icon.svgContent }}
            />
          )}
          {/* PR chip stays visible across columns/session states (rev4 q3).
              Turns red when the PR has merge conflicts (#124). */}
          {issue.pr_number != null && issue.pr_url && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                BrowserOpenURL(issue.pr_url!);
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              onContextMenu={(e) => openCardMenu(e, [{ label: "Copy PR URL", text: issue.pr_url! }])}
              className={conflictsInfo
                ? "px-1.5 py-0.5 rounded text-[10px] font-medium bg-red-700/40 text-red-200 border border-red-700 hover:bg-red-700/60"
                : "px-1.5 py-0.5 rounded text-[10px] font-medium bg-emerald-700/40 text-emerald-200 border border-emerald-700 hover:bg-emerald-700/60"
              }
              title={conflictsInfo
                ? `PR #${issue.pr_number} — merge conflict with ${conflictsInfo.base}`
                : `Open ${issue.pr_url}`
              }
            >
              {conflictsInfo ? `⚡ PR #${issue.pr_number}` : `✓ PR #${issue.pr_number}`}
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
        orphanQuestion={orphanQuestion}
        planReady={planReady}
        blocked={blocked}
        isPrimitive={isPrimitive}
        dependencies={issue.dependencies ?? []}
        labels={issue.labels ?? []}
        workspaceID={issue.workspace_id}
        issueNumber={issue.number}
        waitingForPool={issue.waiting_for_pool ?? false}
        column={issue.column}
        prNumber={issue.pr_number ?? null}
        testsFailingInfo={testsFailingInfo}
        conflictsInfo={conflictsInfo}
        onContinue={() => setContinueOpen(true)}
        onClearFailure={() => ClearIssueFailure(issue.workspace_id, issue.number).catch((err: any) => alert(String(err?.message ?? err)))}
        onCancelSession={activeSession ? () => CancelSession(activeSession.id).catch((err: any) => alert(String(err?.message ?? err))) : null}
        onPlanNow={() => SpawnPlanForIssue(issue.workspace_id, issue.number).catch((err: any) => alert(String(err?.message ?? err)))}
      />
      {activeSession && activeSession.state === "running" && (
        <AgentActivityStrip
          sessionId={activeSession.id}
          sessionStartedAt={new Date(activeSession.started_at as any)}
        />
      )}
      <div className="flex justify-end items-center gap-2 mt-0.5">
        {(issue.cost_usd ?? 0) > 0 && (
          <CostChip
            costUSD={issue.cost_usd!}
            inputTokens={view?.last_session?.input_tokens ?? null}
            outputTokens={view?.last_session?.output_tokens ?? null}
          />
        )}
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
      {diagAnchor && (
        <DiagnosticPopover
          workspaceID={issue.workspace_id}
          issueNumber={issue.number}
          anchor={diagAnchor}
          onClose={() => setDiagAnchor(null)}
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

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function CostChip({
  costUSD,
  inputTokens,
  outputTokens,
}: {
  costUSD: number;
  inputTokens: number | null;
  outputTokens: number | null;
}) {
  const totalTokens = (inputTokens ?? 0) + (outputTokens ?? 0);
  const tokenSuffix = totalTokens > 0 ? ` · ${formatTokens(totalTokens)} tok` : "";
  let tooltip = `Cumulative LLM cost: $${costUSD.toFixed(4)}`;
  if (inputTokens != null || outputTokens != null) {
    const parts = [];
    if (inputTokens != null && inputTokens > 0) parts.push(`${formatTokens(inputTokens)} in`);
    if (outputTokens != null && outputTokens > 0) parts.push(`${formatTokens(outputTokens)} out`);
    if (parts.length > 0) tooltip += `\nlast session: ${parts.join(", ")}`;
  }
  return (
    <span
      className="text-[10px] text-slate-500 font-mono tabular-nums"
      title={tooltip}
    >
      ${costUSD < 0.01 ? costUSD.toFixed(4) : costUSD.toFixed(2)}{tokenSuffix}
    </span>
  );
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

type TestsFailingInfo = {
  failing_jobs: string[];
  failing_check_run_urls: string[];
  head_sha: string;
  self_heal_attempts?: number;
  attempt_cap?: number;
  max_attempts_reached?: boolean;
};

type ConflictsInfo = {
  pr_number: number;
  base: string;
  head: string;
  conflicting_files: string[];
};

type OrphanQuestionInfo = {
  pending_question_id: string;
  since: number;
};

function StatusRow({
  activeSession,
  activity,
  lastFailure,
  pausedSession,
  orphanQuestion,
  planReady,
  blocked,
  isPrimitive,
  dependencies,
  labels,
  workspaceID,
  issueNumber,
  waitingForPool,
  column,
  prNumber,
  testsFailingInfo,
  conflictsInfo,
  onContinue,
  onClearFailure,
  onCancelSession,
  onPlanNow,
}: {
  activeSession: types.Session | null;
  activity: SessionActivity | null;
  lastFailure: types.Session | null;
  pausedSession: types.Session | null;
  orphanQuestion: OrphanQuestionInfo | null;
  planReady: { revision: number } | null;
  blocked: boolean;
  isPrimitive: boolean;
  dependencies: number[];
  labels: string[];
  workspaceID: string;
  issueNumber: number;
  waitingForPool: boolean;
  column: string;
  prNumber: number | null;
  testsFailingInfo: TestsFailingInfo | null;
  conflictsInfo: ConflictsInfo | null;
  onContinue: () => void;
  onClearFailure: () => void;
  onCancelSession: (() => void) | null;
  onPlanNow: () => void;
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
    if (orphanQuestion) {
      return (
        <>
          <div className="text-[11px] mt-1.5 space-y-0.5">
            <div className="flex items-center gap-1.5 flex-wrap">
              <Pulse className="bg-red-400" />
              <span className="text-red-300 font-medium">BLOCKED — orphan question</span>
              <RecoverButton workspaceID={workspaceID} issueNumber={issueNumber} />
            </div>
            <div className="text-slate-500 pl-3.5">question file missing — session cannot resume</div>
          </div>
          {menuEl}
        </>
      );
    }
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
    const pipelineStepName = activeSession.pipeline_step_name;
    const label = isBlocked
      ? "blocked"
      : activeSession.state === "waiting_for_input"
      ? planMode
        ? "needs your answer"
        : "needs input"
      : planMode
      ? "planning"
      : pipelineStepName
      ? pipelineStepName
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
            {onCancelSession && (
              <button
                onClick={(e) => { e.stopPropagation(); onCancelSession(); }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-500 hover:border-red-700 hover:text-red-400"
                title="Cancel this session"
              >
                ✕ Cancel
              </button>
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
    const tooltip = "waiting for an available agent pool";
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
  const showConflicts =
    conflictsInfo && column === "review" && !activeSession && !pausedSession && !waitingForPool;
  if (showConflicts && conflictsInfo) {
    const fileCount = conflictsInfo.conflicting_files?.length ?? 0;
    return (
      <>
        <div className="text-[11px] mt-1.5 space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-red-400" />
            <span className="text-red-300 font-medium">MERGE CONFLICT</span>
            <span className="text-slate-500">— conflicts with {conflictsInfo.base}</span>
            <button
              onClick={(e) => {
                e.stopPropagation();
                ResolveConflicts(workspaceID, issueNumber).catch((err: any) =>
                  alert(String(err?.message ?? err))
                );
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-red-700 text-red-300 hover:border-red-500 hover:text-red-200"
              title="Auto-resolve: spawn a Continue-Work session pre-populated with conflict resolution context"
            >
              ✦ Resolve Conflicts
            </button>
          </div>
          {fileCount > 0 && (
            <div className="space-y-0.5 pl-3.5">
              {(conflictsInfo.conflicting_files ?? []).map((file, i) => (
                <span key={i} className="block text-red-400 truncate">⚡ {file}</span>
              ))}
            </div>
          )}
        </div>
        {menuEl}
      </>
    );
  }

  const showTestsFailing =
    testsFailingInfo && column === "review" && !activeSession && !pausedSession && !waitingForPool;
  if (showTestsFailing && testsFailingInfo) {
    const jobCount = testsFailingInfo.failing_jobs?.length ?? 0;
    const maxReached = testsFailingInfo.max_attempts_reached ?? false;
    return (
      <>
        <div className="text-[11px] mt-1.5 space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-red-400" />
            <span className="text-red-300 font-medium">TEST FAILURE</span>
            <span className="text-slate-500">— {jobCount} check{jobCount !== 1 ? "s" : ""} red</span>
            {maxReached ? (
              <span className="ml-auto text-[10px] text-amber-400 border border-amber-800 rounded px-1.5 py-0.5">
                max attempts reached
              </span>
            ) : (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  SelfHeal(workspaceID, issueNumber).catch((err: any) =>
                    alert(String(err?.message ?? err))
                  );
                }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-red-700 text-red-300 hover:border-red-500 hover:text-red-200"
                title="Auto-fix: spawn a Continue-Work session pre-populated with failing job details"
              >
                ✦ Self-heal
              </button>
            )}
          </div>
          {(testsFailingInfo.failing_jobs ?? []).length > 0 && (
            <div className="space-y-0.5 pl-3.5">
              {(testsFailingInfo.failing_jobs ?? []).map((job, i) => {
                const url = testsFailingInfo.failing_check_run_urls?.[i];
                return url ? (
                  <button
                    key={i}
                    onClick={(e) => {
                      e.stopPropagation();
                      BrowserOpenURL(url);
                    }}
                    onMouseDown={(e) => e.stopPropagation()}
                    onPointerDown={(e) => e.stopPropagation()}
                    className="block text-red-400 hover:text-red-200 truncate max-w-full text-left"
                    title={url}
                  >
                    ✗ {job}
                  </button>
                ) : (
                  <span key={i} className="block text-red-400 truncate">✗ {job}</span>
                );
              })}
            </div>
          )}
          {!maxReached && (testsFailingInfo.self_heal_attempts ?? 0) > 0 && (
            <div className="text-slate-500 pl-3.5">
              attempt {testsFailingInfo.self_heal_attempts}/{testsFailingInfo.attempt_cap ?? 3}
            </div>
          )}
        </div>
        {menuEl}
      </>
    );
  }
  if (lastFailure) {
    const reason = lastFailure.blocked_reason || "session ended without success";
    const showClear = column === "todo" || column === "plan" || column === "in_progress";
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
            {showClear && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onClearFailure();
                }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-red-800 text-red-400 hover:border-red-600 hover:text-red-300"
                title="Clear failure — marks the most recent blocked session as acknowledged and clears the card's blocked overlay."
              >
                ✕ Clear failure
              </button>
            )}
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
              Replan(workspaceID, issueNumber).catch((err: any) => {
                const msg = String(err?.message ?? err);
                if (msg.includes("already in flight")) {
                  if (confirm(`A session for #${issueNumber} is currently in progress. Cancel it and start a fresh plan?`)) {
                    ReplanForce(workspaceID, issueNumber).catch((e2: any) => alert(String(e2?.message ?? e2)));
                  }
                } else {
                  alert(msg);
                }
              });
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
  const showPlanNow = !blocked && (column === "todo" || column === "plan");
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
        {showPlanNow && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onPlanNow();
            }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
            className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-sky-700 text-sky-400 hover:border-sky-500 hover:text-sky-300"
            title="Start a planning pass for this issue"
          >
            ⚡ Plan now
          </button>
        )}
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
