import { create } from "zustand";
import { ListWorkspaces } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

// Persist the chip-selection across app launches so reopening the app
// drops the user back into the workspace they were last working in. The
// sentinel string "__ALL__" distinguishes "user explicitly chose All" from
// "nothing has ever been picked" (null) — the latter triggers the
// auto-select-when-single-workspace fallback below; the former does not.
const STORAGE_KEY = "prismconductor.workspaceSelected";
const ALL_SENTINEL = "__ALL__";

function loadPersisted(): string | null | undefined {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return undefined; // never set: first launch
    if (raw === ALL_SENTINEL) return null; // explicit All
    return raw;
  } catch {
    return undefined;
  }
}

function persist(id: string | null): void {
  try {
    localStorage.setItem(STORAGE_KEY, id ?? ALL_SENTINEL);
  } catch {
    // localStorage unavailable (private browsing, quota): chip still works
    // for the duration of the session, just won't survive restart.
  }
}

const initial = loadPersisted();

type State = {
  workspaces: types.Workspace[];
  selectedID: string | null;
  loading: boolean;
  refresh: () => Promise<void>;
  setSelected: (id: string | null) => void;
};

export const useWorkspaceStore = create<State>((set) => ({
  workspaces: [],
  // initial === undefined (never persisted) → null = "nothing chosen yet"
  // initial === null (persisted as ALL) → null = "user picked All"
  // initial === "ws-id" → the previously-selected workspace
  selectedID: initial === undefined ? null : initial,
  loading: false,
  refresh: async () => {
    set({ loading: true });
    try {
      const ws = (await ListWorkspaces()) ?? [];
      set((s) => {
        // Auto-select when there's exactly one workspace AND no prior
        // explicit choice (initial === undefined). After the user has
        // ever clicked a chip — including All — keep that choice.
        const next =
          s.selectedID !== null
            ? s.selectedID
            : initial === undefined && ws.length === 1
            ? ws[0].id
            : null;
        // If the persisted ID points at a workspace that no longer exists
        // (e.g. user deleted it from another machine), fall back to All
        // rather than leaving the chip on a phantom workspace that filters
        // out every card.
        const valid = next === null || ws.some((w) => w.id === next);
        return {
          workspaces: ws,
          loading: false,
          selectedID: valid ? next : null,
        };
      });
    } catch {
      set({ loading: false });
    }
  },
  setSelected: (id) => {
    persist(id);
    set({ selectedID: id });
  },
}));
