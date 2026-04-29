import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { types } from "../../wailsjs/go/models";
import { cn } from "../lib/cn";

export type CardProps = {
  issue: types.Issue;
  workspaceColor?: string;
  workspaceLabel?: string;
  onClick?: () => void;
};

export function Card({ issue, workspaceColor, workspaceLabel, onClick }: CardProps) {
  const id = `${issue.workspace_id}#${issue.number}`;
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id,
    data: { number: issue.number, workspaceID: issue.workspace_id, column: issue.column },
  });
  const style = {
    transform: CSS.Translate.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };

  const blocked = (issue.dependencies ?? []).length > 0;

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      onClick={(e) => {
        // Suppress click during drag.
        if (isDragging) return;
        onClick?.();
        e.stopPropagation();
      }}
      className={cn(
        "w-full text-left rounded-md border border-slate-700 bg-slate-800/70 hover:bg-slate-800 px-3 py-2 mb-2",
        "shadow-sm transition-colors cursor-grab active:cursor-grabbing select-none",
      )}
    >
      <div className="flex items-center justify-between text-xs text-slate-400">
        <span className="flex items-center gap-2">
          <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: workspaceColor ?? "#64748b" }} />
          #{issue.number}
          <span className="text-slate-500">{workspaceLabel ?? issue.workspace_id}</span>
        </span>
        {issue.priority ? <span className="text-slate-500">P{issue.priority.toFixed(2)}</span> : null}
      </div>
      <div className="text-sm text-slate-100 mt-1 line-clamp-2">{issue.title}</div>
      <div className="text-[11px] text-slate-400 mt-1 flex items-center gap-2">
        {blocked && (
          <span className="text-amber-300">
            🚫 blocked by {issue.dependencies?.map((n) => `#${n}`).join(", ")}
          </span>
        )}
        {(issue.labels ?? []).slice(0, 3).map((l) => (
          <span key={l} className="text-slate-600">·{l}</span>
        ))}
      </div>
    </div>
  );
}
