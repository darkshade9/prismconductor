import { create } from "zustand";
import { types } from "../../wailsjs/go/models";

// Mirrors SessionActivity in Go. Wails only generates TS types for
// structs that appear in bound method signatures; activity rides through as
// an event payload, so declare it here.
export type SessionActivity = {
  session_id: string;
  workspace_id: string;
  issue_number: number;
  tool_count: number;
  last_action: string;
  last_action_at: string;
};

type SessionView = {
  id: string;
  meta?: types.Session;
  lines: string[]; // stored verbatim — including @live tail when streaming
  activity?: SessionActivity;
};

type State = {
  sessions: Record<string, SessionView>;
  activeId: string | null;
  appendLine: (id: string, line: string) => void;
  setMeta: (sess: types.Session) => void;
  setActivity: (act: SessionActivity) => void;
  loadTranscript: (id: string, body: string) => void;
  setActive: (id: string | null) => void;
  clear: (id: string) => void;
};

const LIVE_PREFIX = "@live ";

export const useSessionStore = create<State>((set) => ({
  sessions: {},
  activeId: null,
  appendLine: (id, line) =>
    set((s) => {
      const cur = s.sessions[id] ?? { id, lines: [] };
      let lines = cur.lines;
      // @live lines replace the previous @live tail (token-streaming).
      // Anything else: drop any pending @live tail (the assistant message
      // is committing) and append the new line.
      if (line.startsWith(LIVE_PREFIX)) {
        if (lines.length > 0 && lines[lines.length - 1].startsWith(LIVE_PREFIX)) {
          lines = [...lines.slice(0, -1), line];
        } else {
          lines = [...lines, line];
        }
      } else {
        if (lines.length > 0 && lines[lines.length - 1].startsWith(LIVE_PREFIX)) {
          lines = [...lines.slice(0, -1), line];
        } else {
          lines = [...lines, line];
        }
      }
      return {
        sessions: { ...s.sessions, [id]: { ...cur, lines } },
        activeId: s.activeId ?? id,
      };
    }),
  setMeta: (sess) =>
    set((s) => {
      const cur = s.sessions[sess.id] ?? { id: sess.id, lines: [] };
      return { sessions: { ...s.sessions, [sess.id]: { ...cur, meta: sess } } };
    }),
  setActivity: (act) =>
    set((s) => {
      const cur = s.sessions[act.session_id] ?? { id: act.session_id, lines: [] };
      return { sessions: { ...s.sessions, [act.session_id]: { ...cur, activity: act } } };
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
