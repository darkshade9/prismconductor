import { create } from "zustand";
import { ListPools } from "../../wailsjs/go/main/App";

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
