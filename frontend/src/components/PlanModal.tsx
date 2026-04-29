import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ApprovePlan, LatestPlan, RejectPlan, SetIssueLabels, SubmitAnswers } from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { useIssueStore } from "../stores/issueStore";
import { useLabelsStore } from "../stores/labelsStore";
import { AnswerState, QuestionForm, emptyAnswers, isComplete } from "./QuestionForm";
import { LabelsModal } from "./LabelsModal";
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
}: {
  open: boolean;
  onClose: () => void;
  issue: types.Issue | null;
}) {
  const refreshIssues = useIssueStore((s) => s.refresh);
  const refreshLabels = useLabelsStore((s) => s.refresh);
  const labelsCache = useLabelsStore((s) => (issue ? s.byWorkspace[issue.workspace_id] ?? [] : []));
  const [plan, setPlan] = useState<types.Plan | null>(null);
  const [answers, setAnswers] = useState<AnswerState>(emptyAnswers());
  const [refineText, setRefineText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [labelsModal, setLabelsModal] = useState<{ open: boolean; suggestName?: string }>({
    open: false,
  });

  useEffect(() => {
    if (!open || !issue) return;
    setLoading(true);
    setError(null);
    setAnswers(emptyAnswers());
    LatestPlan(issue.workspace_id, issue.number)
      .then((p) => setPlan(p ?? null))
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setLoading(false));
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
      onClose();
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
      await ApprovePlan(issue.workspace_id, issue.number, plan.revision);
      await refreshIssues();
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
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
      await refreshIssues();
      onClose();
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
          <div className="text-slate-200">
            Plan: #{issue.number} — <span className="text-slate-400">{issue.title}</span>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="px-4 py-3 text-xs text-slate-500 flex items-center gap-3 border-b border-slate-800">
          <span>{issue.workspace_id}</span>
          {plan && (
            <>
              <span>Revision {plan.revision}</span>
              <span>Complexity: {plan.estimated_complexity || "—"}</span>
              <span>{ageMin !== null ? `Generated ${ageMin}m ago` : ""}</span>
            </>
          )}
        </div>

        <div className="flex-1 overflow-y-auto px-4 py-3 text-sm space-y-4">
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

              <Section title="What the agent plans to do">
                <Markdown text={plan.plan_markdown} />
              </Section>

              {(plan.files_to_modify ?? []).length > 0 && (
                <Section title="Files to modify">
                  <ul className="font-mono text-xs space-y-0.5">
                    {plan.files_to_modify.map((f) => (
                      <li key={f.path} className="text-slate-300">
                        <span className="text-slate-500 w-3 inline-block">{INTENT_GLYPH[f.intent] ?? "?"}</span>{" "}
                        {f.path}{" "}
                        <span className="text-slate-600">({f.intent})</span>
                      </li>
                    ))}
                  </ul>
                </Section>
              )}

              {(plan.dependencies_detected ?? []).length > 0 && (
                <Section title="Detected dependencies">
                  <ul className="text-xs space-y-0.5">
                    {plan.dependencies_detected.map((n) => (
                      <li key={n} className="text-amber-300">• Depends on #{n}</li>
                    ))}
                  </ul>
                </Section>
              )}

              {hasQuestions && (
                <Section title={`Questions (${questions.length})`}>
                  <QuestionForm questions={questions} state={answers} onChange={setAnswers} />
                </Section>
              )}

              <Section title="Refine plan (free-form)">
                <textarea
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
                  : ""
              }
            >
              {hasQuestions ? "Submit answers & request revision" : "Request revision"}
            </button>
            <button
              onClick={approve}
              disabled={busy || (hasQuestions && !plan.ready_to_execute)}
              className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
              title={hasQuestions && !plan.ready_to_execute ? "Submit answers and wait for the next revision before approving" : ""}
            >
              {busy ? "…" : "Approve"}
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
  const suggestions = (plan.suggested_labels ?? []).filter((n) => !current.includes(n));

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
              const color = cacheByName.get(name)?.color ?? "475569";
              const fg = getContrastText(color);
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
