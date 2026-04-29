import { create } from "zustand";
import {
  ListIssues,
  MoveIssueColumn,
  RemoveIssue,
  ReorderIssues,
} from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

export type Column = "todo" | "plan" | "in_progress" | "review" | "done";

type State = {
  issues: types.Issue[];
  loading: boolean;
  refresh: (workspaceID?: string) => Promise<void>;
  moveColumn: (workspaceID: string, number: number, column: Column) => Promise<void>;
  reorder: (workspaceID: string, column: Column, ordered: number[]) => Promise<void>;
  remove: (workspaceID: string, number: number) => Promise<void>;
  // Optimistic local update before backend confirms — keeps drag UX smooth.
  applyLocalMove: (workspaceID: string, number: number, column: Column) => void;
  applyLocalReorder: (workspaceID: string, column: Column, ordered: number[]) => void;
};

export const useIssueStore = create<State>((set, get) => ({
  issues: [],
  loading: false,
  refresh: async (workspaceID) => {
    set({ loading: true });
    try {
      const all = await ListIssues(workspaceID ?? "");
      set({ issues: all ?? [], loading: false });
    } catch {
      set({ loading: false });
    }
  },
  moveColumn: async (workspaceID, number, column) => {
    // eslint-disable-next-line no-console
    console.debug(`[issueStore] moveColumn ws=${workspaceID} #${number} → ${column}`);
    get().applyLocalMove(workspaceID, number, column);
    try {
      await MoveIssueColumn(workspaceID, number, column);
    } catch (e) {
      // eslint-disable-next-line no-console
      console.error("[issueStore] MoveIssueColumn failed", { workspaceID, number, column }, e);
    }
  },
  reorder: async (workspaceID, column, ordered) => {
    get().applyLocalReorder(workspaceID, column, ordered);
    await ReorderIssues(workspaceID, column, ordered);
  },
  remove: async (workspaceID, number) => {
    await RemoveIssue(workspaceID, number);
    set((s) => ({
      issues: s.issues.filter((i) => !(i.workspace_id === workspaceID && i.number === number)),
    }));
  },
  applyLocalMove: (workspaceID, number, column) =>
    set((s) => ({
      issues: s.issues.map((i) =>
        i.workspace_id === workspaceID && i.number === number
          ? Object.assign(Object.create(Object.getPrototypeOf(i)), i, { column }) as types.Issue
          : i,
      ),
    })),
  applyLocalReorder: (workspaceID, column, ordered) =>
    set((s) => {
      // Build an order map; everything not listed keeps its prior position.
      const orderIdx = new Map<number, number>();
      ordered.forEach((n, i) => orderIdx.set(n, i));
      const inCol = s.issues
        .filter((i) => i.workspace_id === workspaceID && i.column === column)
        .sort((a, b) => (orderIdx.get(a.number) ?? Infinity) - (orderIdx.get(b.number) ?? Infinity));
      const others = s.issues.filter((i) => !(i.workspace_id === workspaceID && i.column === column));
      return { issues: [...others, ...inCol] };
    }),
}));
