import { ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { cn } from "../lib/cn";

export function Column({
  id,
  title,
  count,
  itemIDs,
  children,
  headerExtra,
}: {
  id: string;
  title: string;
  count: number;
  itemIDs: string[];
  children?: ReactNode;
  headerExtra?: ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({ id, data: { column: id } });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "flex-1 min-w-[200px] rounded-lg p-2 border transition-colors flex flex-col min-h-0",
        isOver ? "bg-slate-800/60 border-emerald-700" : "bg-slate-900/40 border-slate-800",
      )}
    >
      <div className="flex items-center text-xs uppercase tracking-wide text-slate-400 mb-2 px-1 shrink-0">
        <span>
          {title} <span className="text-slate-500">({count})</span>
        </span>
        {headerExtra && <span className="ml-auto">{headerExtra}</span>}
      </div>
      <SortableContext items={itemIDs} strategy={verticalListSortingStrategy}>
        <div className="space-y-1 min-h-[40px] flex-1 overflow-y-auto pr-1">{children}</div>
      </SortableContext>
    </div>
  );
}
