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
  cwd: string;
};

const HEIGHT_KEY = "prismconductor.agentterm.height";
const DEFAULT_HEIGHT = 288;

function loadHeight(): number {
  try {
    const raw = localStorage.getItem(HEIGHT_KEY);
    if (raw) {
      const n = parseInt(raw, 10);
      if (!isNaN(n) && n >= 120) return n;
    }
  } catch {
    // ignore
  }
  return DEFAULT_HEIGHT;
}

function saveHeight(h: number): void {
  try {
    localStorage.setItem(HEIGHT_KEY, String(h));
  } catch {
    // ignore
  }
}

type State = {
  drawerOpen: boolean;
  sessions: Record<string, AgentTermSession | null>; // workspaceID → session
  availableAgents: AgentInfo[];
  height: number;
  expandedHeight: number;
  collapsed: boolean;

  setDrawerOpen: (open: boolean) => void;
  toggleDrawer: () => void;
  setSession: (workspaceID: string, sess: AgentTermSession | null) => void;
  clearSession: (workspaceID: string) => void;
  setAvailableAgents: (agents: AgentInfo[]) => void;
  setHeight: (h: number) => void;
  setCollapsed: (c: boolean) => void;
};

const initialHeight = loadHeight();

export const useAgentTerminalStore = create<State>((set, get) => ({
  drawerOpen: false,
  sessions: {},
  availableAgents: [],
  height: initialHeight,
  expandedHeight: initialHeight,
  collapsed: false,

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

  setHeight: (h) => {
    saveHeight(h);
    set({ height: h, expandedHeight: h });
  },

  setCollapsed: (c) => {
    if (c) {
      const { height } = get();
      set({ collapsed: true, expandedHeight: height });
    } else {
      const { expandedHeight } = get();
      const h = expandedHeight >= 120 ? expandedHeight : DEFAULT_HEIGHT;
      saveHeight(h);
      set({ collapsed: false, height: h });
    }
  },
}));
