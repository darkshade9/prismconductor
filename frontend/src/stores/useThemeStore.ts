import { create } from "zustand";

export type ThemeMode = "light" | "dark" | "system";
export type EffectiveTheme = "light" | "dark";

const STORAGE_KEY = "prismconductor.theme";

function getSystemTheme(): EffectiveTheme {
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function computeEffective(mode: ThemeMode): EffectiveTheme {
  if (mode === "system") return getSystemTheme();
  return mode;
}

function applyTheme(effective: EffectiveTheme) {
  if (typeof document === "undefined") return;
  if (effective === "dark") {
    document.documentElement.classList.add("dark");
  } else {
    document.documentElement.classList.remove("dark");
  }
}

function loadMode(): ThemeMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === "light" || raw === "dark" || raw === "system") return raw;
  } catch {
    // ignore
  }
  return "dark";
}

const initialMode = loadMode();
const initialEffective = computeEffective(initialMode);
applyTheme(initialEffective);

type State = {
  mode: ThemeMode;
  effective: EffectiveTheme;
  setMode: (mode: ThemeMode) => void;
};

export const useThemeStore = create<State>((set) => ({
  mode: initialMode,
  effective: initialEffective,
  setMode: (mode) => {
    try {
      localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // ignore
    }
    const effective = computeEffective(mode);
    applyTheme(effective);
    set({ mode, effective });
  },
}));

// Keep effective in sync when the OS preference changes and mode is 'system'.
if (typeof window !== "undefined") window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", (e) => {
  const { mode } = useThemeStore.getState();
  if (mode === "system") {
    const effective: EffectiveTheme = e.matches ? "dark" : "light";
    applyTheme(effective);
    useThemeStore.setState({ effective });
  }
});
