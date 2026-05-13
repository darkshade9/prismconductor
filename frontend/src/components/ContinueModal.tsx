import { useEffect, useRef, useState } from "react";
import { ContinueWork, FetchPRChecks } from "../../wailsjs/go/main/App";
import { noAutoCorrect } from "../lib/inputs";

export function ContinueModal({
  open,
  onClose,
  workspaceID,
  issueNumber,
  prNumber,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  issueNumber: number;
  prNumber: number;
}) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [fetchingChecks, setFetchingChecks] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-fetch PR checks when modal opens; pre-fill textarea if successful.
  useEffect(() => {
    if (!open) {
      setNote("");
      setError(null);
      return;
    }
    textareaRef.current?.focus();
    setFetchingChecks(true);
    FetchPRChecks(workspaceID, prNumber)
      .then((out) => {
        if (out.trim()) setNote(out.trim());
      })
      .catch(() => {
        // Silently ignore — user can type manually.
      })
      .finally(() => setFetchingChecks(false));
  }, [open, workspaceID, prNumber]);

  if (!open) return null;

  async function submit() {
    if (!note.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await ContinueWork(workspaceID, issueNumber, note.trim());
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]"
      onClick={(e) => e.stopPropagation()}
      onMouseDown={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <div className="w-[560px] bg-slate-900 border border-purple-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800 bg-purple-950/30">
          <div className="text-slate-200 text-sm">
            ↻ Continue work — <span className="text-slate-400">#{issueNumber}</span>
            {prNumber > 0 && (
              <span className="ml-1 text-slate-500">· PR #{prNumber}</span>
            )}
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="px-4 py-3 space-y-2">
          <label className="block text-xs text-slate-400">
            What needs fixing?
            {fetchingChecks && (
              <span className="ml-2 text-slate-500 italic">fetching failing checks…</span>
            )}
          </label>
          <textarea
            {...noAutoCorrect}
            ref={textareaRef}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="e.g. tests failing in TestFoo — expects 200, got 404"
            rows={6}
            className="w-full bg-slate-800 border border-slate-700 rounded px-3 py-2 text-sm text-slate-100 placeholder-slate-600 resize-none focus:outline-none focus:border-purple-600"
          />
          {error && <div className="text-red-400 text-xs">{error}</div>}
        </div>
        <div className="px-4 py-2 border-t border-slate-800 flex items-center justify-between gap-2">
          <div className="text-xs text-slate-500">
            Spawns a worker on the existing PR branch.
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              disabled={busy}
              className="px-3 py-1.5 rounded border border-slate-700 text-slate-300 hover:border-slate-500 disabled:opacity-50 text-sm"
            >
              Cancel
            </button>
            <button
              onClick={submit}
              disabled={busy || !note.trim()}
              className="px-3 py-1.5 rounded bg-purple-600 text-slate-50 hover:bg-purple-500 disabled:opacity-50 text-sm"
            >
              {busy ? "Starting…" : "Continue"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
