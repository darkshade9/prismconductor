import { useCallback, useEffect, useMemo, useState } from "react";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import {
  CollisionDetection,
  DndContext,
  DragEndEvent,
  DragOverEvent,
  DragOverlay,
  DragStartEvent,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { arrayMove } from "@dnd-kit/sortable";
import { ArchiveDone, FilterIssuesByActiveGoal } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useIssueStore, Column as ColumnID } from "../stores/issueStore";
import { matchesQuery } from "../lib/matchesQuery";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { Card } from "./Card";
import { Column } from "./Column";

const COLUMNS: { id: ColumnID; title: string }[] = [
  { id: "todo", title: "TODO" },
  { id: "plan", title: "PLAN" },
  { id: "in_progress", title: "IN_PROGRESS" },
  { id: "review", title: "REVIEW" },
  { id: "done", title: "DONE" },
];

const cardID = (i: types.Issue) => `${i.workspace_id}#${i.number}`;

// Order TODO by orchestrator priority desc when the user hasn't manually
// dragged anything yet. Heuristic: if at least one card has a non-zero
// priority, sort the whole column by priority. Manual drags persist
// manual_order in SQLite, which already sorts the ListIssues query — but we
// can't see manual_order from the JSON, so we conservatively re-sort only when
// priority is informative (>= 1 non-zero).
function sortTodoByPriority(items: types.Issue[]): types.Issue[] {
  const hasPriority = items.some((i) => (i.priority ?? 0) > 0);
  if (!hasPriority) return items;
  return [...items].sort((a, b) => {
    const aBlocked = (a.dependencies ?? []).length > 0 ? 1 : 0;
    const bBlocked = (b.dependencies ?? []).length > 0 ? 1 : 0;
    if (aBlocked !== bBlocked) return aBlocked - bBlocked; // unblocked first
    return (b.priority ?? 0) - (a.priority ?? 0);
  });
}

const fromCardID = (id: string) => {
  const [ws, num] = id.split("#");
  return { workspaceID: ws, number: parseInt(num, 10) };
};

export function Board({ onCardClick }: { onCardClick?: (issue: types.Issue) => void }) {
  const { issues, refresh, applyLocalMove, applyLocalReorder, moveColumn, reorder, searchQuery } = useIssueStore();
  const { workspaces, selectedID } = useWorkspaceStore();
  const [filteredTodoNums, setFilteredTodoNums] = useState<Set<string> | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  // Capture the source column at drag-start. onDragOver does an optimistic
  // local move (so the dragged card visually slips into the destination
  // column mid-drag), which mutates the issue's `column` field. Without
  // capturing the start column we'd later think source === target and skip
  // the backend MoveIssueColumn call.
  const [sourceColAtStart, setSourceColAtStart] = useState<ColumnID | null>(null);

  useEffect(() => {
    refresh(selectedID ?? "");
    const off = EventsOn("bus.orchestrator_ran", () => refresh(selectedID ?? ""));
    return () => {
      if (typeof off === "function") off();
    };
  }, [refresh, selectedID]);

  // Apply active goal's IssueQuery to TODO column when one exists.
  useEffect(() => {
    let cancelled = false;
    FilterIssuesByActiveGoal(issues)
      .then((scoped) => {
        if (cancelled) return;
        if (!scoped) {
          setFilteredTodoNums(null);
          return;
        }
        const set = new Set(scoped.map(cardID));
        setFilteredTodoNums(set);
      })
      .catch(() => setFilteredTodoNums(null));
    return () => {
      cancelled = true;
    };
  }, [issues]);

  const wsMeta = useMemo(() => {
    const m = new Map<string, { color: string; label: string }>();
    workspaces.forEach((w) => m.set(w.id, { color: w.color, label: w.display_name || w.id }));
    return m;
  }, [workspaces]);

  // Group issues by column. For TODO, drop anything excluded by the active goal,
  // and order by orchestrator priority desc when no manual reorder has happened.
  const grouped = useMemo(() => {
    const out: Record<ColumnID, types.Issue[]> = {
      todo: [],
      plan: [],
      in_progress: [],
      review: [],
      done: [],
    };
    for (const i of issues) {
      const col = (i.column || "todo") as ColumnID;
      if (col === "todo" && filteredTodoNums && !filteredTodoNums.has(cardID(i))) continue;
      if (!matchesQuery(i.title, searchQuery)) continue;
      if (out[col]) out[col].push(i);
    }
    // ListIssues already returns by (column, manual_order, number). For TODO, if
    // every item shares manual_order=0 (no human reorder yet), use priority.
    out.todo = sortTodoByPriority(out.todo);
    return out;
  }, [issues, filteredTodoNums, searchQuery]);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  // Pointer-first collision: the column activates as soon as the cursor
  // enters it, not when 100% of the card geometry has crossed the edge.
  // rectIntersection is the keyboard-drag fallback (no pointer position).
  const collisionDetection: CollisionDetection = (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) return pointer;
    return rectIntersection(args);
  };

  function findIssue(id: string): types.Issue | undefined {
    const { workspaceID, number } = fromCardID(id);
    return issues.find((i) => i.workspace_id === workspaceID && i.number === number);
  }

  function findColumn(id: string): ColumnID | null {
    if (COLUMNS.some((c) => c.id === id)) return id as ColumnID;
    const iss = findIssue(id);
    return (iss?.column as ColumnID) ?? null;
  }

  function onDragStart(e: DragStartEvent) {
    const id = String(e.active.id);
    setActiveId(id);
    const iss = findIssue(id);
    setSourceColAtStart(iss ? ((iss.column || "todo") as ColumnID) : null);
  }

  // Cross-column dragover: live-move the card so the user sees the destination
  // column open up before they drop.
  function onDragOver(e: DragOverEvent) {
    const { active, over } = e;
    if (!over) return;
    const activeStr = String(active.id);
    const overStr = String(over.id);
    const activeCol = findColumn(activeStr);
    const overCol = findColumn(overStr);
    if (!activeCol || !overCol || activeCol === overCol) return;
    const activeIssue = findIssue(activeStr);
    if (!activeIssue) return;
    applyLocalMove(activeIssue.workspace_id, activeIssue.number, overCol);
  }

  async function onDragEnd(e: DragEndEvent) {
    setActiveId(null);
    const startCol = sourceColAtStart;
    setSourceColAtStart(null);
    const { active, over } = e;
    // eslint-disable-next-line no-console
    console.debug(`[Board] onDragEnd active=${String(active.id)} over=${String(over?.id ?? "none")} startCol=${startCol ?? "?"}`);
    if (!over) return;
    const activeStr = String(active.id);
    const overStr = String(over.id);
    const activeIssue = findIssue(activeStr);
    if (!activeIssue) return;

    const targetCol = findColumn(overStr);
    if (!targetCol) return;

    // Compute the new in-column ordering after the drop.
    // Use the column captured at drag-start, not the (possibly already
    // optimistically-updated) current column on the issue.
    const sourceCol = (startCol ?? (activeIssue.column || "todo")) as ColumnID;
    const colIssues = (grouped[targetCol] ?? []).slice();

    if (sourceCol !== targetCol) {
      // Cross-column move: persist the column change. The dragover handler
      // already optimistically moved the card to the target column, but the
      // grouped snapshot above was computed pre-drag. Re-compute from the
      // store directly.
      const inTarget = issues.filter(
        (i) => (i.column as ColumnID) === targetCol && i.workspace_id === activeIssue.workspace_id,
      );
      // Place the dragged item at the end if dropped on the column itself,
      // otherwise insert at the target item's index.
      const overIssue = findIssue(overStr);
      const targetNumbers = inTarget
        .filter((i) => i.number !== activeIssue.number)
        .map((i) => i.number);
      let insertAt = targetNumbers.length;
      if (overIssue) insertAt = targetNumbers.indexOf(overIssue.number);
      if (insertAt < 0) insertAt = targetNumbers.length;
      targetNumbers.splice(insertAt, 0, activeIssue.number);

      await moveColumn(activeIssue.workspace_id, activeIssue.number, targetCol);
      if (targetNumbers.length > 1) {
        await reorder(activeIssue.workspace_id, targetCol, targetNumbers);
      }
      return;
    }

    // Same-column reorder.
    const overIssue = findIssue(overStr);
    if (!overIssue || overIssue.number === activeIssue.number) return;
    const ids = colIssues.map((i) => i.number);
    const oldIdx = ids.indexOf(activeIssue.number);
    const newIdx = ids.indexOf(overIssue.number);
    if (oldIdx < 0 || newIdx < 0) return;
    const next = arrayMove(ids, oldIdx, newIdx);
    applyLocalReorder(activeIssue.workspace_id, targetCol, next);
    await reorder(activeIssue.workspace_id, targetCol, next);
  }

  const activeIssue = activeId ? findIssue(activeId) : null;

  const archiveDone = useCallback(async () => {
    const items = grouped.done ?? [];
    const count = items.length;
    if (count === 0) return;
    if (count > 5 && !window.confirm(`Archive ${count} cards from DONE?`)) return;
    await ArchiveDone(selectedID ?? "");
    refresh(selectedID ?? "");
  }, [grouped.done, selectedID, refresh]);

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragEnd={onDragEnd}
      onDragCancel={() => {
        setActiveId(null);
        setSourceColAtStart(null);
      }}
    >
      <div className="flex gap-3 px-4 pb-4 overflow-x-auto h-full">
        {COLUMNS.map((c) => {
          const items = grouped[c.id] ?? [];
          const headerExtra =
            c.id === "done" && items.length > 0 ? (
              <button
                onClick={archiveDone}
                title={`Archive all ${items.length} DONE card${items.length === 1 ? "" : "s"}`}
                className="text-[10px] text-slate-500 hover:text-slate-200 px-1.5 py-0.5 rounded border border-slate-800 hover:border-slate-600"
              >
                📦 Archive {items.length}
              </button>
            ) : undefined;
          return (
            <Column
              key={c.id}
              id={c.id}
              title={c.title}
              count={items.length}
              itemIDs={items.map(cardID)}
              headerExtra={headerExtra}
            >
              {items.map((iss) => {
                const meta = wsMeta.get(iss.workspace_id);
                return (
                  <Card
                    key={cardID(iss)}
                    issue={iss}
                    workspaceColor={meta?.color}
                    workspaceLabel={meta?.label}
                    onClick={() => onCardClick?.(iss)}
                  />
                );
              })}
              {items.length === 0 && (
                <div className="text-xs text-slate-700 px-1 py-2">drop here</div>
              )}
            </Column>
          );
        })}
      </div>
      <DragOverlay>
        {activeIssue ? (
          <Card
            issue={activeIssue}
            workspaceColor={wsMeta.get(activeIssue.workspace_id)?.color}
            workspaceLabel={wsMeta.get(activeIssue.workspace_id)?.label}
          />
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
