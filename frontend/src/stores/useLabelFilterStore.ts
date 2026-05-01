import { create } from "zustand";
import { GetLabelFilter, SetLabelFilter } from "../../wailsjs/go/main/App";

export type LabelFilterMode = "and" | "or";

type State = {
  selected: string[];
  mode: LabelFilterMode;
  toggle: (label: string) => void;
  clear: () => void;
  setMode: (mode: LabelFilterMode) => void;
  loadForWorkspace: (workspaceID: string) => Promise<void>;
};

let debounceTimer: ReturnType<typeof setTimeout> | null = null;

function persist(workspaceID: string, selected: string[], mode: LabelFilterMode) {
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => {
    SetLabelFilter(workspaceID, selected, mode).catch(() => {});
  }, 250);
}

export const useLabelFilterStore = create<State>((set, get) => ({
  selected: [],
  mode: "or",

  loadForWorkspace: async (workspaceID) => {
    if (!workspaceID) {
      set({ selected: [], mode: "or" });
      return;
    }
    try {
      const state = await GetLabelFilter(workspaceID);
      set({
        selected: state?.labels ?? [],
        mode: (state?.mode as LabelFilterMode) ?? "or",
      });
    } catch {
      set({ selected: [], mode: "or" });
    }
  },

  toggle: (label) => {
    set((s) => {
      const next = s.selected.includes(label)
        ? s.selected.filter((l) => l !== label)
        : [...s.selected, label];
      return { selected: next };
    });
  },

  setMode: (mode) => {
    set({ mode });
  },

  clear: () => {
    set({ selected: [], mode: "or" });
  },
}));

// Persist changes after every mutation. Callers that mutate call persist() via
// the action wrappers below so the store itself stays framework-agnostic.
export function toggleLabel(workspaceID: string, label: string) {
  useLabelFilterStore.getState().toggle(label);
  const { selected, mode } = useLabelFilterStore.getState();
  persist(workspaceID, selected, mode);
}

export function setFilterMode(workspaceID: string, mode: LabelFilterMode) {
  useLabelFilterStore.getState().setMode(mode);
  const { selected } = useLabelFilterStore.getState();
  persist(workspaceID, selected, mode);
}

export function clearFilter(workspaceID: string) {
  useLabelFilterStore.getState().clear();
  persist(workspaceID, [], "or");
}
