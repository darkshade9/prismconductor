import { useEffect, useState } from "react";
import { ApprovePlan, LatestPlan, RejectPlan, SubmitAnswers } from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { useIssueStore } from "../stores/issueStore";
import { AnswerState, QuestionForm, emptyAnswers, isComplete } from "./QuestionForm";

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
  const [plan, setPlan] = useState<types.Plan | null>(null);
  const [answers, setAnswers] = useState<AnswerState>(emptyAnswers());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open || !issue) return;
    setLoading(true);
    setError(null);
    setAnswers(emptyAnswers());
    LatestPlan(issue.workspace_id, issue.number)
      .then((p) => setPlan(p ?? null))
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setLoading(false));
  }, [open, issue?.workspace_id, issue?.number]);

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
      await SubmitAnswers(
        new main.AnswerSubmission({
          workspace_id: issue.workspace_id,
          issue_number: issue.number,
          revision: plan.revision,
          answers: answers.single,
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
            </>
          )}

          {error && <div className="text-red-400 text-xs">{error}</div>}
        </div>

        {plan && (
          <div className="px-4 py-2 border-t border-slate-800 flex justify-end gap-2">
            <button onClick={reject} disabled={busy} className="px-3 py-1 text-red-400 hover:text-red-300 disabled:opacity-50">
              Reject
            </button>
            {hasQuestions && (
              <button
                onClick={submit}
                disabled={busy || !allRequiredAnswered}
                className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50"
                title={!allRequiredAnswered ? "Answer all required questions first" : ""}
              >
                Submit answers & request revision
              </button>
            )}
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

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-xs uppercase tracking-wide text-slate-500 mb-1">{title}</div>
      {children}
    </div>
  );
}

// Tiny markdown stand-in: preserves line breaks, no real parser.
function Markdown({ text }: { text: string }) {
  return <div className="whitespace-pre-wrap text-slate-200 leading-relaxed">{text}</div>;
}

function minutesAgo(iso: any): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (isNaN(d.getTime())) return null;
  return Math.max(0, Math.floor((Date.now() - d.getTime()) / 60000));
}
