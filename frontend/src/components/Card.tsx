import { useEffect, useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { Replan, ReplanForce, ClearIssueFailure, SelfHeal, CancelSession, SpawnPlanForIssue, ResolveConflicts, AttachManualPR, PrepareManualPushCommand, OpenOrphanPR, RetryExecuteForApprovedPlan, CancelRetries, RetryNow } from "../../wailsjs/go/main/App";
import { ClipboardSetText } from "../../wailsjs/runtime/runtime";
import { RecoverButton } from "./RecoverButton";
import { types, issueview } from "../../wailsjs/go/models";
import { useSessionStore, SessionActivity } from "../stores/sessionStore";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { useIssueViewStore } from "../stores/useIssueViewStore";
import { resolveProviderIcon } from "../lib/providerIcon";
import { getContrastText } from "../lib/contrast";
import { LabelManagePopover } from "./LabelManagePopover";
import { MidRunQuestionModal } from "./MidRunQuestionModal";
import { ContinueModal } from "./ContinueModal";
import { PRCommentReviewModal } from "./PRCommentReviewModal";
import { AgentActivityStrip } from "./AgentActivityStrip";
import { DiagnosticPopover } from "./DiagnosticPopover";
import { SessionLedger } from "./SessionLedger";
import { cn } from "../lib/cn";
import { CopyMenu, CopyAction, toQuotedBlock } from "./CopyMenu";
import { useGlowColorsStore, type GlowState } from "../stores/useGlowColorsStore";
import { resolveGlowClass, resolveCardBorderGlow } from "../lib/glow";

// CardStateValue mirrors the backend cardstate.CardState enum (#30).
// These values are emitted by the assembler and read additively here while
// the frontend falls back to its own derivation when the field is absent.
type CardStateValue =
  | "todo"
  | "plan_ready"
  | "planning"
  | "needs_answer"
  | "in_progress"
  | "blocked"
  | "waiting_for_pool"
  | "waiting_for_dep"
  | "needs_pr"
  | "orphan_question"
  | "merge_conflict"
  | "tests_failing"
  | "plan_failed"
  | "review"
  | "done"
  | "retry_scheduled";

type PendingRetryInfo = {
  attempt: number;
  max_attempts: number;
  not_before: number; // unix seconds
};

// Extended view type that includes the new card_state fields not yet in
// the auto-generated models.ts (wails generate module adds them on next build).
type IssueViewWithCardState = issueview.IssueView & {
  card_state?: CardStateValue;
  card_state_details?: {
    reason?: string;
    session_id?: string;
    session_mode?: string;
    blocked_reason?: string;
    retry_info?: PendingRetryInfo;
  };
};

export type CardProps = {
  issue: types.Issue;
  workspaceColor?: string;
  workspaceLabel?: string;
  relatedSiblings?: string[];
  onClick?: () => void;
};

export function Card({ issue, workspaceColor, workspaceLabel, relatedSiblings, onClick }: CardProps) {
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
  const view = useIssueViewStore((s) => s.get(issue.workspace_id, issue.number)) as IssueViewWithCardState | null;
  const activeSession: types.Session | null = view?.active_session ?? null;
  const pausedSession: types.Session | null = view?.paused_session ?? null;
  const lastFailure: types.Session | null = view?.last_failure ?? null;
  const testsFailingInfo = view?.tests_failing_info ?? null;
  const conflictsInfo = view?.conflicts_info ?? null;
  const orphanQuestion = view?.orphan_question ?? null;
  const needsPRInfo = view?.needs_pr_info ?? null;
  const planFailed = view?.plan_failed ?? null;
  const orphanPRInfo = view?.orphan_pr_info ?? null;
  const pendingRetryInfo: PendingRetryInfo | null = view?.card_state_details?.retry_info ?? null;
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

  // Issue #232: detect plans where the planner admitted it could not fetch
  // the GitHub issue body. Show a yellow warning banner on the card so the
  // user knows the plan may be off-topic or incomplete.
  const planHasFetchWarning = (() => {
    const p = view?.latest_plan;
    if (!p) return false;
    const combined = [p.plan_markdown ?? "", (p as any).goal_summary ?? "", (p as any).executive_summary ?? ""].join(" ").toLowerCase();
    return [
      "i couldn't fetch", "i could not fetch", "failed to retrieve the issue",
      "could not access the issue", "unable to fetch the issue", "i was unable to read",
      "i could not read the issue", "i couldn't read the issue", "failed to fetch the issue",
      "cannot fetch the issue", "unable to retrieve the issue", "couldn't access the issue",
    ].some(phrase => combined.includes(phrase));
  })();

  const waitingForDep = view?.waiting_for_dep ?? issue.waiting_for_dep ?? null;
  const blocked = (issue.dependencies ?? []).length > 0;
  const isPrimitive = !blocked && (issue.priority ?? 0) >= 0.7;

  // Determine which customizable glow state applies.
  // When card_state is present (backend-derived, #30), map it directly to a
  // GlowState; otherwise fall back to the legacy per-field derivation.
  const glowColors = useGlowColorsStore((s) => s.colors);
  const glowState = ((): GlowState | null => {
    const cs = view?.card_state;
    if (cs) {
      // Backend-authoritative path (additive — active when card_state present).
      switch (cs) {
        case "planning":        return "planning";
        case "in_progress":     return "inProgress";
        case "plan_ready":      return "planReady";
        case "orphan_question": return "planReady";
        case "needs_answer":    return "planReady";
        case "blocked":
        case "merge_conflict":
        case "tests_failing":
        case "plan_failed":     return "blocked";
        case "review":
        case "waiting_for_dep": return "review";
        // waiting_for_pool and needs_pr use hardcoded glows below.
        case "waiting_for_pool":
        case "needs_pr":
        case "todo":
        case "done":
        default:                return null;
      }
    }
    // Legacy derivation — kept as fallback until full Phase 2 cutover.
    if (pausedSession) return "planReady";
    if (activeSession?.state === "blocked") return "blocked";
    if (activeSession?.mode === "plan") return "planning";
    if (activeSession?.mode === "execute") return "inProgress";
    if (issue.waiting_for_pool) return null;
    if (!activeSession && needsPRInfo && !issue.pr_number) return null;
    if (!activeSession && (lastFailure || testsFailingInfo || conflictsInfo)) return "blocked";
    if (planReady) return "planReady";
    if (
      issue.column === "review" &&
      issue.pr_number != null &&
      !activeSession &&
      !pausedSession &&
      !testsFailingInfo &&
      !conflictsInfo
    ) return "review";
    return null;
  })();

  const customGlowClass = resolveGlowClass(glowState, glowState ? glowColors[glowState] : undefined);

  // Hardcoded glows for states not in the 6 customizable ones.
  const hardcodedGlowClass =
    !activeSession && !pausedSession && issue.waiting_for_pool
      ? "border-pink-500 card-glow-waiting"
      : !activeSession && !pausedSession && !issue.waiting_for_pool && needsPRInfo && !issue.pr_number
      ? "border-emerald-700 card-glow-needs-pr"
      : null;

  const cardBorderGlow = resolveCardBorderGlow(customGlowClass, hardcodedGlowClass);

  const [midRunOpen, setMidRunOpen] = useState(false);
  const [continueOpen, setContinueOpen] = useState(false);
  const [prCommentOpen, setPrCommentOpen] = useState(false);
  const [cardMenu, setCardMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);
  const [diagAnchor, setDiagAnchor] = useState<{ x: number; y: number } | null>(null);
  const [ledgerAnchor, setLedgerAnchor] = useState<{ x: number; y: number } | null>(null);

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
        "w-full text-left rounded-md border",
        "bg-white/90 dark:bg-slate-800/70 hover:bg-white dark:hover:bg-slate-800",
        "px-3 py-2 mb-2 shadow-sm transition-colors cursor-grab active:cursor-grabbing select-none",
        cardBorderGlow,
      )}
    >
      <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
        <span className="flex items-center gap-2 min-w-0">
          <span className="inline-block h-2 w-2 rounded-full shrink-0" style={{ backgroundColor: workspaceColor ?? "#64748b" }} />
          #{issue.number}
          <span className="text-slate-400 dark:text-slate-500 truncate">{workspaceLabel ?? issue.workspace_id}</span>
          {relatedSiblings && relatedSiblings.length > 0 && (
            <span
              className="text-[10px] text-slate-500 dark:text-slate-600 shrink-0"
              title={relatedSiblings.join(", ")}
            >
              +{relatedSiblings.length} related
            </span>
          )}
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
            className="text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300 text-[11px] leading-none"
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
          {/* New Comment badge (#159): orange, shown for REVIEW cards with unread PR comments. */}
          {issue.column === "review" && (view?.unread_comment_count ?? 0) > 0 && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                setPrCommentOpen(true);
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-orange-700/40 text-orange-200 border border-orange-600 hover:bg-orange-700/60 whitespace-nowrap"
              title="New PR comments — click to review"
            >
              New Comment ({view!.unread_comment_count})
            </button>
          )}
        </span>
      </div>
      <div
        className="text-sm text-slate-900 dark:text-slate-100 mt-1 line-clamp-2"
        onContextMenu={(e) => openCardMenu(e, [{ label: "Copy title", text: issue.title }])}
      >{issue.title}</div>

      {planHasFetchWarning && (
        <div className="mt-1 px-2 py-1 text-xs rounded bg-yellow-500/20 text-yellow-700 dark:text-yellow-300 border border-yellow-500/40">
          ⚠ Planner could not fetch the GitHub issue — plan may be incomplete
        </div>
      )}

      <StatusRow
        activeSession={activeSession}
        activity={activity}
        lastFailure={lastFailure}
        pausedSession={pausedSession}
        orphanQuestion={orphanQuestion}
        planReady={planReady}
        planFailed={planFailed}
        orphanPRInfo={orphanPRInfo}
        blocked={blocked}
        isPrimitive={isPrimitive}
        dependencies={issue.dependencies ?? []}
        crossDeps={issue.cross_deps ?? []}
        labels={issue.labels ?? []}
        workspaceID={issue.workspace_id}
        issueNumber={issue.number}
        waitingForPool={issue.waiting_for_pool ?? false}
        waitingForDep={waitingForDep}
        column={issue.column}
        prNumber={issue.pr_number ?? null}
        testsFailingInfo={testsFailingInfo}
        conflictsInfo={conflictsInfo}
        needsPRInfo={needsPRInfo}
        onContinue={() => setContinueOpen(true)}
        onClearFailure={() => ClearIssueFailure(issue.workspace_id, issue.number).catch((err: any) => alert(String(err?.message ?? err)))}
        onCancelSession={activeSession ? () => CancelSession(activeSession.id).catch((err: any) => alert(String(err?.message ?? err))) : null}
        onPlanNow={() => SpawnPlanForIssue(issue.workspace_id, issue.number).catch((err: any) => alert(String(err?.message ?? err)))}
        spawnFailureReason={issue.failure_reason ?? null}
        pendingRetryInfo={pendingRetryInfo}
      />
      {activeSession && activeSession.state === "running" && (
        <AgentActivityStrip
          sessionId={activeSession.id}
          sessionStartedAt={new Date(activeSession.started_at as any)}
        />
      )}
      <div className="flex justify-end items-center gap-2 mt-0.5">
        {(issue.cost_usd ?? 0) > 0 && (
          <button
            type="button"
            className="cursor-pointer focus:outline-none"
            onClick={(e) => {
              e.stopPropagation();
              setLedgerAnchor({ x: e.clientX, y: e.clientY });
            }}
            aria-label="View session ledger"
          >
            <CostChip
              costUSD={issue.cost_usd!}
              inputTokens={view?.last_session?.input_tokens ?? null}
              outputTokens={view?.last_session?.output_tokens ?? null}
            />
          </button>
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
      {issue.column === "review" && (
        <PRCommentReviewModal
          open={prCommentOpen}
          onClose={() => setPrCommentOpen(false)}
          workspaceID={issue.workspace_id}
          issueNumber={issue.number}
          autoContinue={true}
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
      {ledgerAnchor && (
        <SessionLedger
          workspaceID={issue.workspace_id}
          issueNumber={issue.number}
          anchor={ledgerAnchor}
          onClose={() => setLedgerAnchor(null)}
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
      className="text-[10px] text-slate-400 dark:text-slate-500 font-mono tabular-nums"
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
      className="text-[10px] text-slate-400 dark:text-slate-500 font-mono tabular-nums"
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

type NeedsPRInfo = {
  branch: string;
  worktree_dir: string;
  reason: string;
  kind: string;
  commit_msg_file?: string;
  commit_msg?: string;
};

type PlanFailedInfo = {
  session_id: string;
  reason?: string;
};

type OrphanPRInfo = {
  branch: string;
};

function StatusRow({
  activeSession,
  activity,
  lastFailure,
  pausedSession,
  orphanQuestion,
  planReady,
  planFailed,
  orphanPRInfo,
  blocked,
  isPrimitive,
  dependencies,
  crossDeps,
  labels,
  workspaceID,
  issueNumber,
  waitingForPool,
  waitingForDep,
  column,
  prNumber,
  testsFailingInfo,
  conflictsInfo,
  needsPRInfo,
  onContinue,
  onClearFailure,
  onCancelSession,
  onPlanNow,
  spawnFailureReason,
  pendingRetryInfo,
}: {
  activeSession: types.Session | null;
  activity: SessionActivity | null;
  lastFailure: types.Session | null;
  pausedSession: types.Session | null;
  orphanQuestion: OrphanQuestionInfo | null;
  planReady: { revision: number } | null;
  planFailed: PlanFailedInfo | null;
  orphanPRInfo: OrphanPRInfo | null;
  blocked: boolean;
  isPrimitive: boolean;
  dependencies: types.IssueDep[];
  crossDeps: types.IssueDep[];
  labels: string[];
  workspaceID: string;
  issueNumber: number;
  waitingForPool: boolean;
  waitingForDep: types.IssueDep | null;
  column: string;
  prNumber: number | null;
  testsFailingInfo: TestsFailingInfo | null;
  conflictsInfo: ConflictsInfo | null;
  needsPRInfo: NeedsPRInfo | null;
  onContinue: () => void;
  onClearFailure: () => void;
  onCancelSession: (() => void) | null;
  onPlanNow: () => void;
  spawnFailureReason: string | null;
  pendingRetryInfo: PendingRetryInfo | null;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);
  // Hooks for the conditional render branches below MUST be declared at the
  // component top (rules-of-hooks). Previously these lived inside
  // `if (showNeedsPR ...)` and `if (orphanPRInfo ...)` blocks, which produced
  // a different hook count on each render whenever the card transitioned
  // across those state boundaries — and React minified error #310 fired
  // ("rendered more/fewer hooks than during the previous render"). Card #204,
  // which has needs_pr_info populated from a NEEDS_PR sentinel, lived right
  // at one of those boundaries and reproducibly threw the error.
  const [attachOpen, setAttachOpen] = useState(false);
  const [prURLInput, setPRURLInput] = useState("");
  const [msgExpanded, setMsgExpanded] = useState(false);
  const [orphanPROpening, setOrphanPROpening] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [cancellingRetry, setCancellingRetry] = useState(false);
  const [secsLeft, setSecsLeft] = useState(() =>
    pendingRetryInfo
      ? Math.max(0, Math.ceil((pendingRetryInfo.not_before * 1000 - Date.now()) / 1000))
      : 0
  );
  useEffect(() => {
    if (!pendingRetryInfo) return;
    const tick = () =>
      setSecsLeft(Math.max(0, Math.ceil((pendingRetryInfo.not_before * 1000 - Date.now()) / 1000)));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [pendingRetryInfo?.not_before]);

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
  // NEEDS_PR badge: show when NeedsPRInfo is set and no PR has been attached yet.
  const showNeedsPR = needsPRInfo && !prNumber && !activeSession && !pausedSession && !waitingForPool;
  if (showNeedsPR && needsPRInfo) {
    const info = needsPRInfo; // capture for closures — TypeScript can't narrow through async boundaries
    const isCommitSigning = info.kind === "commit_signing";

    function copyPushCmd(e: React.MouseEvent) {
      e.stopPropagation();
      PrepareManualPushCommand(workspaceID, issueNumber)
        .then((cmd) => ClipboardSetText(cmd))
        .catch(() => {
          // Fall back to constructing the command locally if the binding fails.
          const fallback = isCommitSigning && info.commit_msg_file
            ? `cd "${info.worktree_dir}" && git add -A && git commit -S -F "${info.commit_msg_file}" && git push -u origin ${info.branch}`
            : isCommitSigning
              ? `cd "${info.worktree_dir}" && git add -A && git commit -S -m 'feat: implement changes' && git push -u origin ${info.branch}`
              : `cd "${info.worktree_dir}" && git push -u origin ${info.branch}`;
          ClipboardSetText(fallback);
        });
    }

    return (
      <>
        <div className="text-[11px] mt-1.5 space-y-1">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-emerald-600" />
            <span className="text-emerald-400 font-medium">push needed</span>
            <span className="text-slate-500">— commits in worktree</span>
          </div>
          <div className="text-slate-400 text-[10px] truncate pl-3.5" title={info.reason}>
            {info.reason || "could not push"}
          </div>
          {info.commit_msg && (
            <div className="pl-3.5">
              <button
                onClick={(e) => { e.stopPropagation(); setMsgExpanded(!msgExpanded); }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="text-[10px] text-slate-500 hover:text-slate-300"
              >
                {msgExpanded ? "▾ hide commit message" : "▸ show commit message"}
              </button>
              {msgExpanded && (
                <pre className="mt-1 text-[10px] text-slate-400 bg-slate-900 rounded p-1.5 whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
                  {info.commit_msg}
                </pre>
              )}
            </div>
          )}
          <div className="flex gap-1.5 flex-wrap pl-3.5">
            <button
              onClick={copyPushCmd}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] border border-emerald-800 text-emerald-400 hover:border-emerald-600 hover:text-emerald-300"
            >
              ⎘ Copy {isCommitSigning ? "commit+push" : "push"} command
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); BrowserOpenURL("file://" + info.worktree_dir); }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-200"
              title={info.worktree_dir}
            >
              ⌂ Open folder
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); setAttachOpen(!attachOpen); }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] border border-purple-800 text-purple-400 hover:border-purple-600 hover:text-purple-300"
            >
              ↗ I opened the PR
            </button>
          </div>
          {attachOpen && (
            <div className="pl-3.5 flex gap-1.5" onClick={(e) => e.stopPropagation()}>
              <input
                className="flex-1 bg-slate-900 border border-slate-700 rounded px-1.5 py-0.5 text-[10px] text-slate-200 placeholder-slate-600"
                placeholder="https://github.com/…/pull/123"
                value={prURLInput}
                onChange={(e) => setPRURLInput(e.target.value)}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
              />
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  AttachManualPR(workspaceID, issueNumber, prURLInput)
                    .then(() => { setAttachOpen(false); setPRURLInput(""); })
                    .catch((err: any) => alert(String(err?.message ?? err)));
                }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="px-1.5 py-0.5 rounded text-[10px] border border-purple-700 text-purple-300 hover:border-purple-500"
              >
                Attach
              </button>
            </div>
          )}
        </div>
        {menuEl}
      </>
    );
  }

  // Orphan PR recovery: execute session pushed a branch but died before opening a PR (#194).
  if (orphanPRInfo && !activeSession && !pausedSession && !needsPRInfo) {
    const opening = orphanPROpening;
    const setOpening = setOrphanPROpening;
    return (
      <>
        <div className="text-[11px] mt-1.5 space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-red-400" />
            <span className="text-red-300 font-medium">BLOCKED — branch pushed, no PR</span>
          </div>
          <div className="text-slate-400 text-[10px] truncate pl-3.5" title={orphanPRInfo.branch}>
            {orphanPRInfo.branch}
          </div>
          <div className="flex gap-1.5 pl-3.5">
            <button
              onClick={(e) => {
                e.stopPropagation();
                setOpening(true);
                OpenOrphanPR(workspaceID, issueNumber)
                  .catch((err: any) => alert(String(err?.message ?? err)))
                  .finally(() => setOpening(false));
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              disabled={opening}
              className="px-1.5 py-0.5 rounded text-[10px] border border-emerald-700 text-emerald-300 hover:border-emerald-500 hover:text-emerald-200 disabled:opacity-50"
              title={`Open a draft PR for branch ${orphanPRInfo.branch}`}
            >
              {opening ? "Opening…" : "↗ Open PR for pushed branch"}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onClearFailure(); }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-200"
              title="Dismiss this recovery prompt"
            >
              ✕ Dismiss
            </button>
          </div>
        </div>
        {menuEl}
      </>
    );
  }

  const showConflicts =
    conflictsInfo && !activeSession && !pausedSession && !waitingForPool;
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
    testsFailingInfo && !activeSession && !pausedSession && !waitingForPool;
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
  // Pending auto-retry: transient failure, scheduler queued a re-spawn (#221).
  if (pendingRetryInfo && lastFailure) {
    const reason = lastFailure.blocked_reason || "session ended without success";
    const countdownLabel = secsLeft > 0 ? `in ${secsLeft}s` : "now";
    return (
      <>
        <div
          className="text-[11px] mt-1.5 space-y-0.5"
          onContextMenu={(e) => openMenu(e, [
            { label: "Copy reason", text: reason },
            { label: "Copy as quoted block", text: toQuotedBlock(reason) },
          ])}
        >
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-amber-400" />
            <span className="text-amber-300">retry {countdownLabel}</span>
            <span className="text-slate-500">· {pendingRetryInfo.attempt}/{pendingRetryInfo.max_attempts}</span>
            <button
              onClick={(e) => {
                e.stopPropagation();
                setRetrying(true);
                RetryNow(workspaceID, issueNumber)
                  .catch((err: any) => alert(String(err?.message ?? err)))
                  .finally(() => setRetrying(false));
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              disabled={retrying}
              className="px-1.5 py-0.5 rounded text-[10px] border border-emerald-700 text-emerald-300 hover:border-emerald-500 hover:text-emerald-200 disabled:opacity-50"
              title="Fire the retry immediately"
            >
              {retrying ? "…" : "↺ Now"}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                setCancellingRetry(true);
                CancelRetries(workspaceID, issueNumber)
                  .catch((err: any) => alert(String(err?.message ?? err)))
                  .finally(() => setCancellingRetry(false));
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              disabled={cancellingRetry}
              className="px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-200 disabled:opacity-50"
              title="Cancel the pending retry and leave the card in blocked state"
            >
              {cancellingRetry ? "…" : "✕ Cancel"}
            </button>
          </div>
          <div className="text-slate-400 break-words" title={reason}>⚠ {reason}</div>
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
  // Plan failure: plan session ended without producing a plan and without
  // emitting BLOCKED: (those are shown via lastFailure above). Show a red
  // badge with a tooltip linking to the session transcript (#191).
  if (planFailed && !activeSession && !pausedSession && !lastFailure) {
    const reason = planFailed.reason || "plan session ended without success";
    const showClear = column === "todo" || column === "plan" || column === "in_progress";
    return (
      <>
        <div
          className="text-[11px] mt-1.5 space-y-0.5"
          title={reason}
          onContextMenu={(e) => openMenu(e, [
            { label: "Copy reason", text: reason },
            { label: "Copy as quoted block", text: toQuotedBlock(reason) },
          ])}
        >
          <div className="flex items-center gap-1.5">
            <Pulse className="bg-red-400" />
            <span className="text-red-300">plan failed</span>
            <span className="text-slate-500 font-mono text-[10px]">·&nbsp;{planFailed.session_id.slice(0, 8)}</span>
            {showClear && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onClearFailure();
                }}
                onMouseDown={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
                className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-red-800 text-red-400 hover:border-red-600 hover:text-red-300"
                title="Clear failure and retry planning"
              >
                ✕ Clear
              </button>
            )}
          </div>
          <div className="text-slate-400 break-words">⚠ {reason}</div>
        </div>
        {menuEl}
      </>
    );
  }
  // Spawn failure: SpawnExecute failed after plan approval, card rolled back
  // to PLAN. Show failure_reason with a retry button (issue #218).
  if (spawnFailureReason && column === "plan" && !activeSession && !lastFailure && !pausedSession) {
    return (
      <>
        <div
          className="text-[11px] mt-1.5 space-y-0.5"
          onContextMenu={(e) => openMenu(e, [
            { label: "Copy reason", text: spawnFailureReason },
            { label: "Copy as quoted block", text: toQuotedBlock(spawnFailureReason) },
          ])}
        >
          <div className="flex items-center gap-1.5 flex-wrap">
            <Pulse className="bg-red-400" />
            <span className="text-red-300">execute failed to start</span>
            <button
              onClick={(e) => {
                e.stopPropagation();
                setRetrying(true);
                RetryExecuteForApprovedPlan(workspaceID, issueNumber)
                  .catch((err: any) => alert(String(err?.message ?? err)))
                  .finally(() => setRetrying(false));
              }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              disabled={retrying}
              className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-emerald-700 text-emerald-300 hover:border-emerald-500 hover:text-emerald-200 disabled:opacity-50"
              title={spawnFailureReason}
            >
              {retrying ? "Retrying…" : "▶ Retry execute"}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onClearFailure(); }}
              onMouseDown={(e) => e.stopPropagation()}
              onPointerDown={(e) => e.stopPropagation()}
              className="px-1.5 py-0.5 rounded text-[10px] border border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-200"
              title="Clear this error without retrying"
            >
              ✕
            </button>
          </div>
          <div className="text-slate-400 break-words pl-3.5" title={spawnFailureReason}>
            ⚠ {spawnFailureReason}
          </div>
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
  const showPlanNow = !blocked && crossDeps.length === 0 && (column === "todo" || column === "plan");
  return (
    <>
      <div className="text-[11px] text-slate-500 dark:text-slate-400 mt-1 flex items-center gap-1.5 flex-wrap">
        {isPrimitive && <span className="text-emerald-400">🔴 primitive</span>}
        {blocked && (
          <span className="text-amber-300">
            🚫 blocked by {dependencies.map((d) => d.workspace_id ? `${d.workspace_id}#${d.number}` : `#${d.number}`).join(", ")}
          </span>
        )}
        {waitingForDep && (
          <span
            className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-700/30 text-amber-200 border border-amber-700"
            title={`Waiting for ${waitingForDep.workspace_id || "this workspace"}#${waitingForDep.number} to close before merging`}
          >
            ⏳ waiting for {waitingForDep.workspace_id ? `${waitingForDep.workspace_id}#${waitingForDep.number}` : `#${waitingForDep.number}`}
          </span>
        )}
        {!blocked && crossDeps.length > 0 && (
          <span
            className="text-indigo-300"
            title={crossDeps.map((d) => `${d.workspace_id}#${d.number}`).join(", ")}
          >
            ⏳ waiting on {crossDeps.map((d) => `${d.workspace_id}#${d.number}`).join(", ")}
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
