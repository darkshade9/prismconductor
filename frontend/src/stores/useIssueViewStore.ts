import { create } from "zustand";
import { EventsOnWrapped } from "../lib/eventBus";
import { ListIssueViews } from "../../wailsjs/go/main/App";
import { issueview } from "../../wailsjs/go/models";

type State = {
  views: Record<string, issueview.IssueView>; // key: "wsID#number"
  loadForWorkspace: (workspaceID: string) => Promise<void>;
  get: (workspaceID: string, issueNumber: number) => issueview.IssueView | null;
};

export const useIssueViewStore = create<State>((set, get) => ({
  views: {},
  loadForWorkspace: async (workspaceID) => {
    if (!workspaceID) return;
    try {
      const list = await ListIssueViews(workspaceID);
      if (!list) return;
      set((s) => {
        const next = { ...s.views };
        for (const v of list) {
          next[`${v.issue.workspace_id}#${v.issue.number}`] = v;
        }
        return { views: next };
      });
    } catch {
      // Store unavailable on first render — incremental events will populate.
    }
  },
  get: (workspaceID, issueNumber) =>
    get().views[`${workspaceID}#${issueNumber}`] ?? null,
}));

// Incremental updates: subscribe once at module load so every card stays fresh.
EventsOnWrapped("bus.issue_view_updated", (raw: any) => {
  const view = issueview.IssueView.createFrom(raw);
  if (!view?.issue?.workspace_id || !view?.issue?.number) return;
  useIssueViewStore.setState((s) => ({
    views: {
      ...s.views,
      [`${view.issue.workspace_id}#${view.issue.number}`]: view,
    },
  }));
});
