import { create } from "zustand";
import { ListLabels } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

// Shared empty array used by selectors so cache misses keep returning the
// SAME reference. Returning a fresh `?? []` each call would defeat Zustand's
// Object.is equality and trigger an infinite render loop (React error #185).
// Typed as the mutable Label[] so consumers don't fight a readonly cast; in
// practice the array is empty and never written to.
export const EMPTY_LABELS: types.Label[] = [];

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
  list: (workspaceID) => get().byWorkspace[workspaceID] ?? EMPTY_LABELS,
  byName: (workspaceID, name) => (get().byWorkspace[workspaceID] ?? EMPTY_LABELS).find((l) => l.name === name),
}));
