import { useEffect, useState } from "react";
import {
  DndContext,
  DragEndEvent,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ListPools,
  ListProviders,
  SavePool,
  DeletePool,
  ResetPoolCounters,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { main, types, workerpool } from "../../wailsjs/go/models";
import { PoolEditModal } from "./PoolEditModal";

type Role = "plan" | "work" | "orchestrator";

const ROLE_ORDER: Role[] = ["orchestrator", "plan", "work"];
const ROLE_LABEL: Record<Role, string> = {
  orchestrator: "Orchestrator",
  plan: "Plan pools",
  work: "Work pools",
};

export function PoolsPanel() {
  const [pools, setPools] = useState<workerpool.PoolStatus[]>([]);
  const [providers, setProviders] = useState<main.ProviderInfo[]>([]);
  const [editing, setEditing] = useState<types.Pool | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleteErr, setDeleteErr] = useState<Record<string, string>>({});

  async function refresh() {
    try {
      const [ps, pv] = await Promise.all([ListPools(), ListProviders()]);
      setPools(ps ?? []);
      setProviders(pv ?? []);
    } catch (err) {
      console.error("PoolsPanel refresh", err);
    }
  }

  useEffect(() => {
    refresh();
    const offFreed = EventsOn("bus.worker_slot_freed", refresh);
    const offChanged = EventsOn("bus.agent_count_changed", refresh);
    const offState = EventsOn("session.state", refresh);
    const offPending = EventsOn("bus.pending_pool_enqueued", refresh);
    const offDequeued = EventsOn("bus.pending_pool_dequeued", refresh);
    return () => {
      if (typeof offFreed === "function") offFreed();
      if (typeof offChanged === "function") offChanged();
      if (typeof offState === "function") offState();
      if (typeof offPending === "function") offPending();
      if (typeof offDequeued === "function") offDequeued();
    };
  }, []);

  const providerByKind = new Map(providers.map((p) => [p.kind, p]));

  function poolRole(p: types.Pool): Role {
    return ((p.role as Role) || "work") as Role;
  }

  const grouped: Record<Role, workerpool.PoolStatus[]> = {
    orchestrator: [],
    plan: [],
    work: [],
  };
  for (const row of pools) grouped[poolRole(row.pool)].push(row);

  const hasEnabled = (role: Role) =>
    grouped[role].some((row) => {
      const info = providerByKind.get(row.pool.provider);
      return row.pool.enabled && row.pool.capacity > 0 && info?.can_spawn;
    });

  async function onToggleEnabled(p: types.Pool, next: boolean) {
    const updated = types.Pool.createFrom({ ...p, enabled: next });
    try {
      await SavePool(updated);
      await refresh();
    } catch (err) {
      console.error("toggle enabled", err);
    }
  }

  async function onRemove(p: types.Pool) {
    setDeleteErr((s) => ({ ...s, [p.id]: "" }));
    try {
      await DeletePool(p.id);
      await refresh();
    } catch (err: any) {
      const msg = String(err?.message ?? err ?? "");
      const m = /(\d+)\s+active worker/.exec(msg);
      const n = m ? parseInt(m[1], 10) : 0;
      const friendly =
        n > 0
          ? `Pool has ${n} active worker(s); cancel them or wait for completion.`
          : msg || "Delete failed.";
      setDeleteErr((s) => ({ ...s, [p.id]: friendly }));
    }
  }

  async function onReorder(role: Role, oldIndex: number, newIndex: number) {
    const rows = [...grouped[role]];
    const reordered = arrayMove(rows, oldIndex, newIndex);
    // Assign sequential priorities (0 = highest preference).
    const updates = reordered.map((row, i) =>
      SavePool(types.Pool.createFrom({ ...row.pool, priority: i }))
    );
    try {
      await Promise.all(updates);
      await refresh();
    } catch (err) {
      console.error("reorder pools", err);
    }
  }

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  return (
    <div className="space-y-3 text-sm">
      {pools.length > 0 && !hasEnabled("plan") && (
        <div className="rounded border border-amber-700 bg-amber-950/40 px-3 py-2 text-amber-200">
          No plan pool configured. Auto-pull is paused for affected workspaces
          until a plan pool is added.
        </div>
      )}
      {pools.length > 0 && !hasEnabled("work") && (
        <div className="rounded border border-amber-700 bg-amber-950/40 px-3 py-2 text-amber-200">
          No work pool configured. Plan approvals cannot spawn execute workers.
        </div>
      )}
      {pools.length > 0 && !hasEnabled("orchestrator") && (
        <div className="rounded border border-slate-700 bg-slate-950/40 px-3 py-2 text-slate-400">
          No orchestrator pool configured. Backlog ranking is skipped.
        </div>
      )}

      {pools.length === 0 && (
        <div className="text-slate-500">No pools configured. Add one below.</div>
      )}

      {ROLE_ORDER.map((role) => {
        const rows = grouped[role];
        if (rows.length === 0) return null;
        const sortable = role !== "orchestrator" && rows.length > 1;
        return (
          <div key={role} className="space-y-2">
            <div className="text-xs uppercase tracking-wide text-slate-500 flex items-center gap-2">
              {ROLE_LABEL[role]} ({rows.length})
              {sortable && (
                <span className="normal-case tracking-normal text-slate-600">
                  · drag to reorder preference
                </span>
              )}
            </div>
            {sortable ? (
              <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={(e: DragEndEvent) => {
                  const { active, over } = e;
                  if (!over || active.id === over.id) return;
                  const oldIdx = rows.findIndex((r) => r.pool.id === active.id);
                  const newIdx = rows.findIndex((r) => r.pool.id === over.id);
                  if (oldIdx !== -1 && newIdx !== -1) onReorder(role, oldIdx, newIdx);
                }}
              >
                <SortableContext
                  items={rows.map((r) => r.pool.id)}
                  strategy={verticalListSortingStrategy}
                >
                  {rows.map((row, idx) => (
                    <SortablePoolRow
                      key={row.pool.id}
                      row={row}
                      role={role}
                      isFirst={idx === 0}
                      isLast={idx === rows.length - 1}
                      providerByKind={providerByKind}
                      deleteErr={deleteErr[row.pool.id]}
                      onToggleEnabled={onToggleEnabled}
                      onEdit={() => setEditing(row.pool)}
                      onRemove={() => onRemove(row.pool)}
                    />
                  ))}
                </SortableContext>
              </DndContext>
            ) : (
              rows.map((row) => {
                const err = deleteErr[row.pool.id];
                return (
                  <PoolRow
                    key={row.pool.id}
                    row={row}
                    role={role}
                    providerByKind={providerByKind}
                    deleteErr={err}
                    dragHandle={null}
                    onToggleEnabled={onToggleEnabled}
                    onEdit={() => setEditing(row.pool)}
                    onRemove={() => onRemove(row.pool)}
                  />
                );
              })
            )}
          </div>
        );
      })}

      <div className="flex items-center gap-2">
        <button
          className="text-xs text-slate-200 hover:text-slate-50 px-3 py-1 border border-slate-700 rounded"
          onClick={() => setAdding(true)}
        >
          + Add pool
        </button>
        <button
          className="text-xs text-slate-400 hover:text-slate-200 px-3 py-1 border border-slate-800 rounded"
          title="Zero the in-memory active-slot counter on every pool. Use when a pool shows 0/N free but no worker is actually running (drift from a conductor crash or kill mid-session)."
          onClick={async () => {
            await ResetPoolCounters();
            await refresh();
          }}
        >
          Reset counters
        </button>
      </div>

      {(editing || adding) && (
        <PoolEditModal
          providers={providers}
          existingPools={pools.map((row) => row.pool)}
          initial={editing}
          onClose={() => {
            setEditing(null);
            setAdding(false);
          }}
          onSaved={async () => {
            setEditing(null);
            setAdding(false);
            await refresh();
          }}
        />
      )}
    </div>
  );
}

function SortablePoolRow({
  row,
  role,
  isFirst,
  isLast,
  providerByKind,
  deleteErr,
  onToggleEnabled,
  onEdit,
  onRemove,
}: {
  row: workerpool.PoolStatus;
  role: Role;
  isFirst: boolean;
  isLast: boolean;
  providerByKind: Map<string, main.ProviderInfo>;
  deleteErr?: string;
  onToggleEnabled: (p: types.Pool, next: boolean) => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: row.pool.id,
  });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };
  const dragHandle = (
    <span
      {...attributes}
      {...listeners}
      className="cursor-grab active:cursor-grabbing text-slate-600 hover:text-slate-400 select-none px-1"
      title="Drag to reorder preference"
    >
      ⠿
    </span>
  );
  const hint = isFirst ? "← preferred" : isLast ? "← fallback" : null;
  return (
    <div ref={setNodeRef} style={style}>
      <PoolRow
        row={row}
        role={role}
        providerByKind={providerByKind}
        deleteErr={deleteErr}
        dragHandle={dragHandle}
        preferenceHint={hint ?? undefined}
        onToggleEnabled={onToggleEnabled}
        onEdit={onEdit}
        onRemove={onRemove}
      />
    </div>
  );
}

function PoolRow({
  row,
  role,
  providerByKind,
  deleteErr,
  dragHandle,
  preferenceHint,
  onToggleEnabled,
  onEdit,
  onRemove,
}: {
  row: workerpool.PoolStatus;
  role: Role;
  providerByKind: Map<string, main.ProviderInfo>;
  deleteErr?: string;
  dragHandle: React.ReactNode;
  preferenceHint?: string;
  onToggleEnabled: (p: types.Pool, next: boolean) => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const info = providerByKind.get(row.pool.provider);
  return (
    <div className="border border-slate-800 rounded p-2 bg-slate-950/40">
      <div className="flex items-center gap-2">
        {dragHandle}
        <div className="flex-1 min-w-0">
          <div className="text-slate-100 font-mono text-xs flex items-center gap-2">
            <span>{row.pool.name}</span>
            <span className="text-slate-500">
              ({info?.display_name ?? row.pool.provider})
            </span>
            {preferenceHint && (
              <span className="text-slate-600 font-normal">{preferenceHint}</span>
            )}
          </div>
          <div className="text-slate-500 text-xs">
            {row.pool.model || "—"}
            {row.pool.endpoint && ` · ${row.pool.endpoint}`}
          </div>
        </div>
        {role !== "orchestrator" && (
          <div className="font-mono text-slate-300 text-xs">
            {row.active}/{row.pool.capacity}
          </div>
        )}
        <label className="flex items-center gap-1 text-xs">
          <input
            type="checkbox"
            checked={row.pool.enabled}
            onChange={(e) => onToggleEnabled(row.pool, e.target.checked)}
          />
          enabled
        </label>
        <button
          className="text-xs text-slate-300 hover:text-slate-100 px-2 py-1 border border-slate-700 rounded"
          onClick={onEdit}
        >
          Edit
        </button>
        <button
          className="text-xs text-rose-300 hover:text-rose-200 px-2 py-1 border border-rose-900 rounded"
          onClick={onRemove}
        >
          Remove
        </button>
      </div>
      {deleteErr && <div className="mt-1 text-xs text-rose-300">{deleteErr}</div>}
    </div>
  );
}
