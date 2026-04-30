import { create } from "zustand";

export type ToastLevel = "info" | "warning" | "error" | "success";
export type ToastAction = "open_plan" | "open_pr" | "focus_card" | "none";

export type Toast = {
  id: string;
  level: ToastLevel;
  title: string;
  body: string;
  workspace_id: string;
  issue_number: number;
  pr_url?: string;
  action: ToastAction;
};

const MAX_VISIBLE = 3;

type State = {
  recent: Toast[];
  push: (t: Toast) => void;
  dismiss: (id: string) => void;
};

export const useToastStore = create<State>((set) => ({
  recent: [],
  push: (t) =>
    set((s) => {
      const next = [...s.recent, t];
      while (next.length > MAX_VISIBLE) next.shift();
      return { recent: next };
    }),
  dismiss: (id) =>
    set((s) => ({ recent: s.recent.filter((t) => t.id !== id) })),
}));
