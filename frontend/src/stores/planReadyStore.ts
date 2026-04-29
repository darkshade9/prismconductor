import { create } from "zustand";

export type PlanReadyEntry = {
  workspace_id: string;
  issue_number: number;
  revision: number;
};

type State = {
  ready: Map<string, PlanReadyEntry>;
  markReady: (e: PlanReadyEntry) => void;
  clearReady: (workspaceID: string, issueNumber: number) => void;
  isReady: (workspaceID: string, issueNumber: number) => PlanReadyEntry | null;
};

const key = (ws: string, n: number) => `${ws}#${n}`;

export const usePlanReadyStore = create<State>((set, get) => ({
  ready: new Map(),
  markReady: (e) =>
    set((s) => {
      const next = new Map(s.ready);
      next.set(key(e.workspace_id, e.issue_number), e);
      return { ready: next };
    }),
  clearReady: (ws, n) =>
    set((s) => {
      const next = new Map(s.ready);
      next.delete(key(ws, n));
      return { ready: next };
    }),
  isReady: (ws, n) => get().ready.get(key(ws, n)) ?? null,
}));
