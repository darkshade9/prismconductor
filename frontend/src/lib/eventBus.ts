import { EventsOn } from "../../wailsjs/runtime/runtime";

export type EventRecord = {
  name: string;
  payload: unknown;
  receivedAt: number;
};

const BUF_CAP = 500;

// Receive-side ring buffer — updated by every EventsOnWrapped handler.
const receiveBuffer: EventRecord[] = [];

// Handler registry for debugEmit (dev replay and e2e synthetic events).
const registry = new Map<string, Set<(payload: unknown) => void>>();

function pushReceive(name: string, payload: unknown) {
  receiveBuffer.unshift({ name, payload, receivedAt: Date.now() });
  if (receiveBuffer.length > BUF_CAP) receiveBuffer.pop();
}

/**
 * Drop-in replacement for EventsOn that records each receive in a ring buffer
 * and wraps handlers with try/catch so one bad handler can't break the others.
 * Returns the same off-function as EventsOn.
 */
export function EventsOnWrapped<T>(
  name: string,
  handler: (payload: T) => void,
): () => void {
  const wrapped = (payload: T) => {
    pushReceive(name, payload);
    try {
      handler(payload);
    } catch (e) {
      console.error(`[eventBus] handler error for "${name}":`, e);
    }
  };

  if (!registry.has(name)) registry.set(name, new Set());
  registry.get(name)!.add(handler as (payload: unknown) => void);

  const off = EventsOn(name, wrapped as (payload: unknown) => void);

  return () => {
    registry.get(name)?.delete(handler as (payload: unknown) => void);
    if (typeof off === "function") off();
  };
}

/**
 * Synthetically invoke all registered handlers for an event without going
 * through the Wails IPC channel. Used by the EventDiagnostics replay button
 * and by Playwright e2e tests via window.__pcEventBuffer.emit.
 * Also exported directly so unit tests can call it without a browser global.
 */
export function debugEmit(name: string, payload: unknown) {
  pushReceive(name, payload);
  const handlers = registry.get(name);
  if (!handlers) return;
  for (const h of handlers) {
    try {
      h(payload);
    } catch (e) {
      console.error(`[eventBus] debugEmit handler error for "${name}":`, e);
    }
  }
}

/** Returns a snapshot of the receive buffer. Exported for unit tests. */
export function getReceiveBuffer(): EventRecord[] {
  return receiveBuffer.slice();
}

declare global {
  interface Window {
    __pcEventBuffer: {
      readonly receives: EventRecord[];
      emit: (name: string, payload: unknown) => void;
    };
  }
}

// Expose on window for Playwright e2e tests. Guard for Node.js test environments.
if (typeof window !== "undefined") {
  window.__pcEventBuffer = {
    get receives() {
      return receiveBuffer.slice();
    },
    emit: debugEmit,
  };
}
