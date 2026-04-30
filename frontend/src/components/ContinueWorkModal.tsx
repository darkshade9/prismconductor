import { useEffect, useRef, useState } from "react";
import { Continue } from "../../wailsjs/go/main/App";
import { useIssueStore } from "../stores/issueStore";

// Continue work modal (#80). Shown when the user clicks the "↻ Continue"
// chip on a REVIEW-column card with an open PR. Takes a free-text note
// and re-engages an execute worker on the existing branch — no new issue,
// no new PR.

export function ContinueWorkModal({
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
  prNumber: number | null;
}) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const refreshIssues = useIssueStore((s) => s.refresh);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    if (open) {
      setNote("");
      setError(null);
      // Defer focus to next tick so the modal is rendered first.
      setTimeout(() => textareaRef.current?.focus(), 0);
    }
  }, [open]);

  if (!open) return null;

  async function submit() {
    const trimmed = note.trim();
    if (!trimmed) {
      setError("Describe what needs fixing.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await Continue(workspaceID, issueNumber, trimmed);
      await refreshIssues();
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="w-[560px] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">
            Continue work on{" "}
            <span className="text-slate-400">
              #{issueNumber}
              {prNumber != null ? ` (PR #${prNumber})` : ""}
            </span>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">
            ✕
          </button>
        </div>
        <div className="px-4 py-3 space-y-3 text-sm">
          <div className="text-slate-400">
            Re-engages the agent on the existing branch. New commits land on the
            same PR — no new issue, no new PR. Tell the agent what needs fixing:
          </div>
          <textarea
            ref={textareaRef}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={`e.g. "tests in TestFoo are failing — handler_test.go:42 expects ResponseError but the new code returns 503 instead"`}
            rows={6}
            className="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-slate-500 resize-none"
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                submit();
              }
              if (e.key === "Escape") {
                e.preventDefault();
                onClose();
              }
            }}
          />
          <div className="text-xs text-slate-500">
            Tip: paste the output of <code>gh pr checks {prNumber ?? "<n>"}</code>{" "}
            for failing CI; or the relevant review comment.
          </div>
          {error && <div className="text-rose-300 text-sm">{error}</div>}
        </div>
        <div className="px-4 py-2 border-t border-slate-800 flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1 text-slate-400 hover:text-slate-200 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={submit}
            disabled={busy || !note.trim()}
            className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50"
            title="Re-engage execute worker on the existing branch (Cmd/Ctrl+Enter)"
          >
            {busy ? "…" : "Continue"}
          </button>
        </div>
      </div>
    </div>
  );
}
