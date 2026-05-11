import { create } from "zustand";

const DIAGNOSTICS_KEY = "prismconductor.showDiagnostics";

function loadShowDiagnostics(): boolean {
  try {
    return localStorage.getItem(DIAGNOSTICS_KEY) === "true";
  } catch {
    return false;
  }
}

type State = {
  showDiagnostics: boolean;
  diagnosticsUnlocked: boolean;
  setShowDiagnostics: (val: boolean) => void;
  setDiagnosticsUnlocked: (val: boolean) => void;
};

export const useSettingsStore = create<State>((set) => ({
  showDiagnostics: loadShowDiagnostics(),
  diagnosticsUnlocked: false,
  setShowDiagnostics: (val) => {
    try {
      localStorage.setItem(DIAGNOSTICS_KEY, String(val));
    } catch {
      // ignore
    }
    set({ showDiagnostics: val });
  },
  setDiagnosticsUnlocked: (val) => set({ diagnosticsUnlocked: val }),
}));
