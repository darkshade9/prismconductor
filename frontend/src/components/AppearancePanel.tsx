import { useEffect, useRef, useState } from "react";
import { HexColorPicker } from "react-colorful";
import { useThemeStore } from "../stores/useThemeStore";
import {
  useGlowColorsStore,
  GLOW_DEFAULTS,
  type GlowState,
  type GlowEntry,
} from "../stores/useGlowColorsStore";

const GLOW_STATE_LABELS: { state: GlowState; label: string; description: string }[] = [
  { state: "planning",   label: "Planning",    description: "Active plan session" },
  { state: "inProgress", label: "In Progress", description: "Active execute session" },
  { state: "planReady",  label: "Plan Ready",  description: "Plan awaiting review or answer" },
  { state: "review",     label: "Review ✓",    description: "PR mergeable with passing checks (default off)" },
  { state: "blocked",    label: "Blocked",     description: "Blocked or failed session" },
  { state: "done",       label: "Done",        description: "Card in DONE column (default off)" },
];

export function AppearancePanel() {
  const { mode, setMode } = useThemeStore();
  const { colors, setGlow, resetAll } = useGlowColorsStore();
  const [openPicker, setOpenPicker] = useState<GlowState | null>(null);

  return (
    <div className="space-y-6 text-sm">
      <section>
        <h3 className="font-medium text-slate-200 dark:text-slate-200 mb-3">Theme</h3>
        <div className="flex gap-6">
          {(["dark", "light", "system"] as const).map((m) => (
            <label key={m} className="flex items-center gap-2 cursor-pointer select-none">
              <input
                type="radio"
                name="theme-mode"
                value={m}
                checked={mode === m}
                onChange={() => setMode(m)}
                className="accent-sky-500 w-3.5 h-3.5"
              />
              <span className="text-slate-300 dark:text-slate-300 capitalize">{m}</span>
            </label>
          ))}
        </div>
      </section>

      <section>
        <div className="flex items-center justify-between mb-3">
          <h3 className="font-medium text-slate-200 dark:text-slate-200">Card Glows</h3>
          <button
            onClick={resetAll}
            className="text-xs text-slate-500 hover:text-slate-300 border border-slate-700 rounded px-2 py-0.5 hover:border-slate-500"
          >
            Reset all to defaults
          </button>
        </div>
        <div className="space-y-2">
          {GLOW_STATE_LABELS.map(({ state, label, description }) => (
            <GlowRow
              key={state}
              state={state}
              label={label}
              description={description}
              entry={colors[state]}
              isPickerOpen={openPicker === state}
              onTogglePicker={() => setOpenPicker(openPicker === state ? null : state)}
              onClosePicker={() => setOpenPicker(null)}
              onChange={(partial) => setGlow(state, partial)}
              onReset={() => setGlow(state, GLOW_DEFAULTS[state])}
            />
          ))}
        </div>
      </section>
    </div>
  );
}

function GlowRow({
  state,
  label,
  description,
  entry,
  isPickerOpen,
  onTogglePicker,
  onClosePicker,
  onChange,
  onReset,
}: {
  state: GlowState;
  label: string;
  description: string;
  entry: GlowEntry;
  isPickerOpen: boolean;
  onTogglePicker: () => void;
  onClosePicker: () => void;
  onChange: (partial: Partial<GlowEntry>) => void;
  onReset: () => void;
}) {
  const pickerRef = useRef<HTMLDivElement>(null);

  // Close picker on outside click.
  useEffect(() => {
    if (!isPickerOpen) return;
    function onDown(e: MouseEvent) {
      if (pickerRef.current && !pickerRef.current.contains(e.target as Node)) {
        onClosePicker();
      }
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [isPickerOpen, onClosePicker]);

  const isDefault =
    entry.enabled === GLOW_DEFAULTS[state].enabled &&
    entry.color === GLOW_DEFAULTS[state].color;

  return (
    <div className="flex items-center gap-3 py-1.5 border-b border-slate-800 last:border-0">
      {/* Color swatch */}
      <div className="relative" ref={pickerRef}>
        <button
          onClick={onTogglePicker}
          title={`Pick color for ${label} glow`}
          className="w-6 h-6 rounded border-2 border-slate-600 hover:border-slate-400 transition-colors shrink-0"
          style={{ backgroundColor: entry.color }}
        />
        {isPickerOpen && (
          <div className="absolute z-50 mt-1 left-0">
            <div className="bg-slate-800 border border-slate-600 rounded-lg p-3 shadow-2xl">
              <HexColorPicker
                color={entry.color}
                onChange={(color: string) => onChange({ color })}
              />
              <div className="mt-2 flex items-center gap-2">
                <input
                  type="text"
                  value={entry.color}
                  onChange={(e) => {
                    const v = e.target.value;
                    if (/^#[0-9a-fA-F]{6}$/.test(v)) onChange({ color: v });
                  }}
                  className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-0.5 text-xs font-mono text-slate-200"
                  maxLength={7}
                />
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Label + description */}
      <div className="flex-1 min-w-0">
        <span className="text-slate-200">{label}</span>
        <span className="text-slate-500 ml-2 text-xs">{description}</span>
      </div>

      {/* Enable toggle */}
      <label className="flex items-center gap-1.5 cursor-pointer select-none shrink-0">
        <input
          type="checkbox"
          checked={entry.enabled}
          onChange={(e) => onChange({ enabled: e.target.checked })}
          className="accent-sky-500 w-3.5 h-3.5"
        />
        <span className={entry.enabled ? "text-slate-300" : "text-slate-600"}>
          {entry.enabled ? "On" : "Off"}
        </span>
      </label>

      {/* Reset */}
      <button
        onClick={onReset}
        disabled={isDefault}
        className="text-xs text-slate-600 hover:text-slate-300 disabled:opacity-30 disabled:cursor-default shrink-0"
        title="Reset to default"
      >
        ↺
      </button>
    </div>
  );
}
