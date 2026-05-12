import { create } from "zustand";

const DIAGNOSTICS_KEY = "prismconductor.showDiagnostics";
const RECONCILER_KEY = "prismconductor.reconcilerEnabled";

function loadShowDiagnostics(): boolean {
  try {
    return localStorage.getItem(DIAGNOSTICS_KEY) === "true";
  } catch {
    return false;
  }
}

function loadReconcilerEnabled(): boolean {
  try {
    const v = localStorage.getItem(RECONCILER_KEY);
    return v === null ? true : v === "true";
  } catch {
    return true;
  }
}

type State = {
  showDiagnostics: boolean;
  diagnosticsUnlocked: boolean;
  reconcilerEnabled: boolean;
  setShowDiagnostics: (val: boolean) => void;
  setDiagnosticsUnlocked: (val: boolean) => void;
  setReconcilerEnabled: (val: boolean) => void;
};

export const useSettingsStore = create<State>((set) => ({
  showDiagnostics: loadShowDiagnostics(),
  diagnosticsUnlocked: false,
  reconcilerEnabled: loadReconcilerEnabled(),
  setShowDiagnostics: (val) => {
    try {
      localStorage.setItem(DIAGNOSTICS_KEY, String(val));
    } catch {
      // ignore
    }
    set({ showDiagnostics: val });
  },
  setDiagnosticsUnlocked: (val) => set({ diagnosticsUnlocked: val }),
  setReconcilerEnabled: (val) => {
    try {
      localStorage.setItem(RECONCILER_KEY, String(val));
    } catch {
      // ignore
    }
    set({ reconcilerEnabled: val });
  },
}));
