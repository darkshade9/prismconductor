import { create } from "zustand";

type Session = {
  id: string;
  lines: string[];
};

type State = {
  sessions: Record<string, Session>;
  activeId: string | null;
  appendLine: (id: string, line: string) => void;
  setActive: (id: string | null) => void;
  clear: (id: string) => void;
};

export const useSessionStore = create<State>((set) => ({
  sessions: {},
  activeId: null,
  appendLine: (id, line) =>
    set((s) => {
      const cur = s.sessions[id] ?? { id, lines: [] };
      return {
        sessions: { ...s.sessions, [id]: { ...cur, lines: [...cur.lines, line] } },
        activeId: s.activeId ?? id,
      };
    }),
  setActive: (id) => set({ activeId: id }),
  clear: (id) =>
    set((s) => {
      const next = { ...s.sessions };
      delete next[id];
      return { sessions: next };
    }),
}));
