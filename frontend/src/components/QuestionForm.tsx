import { types } from "../../wailsjs/go/models";

export type AnswerState = {
  single: Record<string, string>;
  multi: Record<string, string[]>;
};

export function emptyAnswers(): AnswerState {
  return { single: {}, multi: {} };
}

export function QuestionForm({
  questions,
  state,
  onChange,
}: {
  questions: types.Question[];
  state: AnswerState;
  onChange: (next: AnswerState) => void;
}) {
  function setSingle(id: string, value: string) {
    onChange({ ...state, single: { ...state.single, [id]: value } });
  }
  function toggleMulti(id: string, value: string) {
    const cur = state.multi[id] ?? [];
    const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
    onChange({ ...state, multi: { ...state.multi, [id]: next } });
  }
  return (
    <ul className="space-y-3">
      {questions.map((q, idx) => (
        <li key={q.id} className="text-sm text-slate-300 border border-slate-800 rounded p-2">
          <div className="mb-1 flex items-baseline gap-1">
            <span className="text-slate-500">{idx + 1}.</span>
            <span>{q.prompt}</span>
            {q.required && <span className="text-red-400 text-xs ml-1">*</span>}
          </div>
          {q.type === "single_choice" &&
            (q.options ?? []).map((o) => (
              <label key={o} className="flex items-center gap-2 ml-2 text-slate-300 text-sm">
                <input
                  type="radio"
                  name={q.id}
                  value={o}
                  checked={state.single[q.id] === o}
                  onChange={() => setSingle(q.id, o)}
                />
                {o}
              </label>
            ))}
          {q.type === "multi_choice" &&
            (q.options ?? []).map((o) => (
              <label key={o} className="flex items-center gap-2 ml-2 text-slate-300 text-sm">
                <input
                  type="checkbox"
                  checked={(state.multi[q.id] ?? []).includes(o)}
                  onChange={() => toggleMulti(q.id, o)}
                />
                {o}
              </label>
            ))}
          {q.type === "free_text" && (
            <textarea
              value={state.single[q.id] ?? ""}
              onChange={(e) => setSingle(q.id, e.target.value)}
              rows={3}
              className="w-full bg-slate-800 border border-slate-700 rounded p-1 text-slate-200"
            />
          )}
          {q.type === "yes_no" && (
            <div className="ml-2 flex gap-3 text-sm">
              {["yes", "no"].map((v) => (
                <label key={v} className="flex items-center gap-1 text-slate-300">
                  <input
                    type="radio"
                    name={q.id}
                    value={v}
                    checked={state.single[q.id] === v}
                    onChange={() => setSingle(q.id, v)}
                  />
                  {v}
                </label>
              ))}
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

// Validate that all required questions have answers.
export function isComplete(questions: types.Question[], state: AnswerState): boolean {
  for (const q of questions) {
    if (!q.required) continue;
    if (q.type === "multi_choice") {
      if (!(state.multi[q.id]?.length)) return false;
    } else {
      if (!state.single[q.id]) return false;
    }
  }
  return true;
}
