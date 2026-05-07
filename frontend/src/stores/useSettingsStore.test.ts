import { describe, it, expect, beforeEach, vi } from "vitest";

// Stub localStorage before importing the store.
const storage: Record<string, string> = {};
vi.stubGlobal("localStorage", {
  getItem: (k: string) => storage[k] ?? null,
  setItem: (k: string, v: string) => { storage[k] = v; },
  removeItem: (k: string) => { delete storage[k]; },
});

import { useSettingsStore } from "./useSettingsStore";

const DIAGNOSTICS_KEY = "prismconductor.showDiagnostics";

beforeEach(() => {
  for (const k of Object.keys(storage)) delete storage[k];
  useSettingsStore.setState({ showDiagnostics: false, diagnosticsUnlocked: false });
});

describe("useSettingsStore", () => {
  it("defaults to showDiagnostics=false and diagnosticsUnlocked=false", () => {
    const { showDiagnostics, diagnosticsUnlocked } = useSettingsStore.getState();
    expect(showDiagnostics).toBe(false);
    expect(diagnosticsUnlocked).toBe(false);
  });

  it("setShowDiagnostics persists to localStorage", () => {
    useSettingsStore.getState().setShowDiagnostics(true);
    expect(useSettingsStore.getState().showDiagnostics).toBe(true);
    expect(storage[DIAGNOSTICS_KEY]).toBe("true");
  });

  it("setShowDiagnostics false clears persisted value", () => {
    storage[DIAGNOSTICS_KEY] = "true";
    useSettingsStore.getState().setShowDiagnostics(false);
    expect(useSettingsStore.getState().showDiagnostics).toBe(false);
    expect(storage[DIAGNOSTICS_KEY]).toBe("false");
  });

  it("setDiagnosticsUnlocked sets session flag without persisting", () => {
    useSettingsStore.getState().setDiagnosticsUnlocked(true);
    expect(useSettingsStore.getState().diagnosticsUnlocked).toBe(true);
    expect(storage[DIAGNOSTICS_KEY]).toBeUndefined();
  });

  it("setDiagnosticsUnlocked can be reset to false", () => {
    useSettingsStore.setState({ diagnosticsUnlocked: true });
    useSettingsStore.getState().setDiagnosticsUnlocked(false);
    expect(useSettingsStore.getState().diagnosticsUnlocked).toBe(false);
  });
});
