import { useEffect, useState } from "react";
import {
  ListPools,
  ListProviders,
  SavePool,
  DeletePool,
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
    return () => {
      if (typeof offFreed === "function") offFreed();
      if (typeof offChanged === "function") offChanged();
      if (typeof offState === "function") offState();
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
        return (
          <div key={role} className="space-y-2">
            <div className="text-xs uppercase tracking-wide text-slate-500">
              {ROLE_LABEL[role]} ({rows.length})
            </div>
            {rows.map((row) => {
              const info = providerByKind.get(row.pool.provider);
              const err = deleteErr[row.pool.id];
              return (
                <div
                  key={row.pool.id}
                  className="border border-slate-800 rounded p-2 bg-slate-950/40"
                >
                  <div className="flex items-center gap-2">
                    <div className="flex-1">
                      <div className="text-slate-100 font-mono text-xs flex items-center gap-2">
                        <span>{row.pool.name}</span>
                        <span className="text-slate-500">
                          ({info?.display_name ?? row.pool.provider})
                        </span>
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
                        onChange={(e) =>
                          onToggleEnabled(row.pool, e.target.checked)
                        }
                      />
                      enabled
                    </label>
                    <button
                      className="text-xs text-slate-300 hover:text-slate-100 px-2 py-1 border border-slate-700 rounded"
                      onClick={() => setEditing(row.pool)}
                    >
                      Edit
                    </button>
                    <button
                      className="text-xs text-rose-300 hover:text-rose-200 px-2 py-1 border border-rose-900 rounded"
                      onClick={() => onRemove(row.pool)}
                    >
                      Remove
                    </button>
                  </div>
                  {err && (
                    <div className="mt-1 text-xs text-rose-300">{err}</div>
                  )}
                </div>
              );
            })}
          </div>
        );
      })}

      <button
        className="text-xs text-slate-200 hover:text-slate-50 px-3 py-1 border border-slate-700 rounded"
        onClick={() => setAdding(true)}
      >
        + Add pool
      </button>

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
