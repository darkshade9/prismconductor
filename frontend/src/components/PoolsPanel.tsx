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
  GetModelHint,
  GetPoolCostProjection,
  ListPools,
  ListProviderEntities,
  ListProviders,
  ListWorkspaces,
  SavePool,
  DeletePool,
  ResetPoolCounters,
  PoolSpendToday,
  PoolSpendThisWeek,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { main, llm, types, workerpool } from "../../wailsjs/go/models";
import { PoolEditModal } from "./PoolEditModal";
import { ProviderEditModal } from "./ProvidersPanel";

type Fit = "excellent" | "good" | "fair" | "poor" | "unsuitable" | "";

function fitForRole(hint: llm.ModelHint | null | undefined, role: string): Fit {
  if (!hint) return "";
  switch (role) {
    case "plan": return (hint.plan_fit as Fit) || "";
    case "work": return (hint.work_fit as Fit) || "";
    case "orchestrator": return (hint.orch_fit as Fit) || "";
    case "architect": return (hint.architect_fit as Fit) || "";
    default: return (hint.work_fit as Fit) || "";
  }
}

function CapabilityDot({
  hint,
  role,
}: {
  hint: llm.ModelHint | null | undefined;
  role: string;
}) {
  const fit = fitForRole(hint, role);

  if (!hint || fit === "") {
    return (
      <span
        className="inline-block w-2 h-2 rounded-full bg-slate-600"
        title="No capability data for this model"
      />
    );
  }

  const toolLabel =
    hint.tool_support === "full"
      ? "Tools: full"
      : hint.tool_support === "partial"
      ? "Tools: partial ⚠"
      : "Tools: none ✗";

  const tooltip = [
    `Fit for ${role}: ${fit}`,
    toolLabel,
    hint.context_window
      ? `Context: ${hint.context_window >= 1000000 ? `${hint.context_window / 1000000}M` : `${Math.round(hint.context_window / 1000)}k`} tokens`
      : null,
    hint.cost_tier ? `Cost: ${hint.cost_tier}` : null,
    hint.notes || null,
    `Source: ${hint.source || "bundled"}`,
  ]
    .filter(Boolean)
    .join("\n");

  const colorClass =
    fit === "excellent" || fit === "good"
      ? "bg-emerald-500"
      : fit === "fair"
      ? "bg-amber-400"
      : fit === "poor"
      ? "bg-orange-500"
      : "bg-rose-600";

  return (
    <span
      className={`inline-block w-2 h-2 rounded-full ${colorClass} shrink-0`}
      title={tooltip}
    />
  );
}

type Role = "plan" | "work" | "orchestrator" | "architect";

const ROLE_ORDER: Role[] = ["orchestrator", "plan", "work", "architect"];
const ROLE_LABEL: Record<Role, string> = {
  orchestrator: "Orchestrator",
  plan: "Plan pools",
  work: "Work pools",
  architect: "Architect pools",
};

export function PoolsPanel() {
  const [pools, setPools] = useState<workerpool.PoolStatus[]>([]);
  const [providers, setProviders] = useState<main.ProviderInfo[]>([]);
  const [providerEntities, setProviderEntities] = useState<types.ProviderEntity[]>([]);
  const [workspaces, setWorkspaces] = useState<types.Workspace[]>([]);
  const [editing, setEditing] = useState<types.Pool | null>(null);
  const [adding, setAdding] = useState(false);
  const [addingGeminiProvider, setAddingGeminiProvider] = useState(false);
  const [deleteErr, setDeleteErr] = useState<Record<string, string>>({});
  const [spendToday, setSpendToday] = useState<Record<string, number>>({});
  const [spendWeek, setSpendWeek] = useState<Record<string, number>>({});
  const [poolHints, setPoolHints] = useState<Record<string, llm.ModelHint | null>>({});
  const [projections, setProjections] = useState<Record<string, main.PoolCostProjection>>({});

  async function refresh() {
    try {
      const [ps, pv, ws, pe] = await Promise.all([ListPools(), ListProviders(), ListWorkspaces(), ListProviderEntities()]);
      setPools(ps ?? []);
      setProviders(pv ?? []);
      setWorkspaces(ws ?? []);
      setProviderEntities(pe ?? []);
      // Fetch spend for each pool in parallel.
      const ids = (ps ?? []).map((r) => r.pool.id);
      if (ids.length > 0) {
        const [todayResults, weekResults] = await Promise.all([
          Promise.all(ids.map((id) => PoolSpendToday(id).then((v) => [id, v] as const))),
          Promise.all(ids.map((id) => PoolSpendThisWeek(id).then((v) => [id, v] as const))),
        ]);
        setSpendToday(Object.fromEntries(todayResults));
        setSpendWeek(Object.fromEntries(weekResults));
      }
      // Fetch cost projections for each pool.
      if (ids.length > 0) {
        const projResults = await Promise.all(
          ids.map((id) =>
            GetPoolCostProjection(id)
              .then((p) => [id, p] as const)
              .catch(() => [id, null] as const),
          ),
        );
        const proj: Record<string, main.PoolCostProjection> = {};
        for (const [id, p] of projResults) {
          if (p) proj[id] = p;
        }
        setProjections(proj);
      }
      // Fetch model capability hints for every pool.
      if ((ps ?? []).length > 0) {
        const hintResults = await Promise.all(
          (ps ?? []).map((r) =>
            GetModelHint(r.pool.provider, r.pool.model)
              .then((h) => [r.pool.id, h ?? null] as const)
              .catch(() => [r.pool.id, null] as const),
          ),
        );
        setPoolHints(Object.fromEntries(hintResults));
      }
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
  const workspaceByID = new Map(workspaces.map((w) => [w.id, w]));

  function poolRole(p: types.Pool): Role {
    return ((p.role as Role) || "work") as Role;
  }

  const grouped: Record<Role, workerpool.PoolStatus[]> = {
    orchestrator: [],
    plan: [],
    work: [],
    architect: [],
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

      {pools.length === 0 && (() => {
        const hasGeminiEntity = providerEntities.some((e) => e.kind === "gemini");
        const geminiDriver = providers.find((p) => p.kind === "gemini");
        return (
          <>
            <div className="text-slate-500">No pools configured. Add one below.</div>
            {geminiDriver && !hasGeminiEntity && (
              <div className="rounded border border-blue-800 bg-blue-950/40 px-3 py-3 space-y-2">
                <div className="text-blue-200 text-sm font-medium">$0/mo starter — no credit card needed</div>
                <div className="text-blue-300 text-xs">
                  Google Gemini's free tier includes 1500 Flash requests/day and 50 Pro requests/day.
                  Add a Gemini provider to get a working plan + work pool for free.
                </div>
                <button
                  onClick={() => setAddingGeminiProvider(true)}
                  className="text-xs px-3 py-1 bg-blue-700 hover:bg-blue-600 text-white rounded"
                >
                  Add Gemini provider
                </button>
              </div>
            )}
          </>
        );
      })()}

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
                  {rows.map((row) => (
                    <SortablePoolRow
                      key={row.pool.id}
                      row={row}
                      role={role}
                      rolePeers={rows}
                      providerByKind={providerByKind}
                      workspaceByID={workspaceByID}
                      deleteErr={deleteErr[row.pool.id]}
                      spendToday={spendToday[row.pool.id] ?? 0}
                      spendWeek={spendWeek[row.pool.id] ?? 0}
                      projection={projections[row.pool.id] ?? null}
                      modelHint={poolHints[row.pool.id]}
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
                    workspaceByID={workspaceByID}
                    deleteErr={err}
                    dragHandle={null}
                    spendToday={spendToday[row.pool.id] ?? 0}
                    spendWeek={spendWeek[row.pool.id] ?? 0}
                    projection={projections[row.pool.id] ?? null}
                    modelHint={poolHints[row.pool.id]}
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

      {addingGeminiProvider && (
        <ProviderEditModal
          driverKinds={providers}
          initial={null}
          initialKind="gemini"
          onClose={() => setAddingGeminiProvider(false)}
          onSaved={async () => {
            setAddingGeminiProvider(false);
            await refresh();
          }}
        />
      )}
    </div>
  );
}

function preferenceState(
  row: workerpool.PoolStatus,
  rolePeers: workerpool.PoolStatus[],
  providerByKind: Map<string, main.ProviderInfo>,
): "preferred" | "shared" | "fallback" | null {
  const capable = rolePeers.filter(
    (r) => r.pool.enabled && r.pool.capacity > 0 && (providerByKind.get(r.pool.provider)?.can_spawn ?? false),
  );
  if (capable.length === 0) return null;
  if (!capable.some((r) => r.pool.id === row.pool.id)) return null;
  const sorted = [...capable].sort((a, b) => a.pool.priority - b.pool.priority);
  const lowest = sorted[0].pool.priority;
  const tiedAtLowest = sorted.filter((r) => r.pool.priority === lowest);
  if (row.pool.priority !== lowest) {
    if (row.pool.id === sorted[sorted.length - 1].pool.id) return "fallback";
    return null;
  }
  if (tiedAtLowest.length === 1) return "preferred";
  return "shared";
}

function SortablePoolRow({
  row,
  role,
  rolePeers,
  providerByKind,
  workspaceByID,
  deleteErr,
  spendToday,
  spendWeek,
  projection,
  modelHint,
  onToggleEnabled,
  onEdit,
  onRemove,
}: {
  row: workerpool.PoolStatus;
  role: Role;
  rolePeers: workerpool.PoolStatus[];
  providerByKind: Map<string, main.ProviderInfo>;
  workspaceByID: Map<string, types.Workspace>;
  deleteErr?: string;
  spendToday: number;
  spendWeek: number;
  projection: main.PoolCostProjection | null;
  modelHint?: llm.ModelHint | null;
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
  const state = preferenceState(row, rolePeers, providerByKind);
  let hint: React.ReactNode;
  if (state === "preferred") {
    hint = <span className="text-slate-600 font-normal">← preferred</span>;
  } else if (state === "shared") {
    hint = (
      <span
        className="text-amber-500 font-normal"
        title="Pools tied at the same priority share load via round-robin. Drag to assign distinct priorities."
      >
        ← shared (round-robin)
      </span>
    );
  } else if (state === "fallback") {
    hint = <span className="text-slate-600 font-normal">← fallback</span>;
  }
  return (
    <div ref={setNodeRef} style={style}>
      <PoolRow
        row={row}
        role={role}
        providerByKind={providerByKind}
        workspaceByID={workspaceByID}
        deleteErr={deleteErr}
        dragHandle={dragHandle}
        preferenceHint={hint}
        spendToday={spendToday}
        spendWeek={spendWeek}
        projection={projection}
        modelHint={modelHint}
        onToggleEnabled={onToggleEnabled}
        onEdit={onEdit}
        onRemove={onRemove}
      />
    </div>
  );
}

function poolScopeBadge(pool: types.Pool, workspaceByID: Map<string, types.Workspace>): React.ReactNode {
  if (!pool.scope || pool.scope === "shared") {
    return <span className="text-[10px] text-slate-600 font-normal">(shared)</span>;
  }
  const ws = pool.workspace_id ? workspaceByID.get(pool.workspace_id) : undefined;
  const label = ws ? (ws.display_name || ws.id) : (pool.workspace_id || "?");
  return (
    <span className="text-[10px] font-normal px-1 py-0.5 rounded bg-slate-800 text-slate-400">
      workspace: {label}
    </span>
  );
}

function projectionColorClass(projectedUSD: number, freeTier: boolean): string {
  if (freeTier) return "text-emerald-400";
  if (projectedUSD < 50) return "text-emerald-400";
  if (projectedUSD < 200) return "text-amber-400";
  return "text-rose-400";
}

function PoolRow({
  row,
  role,
  providerByKind,
  workspaceByID,
  deleteErr,
  dragHandle,
  preferenceHint,
  spendToday,
  spendWeek,
  projection,
  modelHint,
  onToggleEnabled,
  onEdit,
  onRemove,
}: {
  row: workerpool.PoolStatus;
  role: Role;
  providerByKind: Map<string, main.ProviderInfo>;
  workspaceByID: Map<string, types.Workspace>;
  deleteErr?: string;
  dragHandle: React.ReactNode;
  preferenceHint?: React.ReactNode;
  spendToday: number;
  spendWeek: number;
  projection: main.PoolCostProjection | null;
  modelHint?: llm.ModelHint | null;
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
            {poolScopeBadge(row.pool, workspaceByID)}
            {preferenceHint}
          </div>
          <div className="text-slate-500 text-xs flex items-center gap-1.5">
            <CapabilityDot hint={modelHint} role={role} />
            <span>{row.pool.model || "—"}</span>
            {row.pool.endpoint && <span>· {row.pool.endpoint}</span>}
          </div>
        </div>
        {projection && (
          <div
            className={`text-[10px] font-mono tabular-nums ${projectionColorClass(projection.projected_month_usd, projection.free_tier)}`}
            title={
              projection.free_tier
                ? "Free tier pool — no per-token billing"
                : `Last 7d: $${projection.last_7d_usd.toFixed(2)} · Last 30d: $${projection.last_30d_usd.toFixed(2)} · ${projection.sessions_30d} sessions`
            }
          >
            {projection.free_tier
              ? "$0/mo (free)"
              : projection.days_with_data === 0
              ? "no data"
              : `~$${projection.projected_month_usd.toFixed(0)}/mo`}
          </div>
        )}
        {role !== "orchestrator" && (
          <div className="font-mono text-slate-300 text-xs">
            {row.active}/{row.pool.capacity}
          </div>
        )}
        {(spendToday > 0 || spendWeek > 0) && (
          <div
            className="text-[10px] text-slate-500 font-mono tabular-nums"
            title={`Today: $${spendToday.toFixed(2)} · This week: $${spendWeek.toFixed(2)}`}
          >
            ${spendToday.toFixed(2)}/day
          </div>
        )}
        <button
          onClick={() => onToggleEnabled(row.pool, !row.pool.enabled)}
          className={
            "text-xs px-2 py-0.5 rounded-full border transition-colors " +
            (row.pool.enabled
              ? "bg-emerald-900/50 border-emerald-700 text-emerald-300 hover:bg-emerald-900"
              : "bg-slate-800 border-slate-700 text-slate-500 hover:text-slate-300")
          }
          title={row.pool.enabled ? "Click to disable" : "Click to enable"}
        >
          {row.pool.enabled ? "enabled" : "disabled"}
        </button>
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
