// Stub for Phase 4 §9.2 — renders one Question per type.
export type Question =
  | { id: string; type: "single_choice" | "multi_choice"; prompt: string; options: string[]; required?: boolean }
  | { id: string; type: "free_text"; prompt: string; required?: boolean }
  | { id: string; type: "yes_no"; prompt: string; required?: boolean };

export function QuestionForm({ questions }: { questions: Question[] }) {
  return (
    <ul className="space-y-3">
      {questions.map((q) => (
        <li key={q.id} className="text-sm text-slate-300">
          <div className="mb-1">{q.prompt}</div>
          {q.type === "single_choice" &&
            q.options.map((o) => (
              <label key={o} className="flex items-center gap-2 ml-2 text-slate-400">
                <input type="radio" name={q.id} value={o} /> {o}
              </label>
            ))}
          {q.type === "multi_choice" &&
            q.options.map((o) => (
              <label key={o} className="flex items-center gap-2 ml-2 text-slate-400">
                <input type="checkbox" name={q.id} value={o} /> {o}
              </label>
            ))}
          {q.type === "free_text" && <textarea className="w-full bg-slate-800 border border-slate-700 rounded p-1 text-slate-200" />}
          {q.type === "yes_no" && (
            <label className="ml-2 text-slate-400 inline-flex items-center gap-2">
              <input type="checkbox" /> Yes
            </label>
          )}
        </li>
      ))}
    </ul>
  );
}
