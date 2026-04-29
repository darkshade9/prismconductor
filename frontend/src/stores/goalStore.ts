import { create } from "zustand";
import {
  ActivateGoal,
  DeleteGoal,
  ListGoals,
  SaveGoal,
  SetGoalStatus,
} from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

type State = {
  goals: types.Goal[];
  loading: boolean;
  refresh: () => Promise<void>;
  save: (g: types.Goal) => Promise<void>;
  remove: (id: string) => Promise<void>;
  activate: (id: string) => Promise<void>;
  setStatus: (id: string, status: string) => Promise<void>;
  active: () => types.Goal | null;
  upNext: () => types.Goal[];
  past: () => types.Goal[];
};

export const useGoalStore = create<State>((set, get) => ({
  goals: [],
  loading: false,
  refresh: async () => {
    set({ loading: true });
    try {
      const gs = await ListGoals();
      set({ goals: gs ?? [], loading: false });
    } catch {
      set({ loading: false });
    }
  },
  save: async (g) => {
    await SaveGoal(g);
    await get().refresh();
  },
  remove: async (id) => {
    await DeleteGoal(id);
    await get().refresh();
  },
  activate: async (id) => {
    await ActivateGoal(id);
    await get().refresh();
  },
  setStatus: async (id, status) => {
    await SetGoalStatus(id, status);
    await get().refresh();
  },
  active: () => get().goals.find((g) => g.status === "active") ?? null,
  upNext: () =>
    get()
      .goals.filter((g) => g.status === "backlog")
      .sort((a, b) => a.order - b.order),
  past: () =>
    get().goals.filter((g) => g.status === "achieved" || g.status === "abandoned"),
}));
