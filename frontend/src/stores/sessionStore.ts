import { create } from "zustand";
import { types } from "../../wailsjs/go/models";

type SessionView = {
  id: string;
  meta?: types.Session;
  lines: string[];
};

type State = {
  sessions: Record<string, SessionView>;
  activeId: string | null;
  appendLine: (id: string, line: string) => void;
  setMeta: (sess: types.Session) => void;
  loadTranscript: (id: string, body: string) => void;
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
  setMeta: (sess) =>
    set((s) => {
      const cur = s.sessions[sess.id] ?? { id: sess.id, lines: [] };
      return { sessions: { ...s.sessions, [sess.id]: { ...cur, meta: sess } } };
    }),
  loadTranscript: (id, body) =>
    set((s) => {
      const lines = body.split(/\r?\n/);
      if (lines.length && lines[lines.length - 1] === "") lines.pop();
      const cur = s.sessions[id] ?? { id, lines: [] };
      return { sessions: { ...s.sessions, [id]: { ...cur, lines } } };
    }),
  setActive: (id) => set({ activeId: id }),
  clear: (id) =>
    set((s) => {
      const next = { ...s.sessions };
      delete next[id];
      return { sessions: next };
    }),
}));
