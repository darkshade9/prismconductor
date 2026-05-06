import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock the Wails runtime before importing eventBus so EventsOn is controlled.
vi.mock("../../wailsjs/runtime/runtime", () => {
  let lastHandler: ((p: unknown) => void) | null = null;
  const EventsOn = vi.fn((_name: string, handler: (payload: unknown) => void) => {
    lastHandler = handler;
    return () => {};
  });
  (EventsOn as any).__trigger = (payload: unknown) => lastHandler?.(payload);
  return { EventsOn };
});

import { EventsOn } from "../../wailsjs/runtime/runtime";
import { EventsOnWrapped, debugEmit, getReceiveBuffer } from "./eventBus";

function triggerWails(payload: unknown) {
  (EventsOn as any).__trigger(payload);
}

beforeEach(() => {
  vi.clearAllMocks();
  // Drain the receive buffer between tests by emitting nothing — buffer is
  // module-level state so we clear it by slicing; use the splice trick.
  getReceiveBuffer(); // just read; buffer drains via module-level array in tests below
});

describe("EventsOnWrapped", () => {
  it("calls EventsOn and invokes the handler on receive", () => {
    const handler = vi.fn();
    EventsOnWrapped("test.event", handler);
    triggerWails({ x: 1 });
    expect(handler).toHaveBeenCalledWith({ x: 1 });
  });

  it("catches handler errors without propagating", () => {
    const bad = vi.fn(() => { throw new Error("boom"); });
    expect(() => {
      EventsOnWrapped("test.err", bad);
      triggerWails("payload");
    }).not.toThrow();
    expect(bad).toHaveBeenCalled();
  });

  it("records the event in the receive buffer", () => {
    EventsOnWrapped("test.buf", vi.fn());
    triggerWails({ key: "value" });
    const buf = getReceiveBuffer();
    const found = buf.find((r) => r.name === "test.buf");
    expect(found).toBeDefined();
    expect(found?.payload).toEqual({ key: "value" });
  });

  it("off function unregisters handler from registry", () => {
    const handler = vi.fn();
    const off = EventsOnWrapped("test.off", handler);
    off();
    debugEmit("test.off", "data");
    expect(handler).not.toHaveBeenCalled();
  });
});

describe("debugEmit", () => {
  it("invokes registered handlers without going through EventsOn", () => {
    const handler = vi.fn();
    EventsOnWrapped("test.debug", handler);
    debugEmit("test.debug", { key: "val" });
    expect(handler).toHaveBeenCalledWith({ key: "val" });
  });

  it("records emit in the receive buffer", () => {
    debugEmit("test.direct", 42);
    const buf = getReceiveBuffer();
    const found = buf.find((r) => r.name === "test.direct");
    expect(found).toBeDefined();
    expect(found?.payload).toBe(42);
  });
});
