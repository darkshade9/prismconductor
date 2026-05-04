import { create } from "zustand";

export type AgentInfo = {
  name: string;
  binary: string;
};

export type AgentTermSession = {
  workspace_id: string;
  session_id: string;
  agent_bin: string;
  pid: number;
};

type State = {
  drawerOpen: boolean;
  sessions: Record<string, AgentTermSession | null>; // workspaceID → session
  availableAgents: AgentInfo[];

  setDrawerOpen: (open: boolean) => void;
  toggleDrawer: () => void;
  setSession: (workspaceID: string, sess: AgentTermSession | null) => void;
  clearSession: (workspaceID: string) => void;
  setAvailableAgents: (agents: AgentInfo[]) => void;
};

export const useAgentTerminalStore = create<State>((set) => ({
  drawerOpen: false,
  sessions: {},
  availableAgents: [],

  setDrawerOpen: (open) => set({ drawerOpen: open }),
  toggleDrawer: () => set((s) => ({ drawerOpen: !s.drawerOpen })),
  setSession: (workspaceID, sess) =>
    set((s) => ({ sessions: { ...s.sessions, [workspaceID]: sess } })),
  clearSession: (workspaceID) =>
    set((s) => {
      const next = { ...s.sessions };
      delete next[workspaceID];
      return { sessions: next };
    }),
  setAvailableAgents: (agents) => set({ availableAgents: agents }),
}));
