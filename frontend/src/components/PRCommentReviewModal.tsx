import { useEffect, useState } from "react";
import { ListPRComments, AcknowledgeComment, RequestFixForComments, PostPRComment } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { noAutoCorrect } from "../lib/inputs";

export function PRCommentReviewModal({
  open,
  onClose,
  workspaceID,
  issueNumber,
  autoContinue,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  issueNumber: number;
  autoContinue: boolean;
}) {
  const [comments, setComments] = useState<types.PRComment[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [postBody, setPostBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"unread" | "post">("unread");

  useEffect(() => {
    if (!open) {
      setSelected(new Set());
      setPostBody("");
      setError(null);
      setTab("unread");
      return;
    }
    loadComments();
  }, [open, workspaceID, issueNumber]);

  async function loadComments() {
    try {
      const all = await ListPRComments(workspaceID, issueNumber);
      setComments(all ?? []);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  }

  const unread = comments.filter((c) => !c.read_at);

  function toggleSelect(id: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function ackOne(id: number) {
    setBusy(true);
    setError(null);
    try {
      await AcknowledgeComment(workspaceID, issueNumber, id);
      await loadComments();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function requestFixSelected() {
    if (selected.size === 0) return;
    setBusy(true);
    setError(null);
    try {
      await RequestFixForComments(workspaceID, issueNumber, Array.from(selected));
      onClose();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function postComment(requestFix: boolean) {
    if (!postBody.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await PostPRComment(workspaceID, issueNumber, postBody.trim(), requestFix);
      setPostBody("");
      if (requestFix) {
        onClose();
      } else {
        await loadComments();
        setTab("unread");
      }
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]"
      onClick={(e) => e.stopPropagation()}
      onMouseDown={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <div className="w-[600px] max-h-[80vh] bg-slate-900 border border-orange-700 rounded-lg flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800 bg-orange-950/30">
          <div className="flex items-center gap-3">
            <span className="text-slate-200 text-sm font-medium">PR Comments</span>
            <span className="text-slate-400 text-xs">#{issueNumber}</span>
            {unread.length > 0 && (
              <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-orange-700/40 text-orange-200 border border-orange-600">
                {unread.length} unread
              </span>
            )}
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-slate-800">
          <button
            onClick={() => setTab("unread")}
            className={`px-4 py-2 text-xs font-medium border-b-2 transition-colors ${
              tab === "unread"
                ? "border-orange-500 text-orange-300"
                : "border-transparent text-slate-400 hover:text-slate-300"
            }`}
          >
            Review ({unread.length})
          </button>
          <button
            onClick={() => setTab("post")}
            className={`px-4 py-2 text-xs font-medium border-b-2 transition-colors ${
              tab === "post"
                ? "border-orange-500 text-orange-300"
                : "border-transparent text-slate-400 hover:text-slate-300"
            }`}
          >
            Post Comment
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4">
          {tab === "unread" && (
            <>
              {unread.length === 0 ? (
                <div className="text-slate-500 text-sm text-center py-8">No unread comments.</div>
              ) : (
                <div className="space-y-3">
                  <div className="text-xs text-slate-500 mb-2">
                    Select comments to batch Request Fix, or use OK to mark individual comments as read.
                  </div>
                  {unread.map((c) => (
                    <div
                      key={c.comment_id}
                      className={`rounded border p-3 cursor-pointer transition-colors ${
                        selected.has(c.comment_id)
                          ? "border-orange-600 bg-orange-950/20"
                          : "border-slate-700 bg-slate-800/40 hover:border-slate-600"
                      }`}
                      onClick={() => toggleSelect(c.comment_id)}
                    >
                      <div className="flex items-center justify-between mb-1">
                        <div className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={selected.has(c.comment_id)}
                            onChange={() => toggleSelect(c.comment_id)}
                            onClick={(e) => e.stopPropagation()}
                            className="accent-orange-500"
                          />
                          <span className="text-xs font-medium text-slate-300">{c.author}</span>
                          {c.kind === "review" && c.file_path && (
                            <span className="text-[10px] text-slate-500 font-mono">
                              {c.file_path}{c.line_number ? `:${c.line_number}` : ""}
                            </span>
                          )}
                          <span className="text-[10px] text-slate-600 capitalize">{c.kind}</span>
                        </div>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            ackOne(c.comment_id);
                          }}
                          disabled={busy}
                          className="text-[10px] text-slate-400 hover:text-slate-200 px-2 py-0.5 rounded border border-slate-700 hover:border-slate-500 disabled:opacity-50"
                        >
                          OK
                        </button>
                      </div>
                      <p className="text-sm text-slate-200 whitespace-pre-wrap line-clamp-4">{c.body}</p>
                      {c.pending_post && (
                        <span className="text-[10px] text-yellow-400 mt-1 block">⏳ Pending sync</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </>
          )}

          {tab === "post" && (
            <div className="space-y-3">
              <label className="block text-xs text-slate-400">Comment body</label>
              <textarea
                {...noAutoCorrect}
                value={postBody}
                onChange={(e) => setPostBody(e.target.value)}
                placeholder="Leave a comment on this PR…"
                rows={6}
                className="w-full bg-slate-800 border border-slate-700 rounded px-3 py-2 text-sm text-slate-100 placeholder-slate-600 resize-none focus:outline-none focus:border-orange-600"
              />
            </div>
          )}
        </div>

        {/* Footer */}
        {error && <div className="px-4 py-1 text-red-400 text-xs border-t border-slate-800">{error}</div>}
        <div className="px-4 py-2 border-t border-slate-800 flex items-center justify-between gap-2">
          {tab === "unread" ? (
            <>
              <div className="text-xs text-slate-500">
                {selected.size > 0 ? `${selected.size} selected` : "Select comments to batch request fix"}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={onClose}
                  disabled={busy}
                  className="px-3 py-1.5 rounded border border-slate-700 text-slate-300 hover:border-slate-500 disabled:opacity-50 text-sm"
                >
                  Close
                </button>
                <button
                  onClick={requestFixSelected}
                  disabled={busy || selected.size === 0}
                  className="px-3 py-1.5 rounded bg-orange-600 text-slate-50 hover:bg-orange-500 disabled:opacity-50 text-sm"
                >
                  {busy ? "Working…" : `Request Fix (${selected.size})`}
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="text-xs text-slate-500">
                {autoContinue
                  ? "Request Fix auto-spawns a Continue Work session."
                  : "Request Fix will ask for confirmation."}
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
                  onClick={() => postComment(false)}
                  disabled={busy || !postBody.trim()}
                  className="px-3 py-1.5 rounded border border-slate-600 text-slate-200 hover:bg-slate-700 disabled:opacity-50 text-sm"
                >
                  {busy ? "Posting…" : "Comment Only"}
                </button>
                <button
                  onClick={() => postComment(true)}
                  disabled={busy || !postBody.trim()}
                  className="px-3 py-1.5 rounded bg-orange-600 text-slate-50 hover:bg-orange-500 disabled:opacity-50 text-sm"
                >
                  {busy ? "Working…" : "Comment & Request Fix"}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
