import { create } from "zustand";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export type ActivityEntry = {
  tool_name: string;
  args_summary: string;
  args_json: string;
  at: string;
};

type StripState = {
  // Keyed by sessionID → ordered ring (oldest first, newest last, max 5).
  tails: Record<string, ActivityEntry[]>;
  // sessionID of the currently subscribed session (to detect switches).
  subscribedId: string | null;
  unsubscribe: (() => void) | null;

  subscribe: (sessionId: string | null) => void;
  getTail: (sessionId: string) => ActivityEntry[];
  _applyEvent: (sessionId: string, tail: ActivityEntry[]) => void;
  _reset: (sessionId: string) => void;
};

// Mirror the Wails session.activity payload shape (only the fields we need).
type ActivityPayload = {
  session_id: string;
  tail?: ActivityEntry[];
};

const RING_CAP = 5;

// Stable empty-array reference. `getTail` falling back to a fresh `[]` on
// every call would defeat zustand's Object.is selector comparison and
// cascade into an infinite render loop (React error #185) — every render
// reads getTail, returns a new [], triggers re-render, repeat. Reuse this
// singleton so absent-tail reads are referentially stable.
const EMPTY_TAIL: ActivityEntry[] = [];

export const useActivityStripStore = create<StripState>((set, get) => ({
  tails: {},
  subscribedId: null,
  unsubscribe: null,

  subscribe: (sessionId) => {
    const { unsubscribe, subscribedId } = get();
    if (subscribedId === sessionId) return;

    // Tear down previous listener.
    if (unsubscribe) {
      unsubscribe();
    }

    if (!sessionId) {
      set({ subscribedId: null, unsubscribe: null });
      return;
    }

    // EventsOn returns a cleanup function that deregisters the listener.
    const cancel = EventsOn("session.activity", (payload: ActivityPayload) => {
      if (payload?.session_id !== sessionId) return;
      if (payload.tail && payload.tail.length > 0) {
        get()._applyEvent(payload.session_id, payload.tail);
      }
    });

    set({ subscribedId: sessionId, unsubscribe: cancel });
  },

  getTail: (sessionId) => get().tails[sessionId] ?? EMPTY_TAIL,

  _applyEvent: (sessionId, tail) => {
    // Backend already sends a capped ring tail; just clamp to RING_CAP locally.
    const capped = tail.slice(-RING_CAP);
    set((s) => ({ tails: { ...s.tails, [sessionId]: capped } }));
  },

  _reset: (sessionId) => {
    set((s) => {
      const next = { ...s.tails };
      delete next[sessionId];
      return { tails: next };
    });
  },
}));
