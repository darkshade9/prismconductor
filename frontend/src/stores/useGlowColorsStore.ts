import { create } from "zustand";

export type GlowState = "planReady" | "planning" | "inProgress" | "review" | "blocked" | "done";
export type GlowEntry = { enabled: boolean; color: string };

const STORAGE_KEY = "prismconductor.glowColors";

export const GLOW_DEFAULTS: Record<GlowState, GlowEntry> = {
  planReady:  { enabled: true,  color: "#f59e0b" }, // amber-500
  planning:   { enabled: true,  color: "#38bdf8" }, // sky-500
  inProgress: { enabled: true,  color: "#a855f7" }, // purple-500
  review:     { enabled: false, color: "#22c55e" }, // green-500 — default OFF per plan
  blocked:    { enabled: true,  color: "#ef4444" }, // red-500
  done:       { enabled: false, color: "#64748b" }, // slate-500
};

// Maps store keys to CSS class names injected at runtime.
export const GLOW_CLASS_MAP: Record<GlowState, string> = {
  planReady:  "prs-glow-planready",
  planning:   "prs-glow-planning",
  inProgress: "prs-glow-inprogress",
  review:     "prs-glow-review",
  blocked:    "prs-glow-blocked",
  done:       "prs-glow-done",
};

// Durations chosen to match existing card glow rhythm.
const GLOW_DURATIONS: Record<GlowState, string> = {
  planReady:  "2.4s",
  planning:   "1.6s",
  inProgress: "1.6s",
  review:     "2.0s",
  blocked:    "1.2s",
  done:       "2.0s",
};

function hexToRGB(hex: string): [number, number, number] {
  const h = hex.replace("#", "");
  return [
    parseInt(h.slice(0, 2), 16),
    parseInt(h.slice(2, 4), 16),
    parseInt(h.slice(4, 6), 16),
  ];
}

function injectGlowCSS(colors: Record<GlowState, GlowEntry>) {
  if (typeof document === "undefined") return;
  let el = document.getElementById("prism-glow-overrides") as HTMLStyleElement | null;
  if (!el) {
    el = document.createElement("style");
    el.id = "prism-glow-overrides";
    document.head.appendChild(el);
  }
  const css = (Object.keys(colors) as GlowState[]).map((state) => {
    const [r, g, b] = hexToRGB(colors[state].color);
    const cls = GLOW_CLASS_MAP[state];
    const dur = GLOW_DURATIONS[state];
    return [
      `@keyframes ${cls} {`,
      `  0%, 100% { box-shadow: 0 0 0 1px rgba(${r},${g},${b},0.55), 0 0 14px 2px rgba(${r},${g},${b},0.25); border-color: rgba(${r},${g},${b},0.9); }`,
      `  50%      { box-shadow: 0 0 0 2px rgba(${r},${g},${b},0.95), 0 0 28px 8px rgba(${r},${g},${b},0.55); border-color: rgba(${r},${g},${b},1); }`,
      `}`,
      `.${cls} { animation: ${cls} ${dur} ease-in-out infinite; }`,
    ].join("\n");
  }).join("\n");
  el.textContent = css;
}

function loadColors(): Record<GlowState, GlowEntry> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Record<GlowState, GlowEntry>>;
      return { ...GLOW_DEFAULTS, ...parsed };
    }
  } catch {
    // ignore
  }
  return { ...GLOW_DEFAULTS };
}

type State = {
  colors: Record<GlowState, GlowEntry>;
  setGlow: (state: GlowState, entry: Partial<GlowEntry>) => void;
  resetAll: () => void;
};

const initialColors = loadColors();
injectGlowCSS(initialColors);

export const useGlowColorsStore = create<State>((set, get) => ({
  colors: initialColors,
  setGlow: (state, entry) => {
    const next: Record<GlowState, GlowEntry> = {
      ...get().colors,
      [state]: { ...get().colors[state], ...entry },
    };
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      // ignore
    }
    injectGlowCSS(next);
    set({ colors: next });
  },
  resetAll: () => {
    const defaults = { ...GLOW_DEFAULTS };
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(defaults));
    } catch {
      // ignore
    }
    injectGlowCSS(defaults);
    set({ colors: defaults });
  },
}));
