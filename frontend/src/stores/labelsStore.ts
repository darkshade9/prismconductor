import { create } from "zustand";
import { ListLabels } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

type State = {
  byWorkspace: Record<string, types.Label[]>;
  loading: Record<string, boolean>;
  refresh: (workspaceID: string) => Promise<void>;
  list: (workspaceID: string) => types.Label[];
  byName: (workspaceID: string, name: string) => types.Label | undefined;
};

const inFlight = new Map<string, Promise<void>>();

export const useLabelsStore = create<State>((set, get) => ({
  byWorkspace: {},
  loading: {},
  refresh: async (workspaceID) => {
    if (!workspaceID) return;
    // Dedup parallel callers (e.g. many cards mounting at once).
    const existing = inFlight.get(workspaceID);
    if (existing) return existing;
    set((s) => ({ loading: { ...s.loading, [workspaceID]: true } }));
    const p = (async () => {
      try {
        const got = (await ListLabels(workspaceID)) ?? [];
        set((s) => ({
          byWorkspace: { ...s.byWorkspace, [workspaceID]: got },
          loading: { ...s.loading, [workspaceID]: false },
        }));
      } catch {
        set((s) => ({ loading: { ...s.loading, [workspaceID]: false } }));
      } finally {
        inFlight.delete(workspaceID);
      }
    })();
    inFlight.set(workspaceID, p);
    return p;
  },
  list: (workspaceID) => get().byWorkspace[workspaceID] ?? [],
  byName: (workspaceID, name) => (get().byWorkspace[workspaceID] ?? []).find((l) => l.name === name),
}));
