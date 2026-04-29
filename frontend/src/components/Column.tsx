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
}: {
  id: string;
  title: string;
  count: number;
  itemIDs: string[];
  children?: ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({ id, data: { column: id } });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "flex-1 min-w-[200px] rounded-lg p-2 border transition-colors",
        isOver ? "bg-slate-800/60 border-emerald-700" : "bg-slate-900/40 border-slate-800",
      )}
    >
      <div className="text-xs uppercase tracking-wide text-slate-400 mb-2 px-1">
        {title} <span className="text-slate-500">({count})</span>
      </div>
      <SortableContext items={itemIDs} strategy={verticalListSortingStrategy}>
        <div className="space-y-1 min-h-[40px]">{children}</div>
      </SortableContext>
    </div>
  );
}
