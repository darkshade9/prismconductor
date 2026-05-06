import { useCallback, useEffect, useMemo, useState } from "react";
import { noAutoCorrect } from "../lib/inputs";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ApprovePlan, LatestPlan, PlanCostEstimate, RejectPlan, SetIssueLabels, SubmitAnswers, WriteAnswersOnly } from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { useIssueStore } from "../stores/issueStore";
import { useIssueViewStore } from "../stores/useIssueViewStore";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { AnswerState, QuestionForm, emptyAnswers, initialAnswers, isComplete } from "./QuestionForm";
import { LabelsModal } from "./LabelsModal";
import { IssueSection } from "./IssueSection";
import { getContrastText } from "../lib/contrast";

const INTENT_GLYPH: Record<string, string> = {
  add: "+",
  modify: "~",
  delete: "-",
  "read-only": "·",
};

export function PlanModal({
  open,
  onClose,
  issue,
  queueDepth = 0,
  onPopNext,
}: {
  open: boolean;
  onClose: () => void;
  issue: types.Issue | null;
  queueDepth?: number;
  onPopNext?: () => void;
}) {
  const refreshIssues = useIssueStore((s) => s.refresh);
  const loadIssueViews = useIssueViewStore((s) => s.loadForWorkspace);
  // Always refresh issues at the SAME scope the Board is currently rendering
  // (the chip-selected workspace, or "" for All). Calling refreshIssues()
  // without an arg defaults to ListIssues("") which silently swaps the
  // global issueStore over to All — Board.tsx then renders every workspace's
  // cards while the chip still says prismconductor, exactly the
  // background-context-switch bug from the ApprovePlan path.
  const selectedWorkspace = useWorkspaceStore((s) => s.selectedID);
  const workspaces = useWorkspaceStore((s) => s.workspaces);
  const activeWorkspace = workspaces.find((w) => w.id === issue?.workspace_id);
  const refreshLabels = useLabelsStore((s) => s.refresh);
  const labelsCache = useLabelsStore((s) => (issue ? s.byWorkspace[issue.workspace_id] ?? EMPTY_LABELS : EMPTY_LABELS));
  const [plan, setPlan] = useState<types.Plan | null>(null);
  const [costEstimate, setCostEstimate] = useState<main.CostEstimate | null>(null);
  const [answers, setAnswers] = useState<AnswerState>(emptyAnswers());
  const [refineText, setRefineText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [labelsModal, setLabelsModal] = useState<{ open: boolean; suggestName?: string }>({
    open: false,
  });

  const handleClose = useCallback(() => {
    setRefineText("");
    setAnswers(emptyAnswers());
    onClose();
  }, [onClose]);

  useEffect(() => {
    if (!open || !issue) return;
    setLoading(true);
    setError(null);
    setAnswers(emptyAnswers());
    setRefineText("");
    setCostEstimate(null);
    LatestPlan(issue.workspace_id, issue.number)
      .then((p) => {
        setPlan(p ?? null);
        if (p?.questions?.length) {
          setAnswers(initialAnswers(p.questions));
        }
      })
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setLoading(false));
    PlanCostEstimate(issue.workspace_id, issue.number)
      .then((est) => setCostEstimate(est ?? null))
      .catch(() => { /* non-fatal — cost estimate is best-effort */ });
    refreshLabels(issue.workspace_id);
  }, [open, issue?.workspace_id, issue?.number, refreshLabels]);

  if (!open || !issue) return null;

  const ageMin = plan ? minutesAgo(plan.generated_at) : null;
  const questions = plan?.questions ?? [];
  const hasQuestions = questions.length > 0;
  const allRequiredAnswered = !hasQuestions || isComplete(questions, answers);

  async function submit() {
    if (!plan || !issue) return;
    setBusy(true);
    setError(null);
    try {
      // Bundle structured answers + the free-text refinement (if any) into a
      // single submission. The worker sees __refine as a top-level free-form
      // direction and rewrites the plan accordingly.
      const merged = { ...answers.single };
      if (refineText.trim()) merged.__refine = refineText.trim();
      await SubmitAnswers(
        new main.AnswerSubmission({
          workspace_id: issue.workspace_id,
          issue_number: issue.number,
          revision: plan.revision,
          answers: merged,
          multi: answers.multi,
        }),
      );
      setRefineText("");
      setAnswers(emptyAnswers());
      handleClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function approve() {
    if (!plan || !issue) return;
    setBusy(true);
    setError(null);
    try {
      // If the user filled in answers / refinement before clicking Approve,
      // persist them to the answers file so the execute skill picks them up
      // — but DO NOT re-spawn the planner (no extra $$ on a re-plan loop).
      // The bundled execute skill reads .prismconductor/answers/<n>-rev<N>.json
      // at the same revision; that's enough to carry the user's choices into
      // execution without burning another planner run.
      if (hasQuestions && allRequiredAnswered) {
        const merged = { ...answers.single };
        if (refineText.trim()) merged.__refine = refineText.trim();
        await WriteAnswersOnly(
          new main.AnswerSubmission({
            workspace_id: issue.workspace_id,
            issue_number: issue.number,
            revision: plan.revision,
            answers: merged,
            multi: answers.multi,
          }),
        );
      }
      await ApprovePlan(issue.workspace_id, issue.number, plan.revision);
      await Promise.all([refreshIssues(selectedWorkspace ?? ""), loadIssueViews(issue.workspace_id)]);
      handleClose();
    } catch {
      // The backend emits a toast and rolls the card back to PLAN on spawn
      // failure, so close the modal and let the toast be the primary signal.
      await Promise.all([
        refreshIssues(selectedWorkspace ?? ""),
        loadIssueViews(issue.workspace_id),
      ]).catch(() => {});
      handleClose();
    } finally {
      setBusy(false);
    }
  }

  async function reject() {
    if (!issue) return;
    setBusy(true);
    setError(null);
    try {
      await RejectPlan(issue.workspace_id, issue.number);
      await Promise.all([refreshIssues(selectedWorkspace ?? ""), loadIssueViews(issue.workspace_id)]);
      handleClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[700px] max-h-[85vh] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="flex items-center gap-2 min-w-0">
            <div className="text-slate-200 truncate">
              Plan: #{issue.number} — <span className="text-slate-400">{issue.title}</span>
            </div>
            {queueDepth > 0 && onPopNext && (
              <button
                type="button"
                onClick={onPopNext}
                className="shrink-0 text-xs px-2 py-0.5 rounded-full border border-amber-700 bg-amber-950/40 text-amber-200 hover:bg-amber-900/40"
                title="Open next plan that became ready while you were reviewing"
              >
                +{queueDepth} more plan{queueDepth === 1 ? "" : "s"} ready
              </button>
            )}
          </div>
          <button onClick={handleClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="px-4 py-3 text-xs text-slate-500 flex items-center gap-3 border-b border-slate-800">
          <span>{issue.workspace_id}</span>
          {plan && (
            <>
              <span>Revision {plan.revision}</span>
              <span>
                Complexity: {plan.estimated_complexity || "—"}
                {plan.estimated_complexity && (
                  <span className="text-slate-600 ml-1">
                    ({activeWorkspace?.complexity_scale === "fibonacci"
                      ? "Fibonacci"
                      : activeWorkspace?.complexity_scale === "linear10"
                      ? "Linear 1–10"
                      : "T-shirt"})
                  </span>
                )}
              </span>
              <span>{ageMin !== null ? `Generated ${ageMin}m ago` : ""}</span>
            </>
          )}
          {costEstimate && (
            <span
              className="ml-auto text-slate-400"
              title={
                costEstimate.has_rate
                  ? `Estimated execute cost (~${costEstimate.tokens.toLocaleString()} tokens × ${costEstimate.model || "unknown model"} rates). Actual cost recorded after the session ends.`
                  : `No rate configured for model "${costEstimate.model || "(unknown)"}". Add rates in Settings to see cost estimates.`
              }
            >
              {costEstimate.has_rate
                ? `~$${costEstimate.cost_usd.toFixed(4)} est.`
                : "cost: —"}
            </span>
          )}
        </div>

        <div className="flex-1 overflow-y-auto px-4 py-3 text-sm space-y-4">
          <IssueSection issue={issue} />

          <hr className="border-slate-700" />

          {loading && <div className="text-slate-500">loading…</div>}
          {!loading && !plan && (
            <div className="text-slate-500">
              No plan yet for this issue. Drag the card to the PLAN column or use the
              "Plan issue" header trigger to spawn a plan worker.
            </div>
          )}

          {plan && (
            <>
              <LabelsSection
                issue={issue}
                plan={plan}
                cache={labelsCache}
                onManage={(name) => setLabelsModal({ open: true, suggestName: name })}
                onError={setError}
                onIssueRefresh={refreshIssues}
              />

              <CollapsibleSection title="The Goal" defaultOpen={true}>
                {plan.goal_summary ? (
                  <Markdown text={plan.goal_summary} />
                ) : (
                  <>
                    <Markdown text={plan.plan_markdown} />
                    <p className="text-xs text-slate-500 mt-2">
                      Older plan format — showing implementation detail. Reject and replan to populate the goal summary.
                    </p>
                  </>
                )}
              </CollapsibleSection>

              <CollapsibleSection title="The Code" defaultOpen={false}>
                {(plan.files_to_modify ?? []).length > 0 && (
                  <ul className="font-mono text-xs space-y-0.5 mb-3">
                    {plan.files_to_modify.map((f) => (
                      <li key={f.path} className="text-slate-300">
                        <span className="text-slate-500 w-3 inline-block">{INTENT_GLYPH[f.intent] ?? "?"}</span>{" "}
                        {f.path}{" "}
                        <span className="text-slate-600">({f.intent})</span>
                      </li>
                    ))}
                  </ul>
                )}
                {plan.goal_summary && <Markdown text={plan.plan_markdown} />}
                {(plan.dependencies_detected ?? []).length > 0 && (
                  <ul className="text-xs space-y-0.5 mt-3">
                    {plan.dependencies_detected.map((n) => (
                      <li key={n} className="text-amber-300">• Depends on #{n}</li>
                    ))}
                  </ul>
                )}
              </CollapsibleSection>

              <CollapsibleSection title="Executive Summary" defaultOpen={true}>
                {plan.executive_summary ? (
                  <Markdown text={plan.executive_summary} />
                ) : (
                  <p className="text-slate-500 text-xs italic">Planner didn't produce one — replan to fill in.</p>
                )}
              </CollapsibleSection>

              {hasQuestions && (
                <Section title={`Questions (${questions.length})`}>
                  <QuestionForm questions={questions} state={answers} onChange={setAnswers} />
                </Section>
              )}

              <Section title="Refine plan (free-form)">
                <textarea
                  {...noAutoCorrect}
                  value={refineText}
                  onChange={(e) => setRefineText(e.target.value)}
                  rows={3}
                  placeholder="e.g. focus on the Settings tab first; skip the conventions sniffer; use my existing useGoalStore pattern"
                  className="w-full bg-slate-800 border border-slate-700 rounded p-2 text-slate-200 text-sm"
                />
                <div className="text-xs text-slate-500 mt-1">
                  Anything you write here goes back to the worker as direction; it'll regenerate the plan
                  with this in mind. Submit alongside any answers above.
                </div>
              </Section>
            </>
          )}

          {error && <div className="text-red-400 text-xs">{error}</div>}
        </div>
        {issue && (
          <LabelsModal
            open={labelsModal.open}
            onClose={() => setLabelsModal({ open: false })}
            workspaceID={issue.workspace_id}
            initialNewName={labelsModal.suggestName}
          />
        )}

        {plan && (
          <div className="px-4 py-2 border-t border-slate-800 flex justify-end gap-2">
            <button onClick={reject} disabled={busy} className="px-3 py-1 text-red-400 hover:text-red-300 disabled:opacity-50">
              Reject
            </button>
            <button
              onClick={submit}
              disabled={busy || (hasQuestions && !allRequiredAnswered) || (!hasQuestions && !refineText.trim())}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50"
              title={
                hasQuestions && !allRequiredAnswered
                  ? "Answer all required questions first"
                  : !hasQuestions && !refineText.trim()
                  ? "Type a refinement to request a revision"
                  : "Spawn another planner run (costs another paid plan call)"
              }
            >
              {hasQuestions ? "Re-plan with these answers" : "Request revision"}
            </button>
            <button
              onClick={approve}
              disabled={busy || (hasQuestions && !allRequiredAnswered)}
              className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
              title={
                hasQuestions && !allRequiredAnswered
                  ? "Answer all required questions first"
                  : "Approve and execute with these answers — skips another paid planner run"
              }
            >
              {busy ? "…" : "Approve & execute"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function LabelsSection({
  issue,
  plan,
  cache,
  onManage,
  onError,
  onIssueRefresh,
}: {
  issue: types.Issue;
  plan: types.Plan;
  cache: types.Label[];
  onManage: (name?: string) => void;
  onError: (msg: string | null) => void;
  onIssueRefresh: () => Promise<void> | void;
}) {
  const cacheByName = useMemo(() => {
    const m = new Map<string, types.Label>();
    for (const l of cache) m.set(l.name, l);
    return m;
  }, [cache]);

  const current = issue.labels ?? [];
  // Issue #24: keep ALL planner suggestions rendered. Applied ones get an
  // (auto-applied) badge; unapplied ones keep the dashed Apply chip.
  const suggestions = plan.suggested_labels ?? [];

  async function removeApplied(name: string) {
    onError(null);
    const next = current.filter((n) => n !== name);
    try {
      await SetIssueLabels(issue.workspace_id, issue.number, next);
      await onIssueRefresh();
    } catch (e: any) {
      onError(String(e?.message ?? e));
    }
  }

  async function applySuggestion(name: string) {
    const exists = cacheByName.has(name);
    if (!exists) {
      // Defer to user — open the LabelsModal pre-filled with the name so they
      // can review the color/description before creating it on GitHub.
      onManage(name);
      return;
    }
    onError(null);
    const next = Array.from(new Set([...current, name]));
    try {
      await SetIssueLabels(issue.workspace_id, issue.number, next);
      await onIssueRefresh();
    } catch (e: any) {
      onError(String(e?.message ?? e));
    }
  }

  return (
    <Section title="Labels">
      <div className="flex flex-wrap items-center gap-1.5">
        {current.length === 0 && (
          <span className="text-xs text-slate-500">no labels yet</span>
        )}
        {current.map((name) => {
          const lab = cacheByName.get(name);
          const color = lab?.color ?? "475569";
          const fg = getContrastText(color);
          return (
            <span
              key={name}
              className="px-1.5 py-0.5 rounded text-[11px] font-medium"
              style={{ backgroundColor: `#${color}`, color: fg }}
              title={lab?.description || name}
            >
              {name}
            </span>
          );
        })}
        <button
          onClick={() => onManage()}
          className="px-2 py-0.5 rounded text-[11px] border border-slate-700 text-slate-400 hover:text-slate-200"
        >
          Manage…
        </button>
      </div>

      {suggestions.length > 0 && (
        <div className="mt-2">
          <div className="text-[11px] text-slate-500 mb-1">Suggested by planner</div>
          <div className="flex flex-wrap items-center gap-1.5">
            {suggestions.map((name) => {
              const exists = cacheByName.has(name);
              const applied = current.includes(name);
              const color = cacheByName.get(name)?.color ?? "475569";
              const fg = getContrastText(color);
              if (applied) {
                return (
                  <button
                    key={name}
                    onClick={() => removeApplied(name)}
                    className="px-1.5 py-0.5 rounded text-[11px] font-medium hover:opacity-80"
                    style={{ backgroundColor: `#${color}`, color: fg }}
                    title="Auto-applied by planner — click to remove"
                  >
                    {name}
                    <span className="ml-1 opacity-70">(auto-applied)</span>
                  </button>
                );
              }
              return (
                <button
                  key={name}
                  onClick={() => applySuggestion(name)}
                  className="px-1.5 py-0.5 rounded text-[11px] font-medium border border-dashed hover:opacity-90"
                  style={{ backgroundColor: `#${color}`, color: fg, borderColor: `#${color}` }}
                  title={
                    exists
                      ? "Apply this label"
                      : "Label not in workspace yet — opens the editor pre-filled to create it"
                  }
                >
                  + {name}
                  {!exists && <span className="ml-1 opacity-70">(create)</span>}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-xs uppercase tracking-wide text-slate-500 mb-1">{title}</div>
      {children}
    </div>
  );
}

function CollapsibleSection({
  title,
  defaultOpen = true,
  children,
}: {
  title: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-2 text-left text-xs uppercase tracking-wide text-slate-500 hover:text-slate-300 mb-1"
      >
        <span
          className="transition-transform duration-150"
          style={{ display: "inline-block", transform: open ? "rotate(90deg)" : "rotate(0)" }}
        >
          ▶
        </span>
        {title}
      </button>
      {open && <div>{children}</div>}
    </div>
  );
}

function Markdown({ text }: { text: string }) {
  return (
    <div className="prose prose-sm prose-invert max-w-none prose-pre:bg-slate-950 prose-pre:border prose-pre:border-slate-800 prose-code:text-amber-200 prose-code:bg-slate-900 prose-code:px-1 prose-code:rounded prose-code:before:content-[''] prose-code:after:content-[''] prose-headings:text-slate-100 prose-strong:text-slate-100 prose-a:text-sky-400">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}

function minutesAgo(iso: any): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (isNaN(d.getTime())) return null;
  return Math.max(0, Math.floor((Date.now() - d.getTime()) / 60000));
}
