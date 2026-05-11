import { useEffect, useState } from "react";
import { ListMidRunQuestionsLog } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";

export function QuestionsLog({
  workspaceID,
  issueNumber,
}: {
  workspaceID: string;
  issueNumber: number;
}) {
  const [entries, setEntries] = useState<main.QuestionLogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!workspaceID || !issueNumber) return;
    setLoading(true);
    ListMidRunQuestionsLog(workspaceID, issueNumber)
      .then((rows) => {
        setEntries(rows ?? []);
        setErr(null);
      })
      .catch((e) => setErr(String(e)))
      .finally(() => setLoading(false));
  }, [workspaceID, issueNumber]);

  if (loading) {
    return <div className="text-slate-500 text-xs py-2">Loading questions log…</div>;
  }
  if (err) {
    return <div className="text-red-400 text-xs py-2">Error: {err}</div>;
  }
  if (entries.length === 0) {
    return <div className="text-slate-500 text-xs py-2">No mid-run questions recorded for this issue.</div>;
  }

  return (
    <div className="space-y-2">
      {entries.map((entry, idx) => {
        const q = entry.question;
        const answered = !!entry.answer_source;
        const isArchitect = entry.answer_source === "architect";
        return (
          <div
            key={q?.id ?? idx}
            className="rounded border border-slate-700 bg-slate-900/40 p-3 text-sm"
          >
            <div className="flex items-start gap-2">
              <span className="text-xs font-mono text-slate-500 mt-0.5 shrink-0">{idx + 1}.</span>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  {q?.audience === "peer_agent" ? (
                    <span className="text-[10px] uppercase tracking-wide text-sky-400 border border-sky-700 px-1 py-0.5 rounded">
                      architect
                    </span>
                  ) : (
                    <span className="text-[10px] uppercase tracking-wide text-slate-500 border border-slate-700 px-1 py-0.5 rounded">
                      user
                    </span>
                  )}
                  {answered && (
                    <span
                      className={
                        "text-[10px] uppercase tracking-wide px-1 py-0.5 rounded border " +
                        (isArchitect
                          ? "text-emerald-400 border-emerald-700"
                          : "text-slate-300 border-slate-600")
                      }
                    >
                      {isArchitect ? "auto-answered" : "user answered"}
                    </span>
                  )}
                  {!answered && (
                    <span className="text-[10px] uppercase tracking-wide text-amber-400 border border-amber-700 px-1 py-0.5 rounded">
                      pending
                    </span>
                  )}
                </div>
                <div className="text-slate-200 break-words">{q?.prompt}</div>
                {answered && (
                  <div className="mt-1.5 text-xs text-slate-400 bg-slate-800 rounded px-2 py-1 break-words">
                    <span className="text-slate-500">Answer: </span>
                    {entry.answer_text || "—"}
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
