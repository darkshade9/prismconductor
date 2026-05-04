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
  /** When true the toast will not auto-dismiss — caller must dismiss() it explicitly. */
  persist?: boolean;
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
      // If a toast with the same id already exists replace it in place so
      // persistent "fetching…" toasts update without position churn.
      const idx = s.recent.findIndex((x) => x.id === t.id);
      if (idx >= 0) {
        const next = [...s.recent];
        next[idx] = t;
        return { recent: next };
      }
      const next = [...s.recent, t];
      while (next.length > MAX_VISIBLE) next.shift();
      return { recent: next };
    }),
  dismiss: (id) =>
    set((s) => ({ recent: s.recent.filter((t) => t.id !== id) })),
}));
