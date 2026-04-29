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
      const ws = await ListWorkspaces();
      set({ workspaces: ws ?? [], loading: false });
    } catch {
      set({ loading: false });
    }
  },
  setSelected: (id) => set({ selectedID: id }),
}));
