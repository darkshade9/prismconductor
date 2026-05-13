import { useEffect, useState } from "react";
import { TriggerFanoutAnalysis, ListFanoutProposals, ApproveFanoutProposal, DismissFanoutProposal } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

export function FanoutReviewModal({
  open,
  onClose,
  workspaceID,
  issueNumber,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  issueNumber: number;
}) {
  const [proposals, setProposals] = useState<types.FanoutProposal[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [analyzing, setAnalyzing] = useState(false);

  useEffect(() => {
    if (!open) {
      setProposals([]);
      setError(null);
      setAnalyzing(false);
      return;
    }
    loadProposals();
  }, [open, workspaceID, issueNumber]);

  async function loadProposals() {
    try {
      const ps = await ListFanoutProposals(workspaceID, issueNumber);
      setProposals(ps ?? []);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  }

  async function triggerAnalysis() {
    setAnalyzing(true);
    setError(null);
    try {
      await TriggerFanoutAnalysis(workspaceID, issueNumber);
      // Analysis runs asynchronously; proposals arrive via fanout.proposals_ready event.
      // Poll once after 3s as a UX convenience for short analyses.
      setTimeout(loadProposals, 3000);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setAnalyzing(false);
    }
  }

  async function approve(id: string) {
    setBusy(true);
    setError(null);
    try {
      await ApproveFanoutProposal(id);
      await loadProposals();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function dismiss(id: string) {
    setBusy(true);
    setError(null);
    try {
      await DismissFanoutProposal(id);
      await loadProposals();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  if (!open) return null;

  const pending = proposals.filter((p) => p.status === "pending");
  const done = proposals.filter((p) => p.status !== "pending");

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="bg-white dark:bg-slate-900 rounded-lg shadow-xl w-full max-w-2xl max-h-[85vh] flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-200 dark:border-slate-700">
          <div>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">
              Cross-repo fan-out
            </h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              #{issueNumber} — proposed follow-up issues for sibling repositories
            </p>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-xl leading-none"
          >
            ×
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
          {error && (
            <div className="px-3 py-2 rounded bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 text-sm border border-red-200 dark:border-red-700">
              {error}
            </div>
          )}

          {pending.length === 0 && done.length === 0 && (
            <div className="text-center py-8 space-y-3">
              <p className="text-sm text-slate-500 dark:text-slate-400">
                No proposals yet. Run an analysis to identify cross-repo impact.
              </p>
              <button
                onClick={triggerAnalysis}
                disabled={analyzing}
                className="px-4 py-2 text-sm font-medium rounded bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                {analyzing ? "Analyzing…" : "Analyze cross-repo impact"}
              </button>
            </div>
          )}

          {pending.length > 0 && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <h3 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wide">
                  Pending ({pending.length})
                </h3>
                <button
                  onClick={triggerAnalysis}
                  disabled={analyzing}
                  className="text-xs text-indigo-600 dark:text-indigo-400 hover:underline disabled:opacity-50"
                >
                  {analyzing ? "Analyzing…" : "Re-analyze"}
                </button>
              </div>
              <div className="space-y-3">
                {pending.map((p) => (
                  <ProposalCard
                    key={p.id}
                    proposal={p}
                    busy={busy}
                    onApprove={() => approve(p.id)}
                    onDismiss={() => dismiss(p.id)}
                  />
                ))}
              </div>
            </div>
          )}

          {done.length > 0 && (
            <div>
              <h3 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wide mb-2">
                Resolved ({done.length})
              </h3>
              <div className="space-y-2">
                {done.map((p) => (
                  <div
                    key={p.id}
                    className="flex items-start gap-3 px-3 py-2 rounded border border-slate-200 dark:border-slate-700 opacity-60"
                  >
                    <span
                      className={`shrink-0 mt-0.5 text-xs font-medium px-1.5 py-0.5 rounded ${
                        p.status === "approved"
                          ? "bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300"
                          : "bg-slate-100 dark:bg-slate-800 text-slate-500"
                      }`}
                    >
                      {p.status === "approved" ? "Filed" : "Dismissed"}
                    </span>
                    <div className="min-w-0">
                      <p className="text-sm text-slate-700 dark:text-slate-300 truncate">{p.title}</p>
                      {p.status === "approved" && p.filed_issue_url && (
                        <a
                          href={p.filed_issue_url}
                          target="_blank"
                          rel="noreferrer"
                          className="text-xs text-indigo-600 dark:text-indigo-400 hover:underline"
                        >
                          #{p.filed_issue_number} ↗
                        </a>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ProposalCard({
  proposal,
  busy,
  onApprove,
  onDismiss,
}: {
  proposal: types.FanoutProposal;
  busy: boolean;
  onApprove: () => void;
  onDismiss: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="border border-slate-200 dark:border-slate-700 rounded-md overflow-hidden">
      <div className="px-3 py-2 bg-slate-50 dark:bg-slate-800/60 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <button
            onClick={() => setExpanded((v) => !v)}
            className="text-sm font-medium text-slate-800 dark:text-slate-100 text-left hover:text-indigo-600 dark:hover:text-indigo-400"
          >
            {proposal.title}
          </button>
          <div className="flex items-center gap-2 mt-0.5">
            <span className="text-xs text-slate-500 dark:text-slate-400">
              → {proposal.target_workspace_id}
            </span>
            {(proposal.labels ?? []).map((l: string) => (
              <span
                key={l}
                className="text-[10px] px-1 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300"
              >
                {l}
              </span>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <button
            onClick={onApprove}
            disabled={busy}
            className="px-2 py-1 text-xs font-medium rounded bg-emerald-600 text-white hover:bg-emerald-700 disabled:opacity-50"
          >
            File issue
          </button>
          <button
            onClick={onDismiss}
            disabled={busy}
            className="px-2 py-1 text-xs font-medium rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-600 disabled:opacity-50"
          >
            Dismiss
          </button>
        </div>
      </div>
      {expanded && (
        <div className="px-3 py-2 text-xs text-slate-600 dark:text-slate-400 whitespace-pre-wrap border-t border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
          {proposal.body || "(no description)"}
        </div>
      )}
    </div>
  );
}
