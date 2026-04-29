import { create } from "zustand";
import { ListWorkspaces } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

type State = {
  workspaces: types.Workspace[];
  selectedID: string | null;
  loading: boolean;
  refresh: () => Promise<void>;
  setSelected: (id: string | null) => void;
};

export const useWorkspaceStore = create<State>((set) => ({
  workspaces: [],
  selectedID: null,
  loading: false,
  refresh: async () => {
    set({ loading: true });
    try {
      const ws = (await ListWorkspaces()) ?? [];
      set((s) => ({
        workspaces: ws,
        loading: false,
        // Auto-select when there's exactly one workspace and nothing's been
        // picked yet — saves the user from having to click the chip.
        selectedID: s.selectedID ?? (ws.length === 1 ? ws[0].id : null),
      }));
    } catch {
      set({ loading: false });
    }
  },
  setSelected: (id) => set({ selectedID: id }),
}));
