import { useEffect, useState } from "react";
import { GetMidRunQuestion, SubmitMidRunAnswer } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { AnswerState, QuestionForm, emptyAnswers, initialAnswers, isComplete } from "./QuestionForm";

// MidRunQuestionModal renders a single mid-run question (#17) that the worker
// raised via /conductor-question. The question lives at
// .prismconductor/questions/<id>.json on disk; the answer file (written by
// SubmitMidRunAnswer below) trips the answer-file watcher, which spawns a
// fresh execute worker on the same branch to continue the work.
export function MidRunQuestionModal({
  open,
  onClose,
  workspaceID,
  issueNumber,
  questionID,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  issueNumber: number;
  questionID: string;
}) {
  const [question, setQuestion] = useState<types.Question | null>(null);
  const [state, setState] = useState<AnswerState>(emptyAnswers());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !questionID) return;
    setLoading(true);
    setError(null);
    GetMidRunQuestion(workspaceID, issueNumber, questionID)
      .then((q) => {
        setQuestion(q);
        setState(initialAnswers([q]));
      })
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setLoading(false));
  }, [open, workspaceID, issueNumber, questionID]);

  if (!open) return null;

  const ready = question != null && isComplete([question], state);

  async function submit() {
    if (!question) return;
    setBusy(true);
    setError(null);
    try {
      const answer =
        question.type === "multi_choice"
          ? ""
          : (state.single[question.id] ?? question.default ?? "");
      const multi = question.type === "multi_choice"
        ? (state.multi[question.id] ?? (question.default ? [question.default] : []))
        : undefined;
      await SubmitMidRunAnswer(
        workspaceID,
        issueNumber,
        new types.MidRunAnswer({
          question_id: question.id,
          answer,
          multi,
        }),
      );
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[600px] max-h-[80vh] bg-slate-900 border border-amber-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800 bg-amber-950/30">
          <div className="text-slate-200">
            ❓ Mid-run question — <span className="text-slate-400">#{issueNumber}</span>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="flex-1 overflow-y-auto px-4 py-3 text-sm space-y-3">
          {loading && <div className="text-slate-500">loading…</div>}
          {error && <div className="text-red-400">{error}</div>}
          {question && (
            <QuestionForm questions={[question]} state={state} onChange={setState} />
          )}
        </div>
        <div className="px-4 py-2 border-t border-slate-800 flex items-center justify-between gap-2">
          <div className="text-xs text-slate-500">
            Answering re-spawns the execute worker on the same branch.
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              disabled={busy}
              className="px-3 py-1.5 rounded border border-slate-700 text-slate-300 hover:border-slate-500 disabled:opacity-50"
            >
              Dismiss
            </button>
            <button
              onClick={submit}
              disabled={busy || !ready}
              className="px-3 py-1.5 rounded bg-amber-600 text-slate-50 hover:bg-amber-500 disabled:opacity-50"
            >
              {busy ? "Submitting…" : "Submit answer"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
