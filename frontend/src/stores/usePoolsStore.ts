import { create } from "zustand";
import { ListPools } from "../../wailsjs/go/main/App";
import { workerpool } from "../../wailsjs/go/models";

export type PoolEntry = {
  id: string;
  name: string;
  provider: string;
};

type State = {
  pools: Record<string, PoolEntry>;
  refresh: () => Promise<void>;
};

export const usePoolsStore = create<State>((set) => ({
  pools: {},
  refresh: async () => {
    try {
      const rows = await ListPools();
      const next: Record<string, PoolEntry> = {};
      for (const r of rows ?? []) {
        if (r.pool?.id) {
          next[r.pool.id] = {
            id: r.pool.id,
            name: r.pool.name,
            provider: r.pool.provider,
          };
        }
      }
      set({ pools: next });
    } catch {
      // silently ignore — pools badge degrades to missing (shows nothing)
    }
  },
}));

export type PoolBudget = {
  name: string;
  max_input_tokens?: number;
  max_turns?: number;
};

export type ProviderSummary = {
  kind: string;
  active: number;
  capacity: number;
  poolNames: string[];
  budgets: PoolBudget[];
};

export type RoleSummary = {
  role: "orchestrator" | "plan" | "work";
  label: string;
  providers: ProviderSummary[];
};

type Role = "orchestrator" | "plan" | "work";

const ROLE_LABEL: Record<Role, string> = {
  orchestrator: "Orchestrator",
  plan: "Planners",
  work: "Workers",
};

const ROLE_ORDER: Role[] = ["orchestrator", "plan", "work"];

export function getPoolSummaryByRole(pools: workerpool.PoolStatus[]): RoleSummary[] {
  const byRole: Record<Role, Map<string, ProviderSummary>> = {
    orchestrator: new Map(),
    plan: new Map(),
    work: new Map(),
  };

  for (const row of pools) {
    if (!row.pool?.enabled) continue;
    const role = ((row.pool.role as Role) || "work") satisfies Role;
    if (!(role in byRole)) continue;
    const kind = row.pool.provider || "generic";
    const map = byRole[role];
    const budget: PoolBudget = {
      name: row.pool.name,
      max_input_tokens: row.pool.max_input_tokens,
      max_turns: row.pool.max_turns,
    };
    const existing = map.get(kind);
    if (existing) {
      existing.active += row.active ?? 0;
      existing.capacity += row.pool.capacity ?? 0;
      existing.poolNames.push(row.pool.name);
      existing.budgets.push(budget);
    } else {
      map.set(kind, {
        kind,
        active: row.active ?? 0,
        capacity: row.pool.capacity ?? 0,
        poolNames: [row.pool.name],
        budgets: [budget],
      });
    }
  }

  return ROLE_ORDER
    .map((role) => ({
      role,
      label: ROLE_LABEL[role],
      providers: [...byRole[role].values()].sort((a, b) =>
        a.kind.localeCompare(b.kind)
      ),
    }))
    .filter((r) => r.providers.length > 0);
}
