import { useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { FetchIssueDetail } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useLabelsStore } from "../stores/labelsStore";
import { getContrastText } from "../lib/contrast";

export function IssueSection({ issue }: { issue: types.Issue }) {
  const [detail, setDetail] = useState<types.Issue>(issue);

  useEffect(() => {
    setDetail(issue);
    FetchIssueDetail(issue.workspace_id, issue.number)
      .then((fresh) => setDetail(fresh))
      .catch(() => {/* keep existing data on error */});
  }, [issue.workspace_id, issue.number]);

  const isOpen = detail.state !== "closed";

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-2">
        <h2 className="text-base font-semibold text-slate-100 leading-snug">
          #{detail.number} — {detail.title}
        </h2>
        {detail.url && (
          <button
            type="button"
            aria-label="Open on GitHub"
            onClick={() => BrowserOpenURL(detail.url)}
            className="shrink-0 text-xs text-sky-400 hover:text-sky-300 whitespace-nowrap mt-0.5"
          >
            Open on GitHub →
          </button>
        )}
      </div>

      <div className="flex items-center gap-3 text-xs text-slate-400">
        <span className="flex items-center gap-1.5">
          {isOpen ? (
            <span className="inline-block h-2 w-2 rounded-full bg-emerald-400" aria-label="open" />
          ) : (
            <span className="inline-block h-2 w-2 rounded-full bg-slate-500" aria-label="closed" />
          )}
          {isOpen ? "Open" : "Closed"}
        </span>
        {detail.updated_at && (
          <span>Updated {relativeTime(detail.updated_at)}</span>
        )}
      </div>

      <LabelRow workspaceID={detail.workspace_id} labels={detail.labels ?? []} />

      <div className="prose prose-sm prose-invert max-w-none prose-pre:bg-slate-950 prose-pre:border prose-pre:border-slate-800 prose-code:text-amber-200 prose-code:bg-slate-900 prose-code:px-1 prose-code:rounded prose-code:before:content-[''] prose-code:after:content-[''] prose-headings:text-slate-100 prose-strong:text-slate-100 prose-a:text-sky-400">
        {detail.body ? (
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{detail.body}</ReactMarkdown>
        ) : (
          <p className="text-slate-500 italic text-sm">(no description)</p>
        )}
      </div>
    </div>
  );
}

function LabelRow({ workspaceID, labels }: { workspaceID: string; labels: string[] }) {
  const byName = useLabelsStore((s) => s.byName);

  if (labels.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-1.5">
      {labels.map((name) => {
        const lab = byName(workspaceID, name);
        const color = lab?.color ?? "475569";
        const fg = getContrastText(color);
        return (
          <span
            key={name}
            className="px-1.5 py-0.5 rounded text-[11px] font-medium"
            style={{ backgroundColor: `#${color}`, color: fg }}
            title={lab?.description || name}
          >
            {name}
          </span>
        );
      })}
    </div>
  );
}

function relativeTime(input: any): string {
  if (!input) return "";
  const d = input instanceof Date ? input : new Date(input);
  if (isNaN(d.getTime())) return "";
  const diffSec = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 30) return `${diffDay}d ago`;
  const diffMo = Math.floor(diffDay / 30);
  if (diffMo < 12) return `${diffMo}mo ago`;
  return `${Math.floor(diffMo / 12)}y ago`;
}
