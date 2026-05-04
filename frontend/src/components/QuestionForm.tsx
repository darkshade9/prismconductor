import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { types } from "../../wailsjs/go/models";
import { CopyMenu, CopyAction } from "./CopyMenu";
import { noAutoCorrect } from "../lib/inputs";

export type AnswerState = {
  single: Record<string, string>;
  multi: Record<string, string[]>;
};

export function emptyAnswers(): AnswerState {
  return { single: {}, multi: {} };
}

export function initialAnswers(questions: types.Question[]): AnswerState {
  const state = emptyAnswers();
  for (const q of questions) {
    const def = q.default ?? null;
    if (!def) continue;
    if (q.type === "multi_choice") {
      state.multi[q.id] = [def];
    } else {
      state.single[q.id] = def;
    }
  }
  return state;
}

// Many planner outputs cram the prompt + every option into the question text
// ("A) thing... B) other thing..."), then send `options: ["A","B","C"]` —
// useless without the prompt. Detect that pattern and pull each option's
// body out of the prompt so radios render the actual choice text.
function expandLetterOptions(
  prompt: string,
  options: string[],
): { prompt: string; expanded: { id: string; label: string }[] } {
  if (options.length === 0) return { prompt, expanded: [] };
  const allShort = options.every((o) => /^[A-Z]$/.test(o.trim()));
  if (!allShort) {
    return { prompt, expanded: options.map((o) => ({ id: o, label: o })) };
  }
  const lettered: { id: string; label: string }[] = [];
  let trimmedPrompt = prompt;
  for (const letter of options.map((o) => o.trim())) {
    // Match the letter followed by ) or . then capture until the next letter
    // marker or end of prompt. Greedy across newlines.
    const re = new RegExp(`\\b${letter}[\\.)]\\s+([\\s\\S]*?)(?=\\s+[A-Z][\\.)]\\s+|$)`, "m");
    const m = trimmedPrompt.match(re);
    if (m && m[1]) {
      lettered.push({ id: letter, label: m[1].trim() });
    } else {
      lettered.push({ id: letter, label: letter });
    }
  }
  // Strip the answer block from the prompt: cut everything from the first
  // "<letter>)" or "<letter>." onward.
  const firstLetterIdx = trimmedPrompt.search(/\s+[A-Z][\.)]\s+/);
  if (firstLetterIdx > 0) {
    trimmedPrompt = trimmedPrompt.slice(0, firstLetterIdx).trim();
  }
  return { prompt: trimmedPrompt, expanded: lettered };
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
  const [qMenu, setQMenu] = useState<{ x: number; y: number; actions: CopyAction[] } | null>(null);

  function setSingle(id: string, value: string) {
    onChange({ ...state, single: { ...state.single, [id]: value } });
  }
  function toggleMulti(id: string, value: string) {
    const cur = state.multi[id] ?? [];
    const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
    onChange({ ...state, multi: { ...state.multi, [id]: next } });
  }
  function openQMenu(e: React.MouseEvent, actions: CopyAction[]) {
    e.preventDefault();
    e.stopPropagation();
    setQMenu({ x: e.clientX, y: e.clientY, actions });
  }

  return (
    <>
    <ol className="space-y-4 list-none p-0">
      {questions.map((q, idx) => {
        const { prompt, expanded } = expandLetterOptions(q.prompt, q.options ?? []);
        return (
          <li
            key={q.id}
            className="rounded-md border border-slate-700 bg-slate-900/40 p-3"
          >
            <div className="flex items-baseline gap-2 mb-2">
              <span className="text-xs font-mono text-slate-500 mt-0.5">{idx + 1}.</span>
              <div
                className="flex-1 prose prose-sm prose-invert max-w-none prose-p:my-0 prose-code:text-amber-200 prose-code:bg-slate-950 prose-code:px-1 prose-code:rounded prose-code:before:content-[''] prose-code:after:content-['']"
                onContextMenu={(e) => openQMenu(e, [{ label: "Copy prompt", text: prompt }])}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{prompt}</ReactMarkdown>
              </div>
              {q.required && !q.default && (
                <span
                  className="text-[10px] uppercase tracking-wide text-amber-400 whitespace-nowrap"
                  title="Planner did not provide a recommendation for this question"
                >
                  ⚠ no recommendation
                </span>
              )}
              {q.required && <span className="text-red-400 text-xs">*</span>}
            </div>

            {(q.type === "single_choice" || q.type === "multi_choice") && (
              <div className="space-y-1.5 mt-2">
                {expanded.map((opt) => {
                  const checked =
                    q.type === "multi_choice"
                      ? (state.multi[q.id] ?? []).includes(opt.id)
                      : state.single[q.id] === opt.id;
                  const recommended = q.default === opt.id;
                  return (
                    <label
                      key={opt.id}
                      className={
                        "flex items-start gap-2 px-2 py-1.5 rounded border cursor-pointer " +
                        (checked
                          ? "border-sky-600 bg-sky-950/30"
                          : "border-slate-800 hover:border-slate-600 hover:bg-slate-900/60")
                      }
                      onContextMenu={(e) => openQMenu(e, [{ label: "Copy option", text: opt.label }])}
                    >
                      <input
                        type={q.type === "multi_choice" ? "checkbox" : "radio"}
                        name={q.id}
                        value={opt.id}
                        checked={checked}
                        onChange={() =>
                          q.type === "multi_choice"
                            ? toggleMulti(q.id, opt.id)
                            : setSingle(q.id, opt.id)
                        }
                        className="mt-1 shrink-0"
                      />
                      <div className="flex-1 min-w-0">
                        {opt.id !== opt.label && (
                          <span className="text-xs font-mono text-slate-500 mr-2">{opt.id})</span>
                        )}
                        <span
                          className={
                            "text-sm whitespace-pre-wrap break-words " +
                            (recommended ? "text-emerald-300 font-bold" : "text-slate-200")
                          }
                        >
                          {opt.label}
                        </span>
                        {recommended && (
                          <span className="ml-2 text-[10px] uppercase tracking-wide text-emerald-400 whitespace-nowrap">
                            ★ recommended
                          </span>
                        )}
                      </div>
                    </label>
                  );
                })}
              </div>
            )}

            {q.type === "free_text" && (
              <>
                <textarea
                  {...noAutoCorrect}
                  value={state.single[q.id] ?? ""}
                  onChange={(e) => setSingle(q.id, e.target.value)}
                  rows={3}
                  className="w-full bg-slate-800 border border-slate-700 rounded p-2 text-sm text-slate-200 mt-2"
                />
                {q.default && state.single[q.id] !== q.default && (
                  <button
                    type="button"
                    onClick={() => setSingle(q.id, q.default!)}
                    className="text-xs text-emerald-400 hover:text-emerald-300 mt-1"
                  >
                    ↺ recommendation
                  </button>
                )}
              </>
            )}

            {q.type === "yes_no" && (
              <div className="flex gap-2 mt-2">
                {["yes", "no"].map((v) => {
                  const checked = state.single[q.id] === v;
                  const recommended = q.default === v;
                  return (
                    <label
                      key={v}
                      className={
                        "flex items-center gap-2 px-3 py-1.5 rounded border cursor-pointer text-sm " +
                        (checked
                          ? "border-sky-600 bg-sky-950/30 text-slate-100"
                          : "border-slate-800 hover:border-slate-600 hover:bg-slate-900/60 text-slate-300")
                      }
                    >
                      <input
                        type="radio"
                        name={q.id}
                        value={v}
                        checked={checked}
                        onChange={() => setSingle(q.id, v)}
                      />
                      <span className={recommended ? "text-emerald-300 font-bold" : undefined}>{v}</span>
                      {recommended && (
                        <span className="ml-1 text-[10px] uppercase tracking-wide text-emerald-400">★</span>
                      )}
                    </label>
                  );
                })}
              </div>
            )}
          </li>
        );
      })}
    </ol>
    {qMenu && (
      <CopyMenu x={qMenu.x} y={qMenu.y} actions={qMenu.actions} onClose={() => setQMenu(null)} />
    )}
    </>
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
