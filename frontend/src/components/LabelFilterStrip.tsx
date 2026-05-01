import { useEffect } from "react";
import { types } from "../../wailsjs/go/models";
import { useLabelsStore } from "../stores/labelsStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { useLabelFilterStore, toggleLabel, setFilterMode, clearFilter, LabelFilterMode } from "../stores/useLabelFilterStore";
import { getContrastText } from "../lib/contrast";

function LabelChip({ label, selected, workspaceID }: { label: types.Label; selected: boolean; workspaceID: string }) {
  const color = label.color ?? "475569";
  const fg = getContrastText(color);

  return (
    <button
      role="checkbox"
      aria-checked={selected}
      aria-pressed={selected}
      onClick={() => toggleLabel(workspaceID, label.name)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          toggleLabel(workspaceID, label.name);
        }
      }}
      className="flex-shrink-0 px-2 py-0.5 rounded text-[11px] font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:ring-slate-400 transition-opacity"
      style={
        selected
          ? { backgroundColor: `#${color}`, color: fg, border: "1px solid transparent" }
          : { backgroundColor: "rgba(71,85,105,0.25)", color: "#94a3b8", border: `1px solid #${color}66` }
      }
      title={label.description || label.name}
    >
      {label.name}
    </button>
  );
}

export function LabelFilterStrip() {
  const { selectedID } = useWorkspaceStore();
  const { list, refresh } = useLabelsStore();
  const { selected, mode, loadForWorkspace } = useLabelFilterStore();

  const wsID = selectedID ?? "";

  useEffect(() => {
    if (!wsID) return;
    refresh(wsID);
    loadForWorkspace(wsID);
  }, [wsID, refresh, loadForWorkspace]);

  const labels = [...list(wsID)].sort((a, b) => a.name.localeCompare(b.name));

  if (labels.length === 0) return null;

  const hasSelection = selected.length > 0;

  return (
    <div className="px-4 py-1.5 border-b border-slate-800 flex items-center gap-2 min-h-[36px]">
      {/* AND/OR toggle — only meaningful when ≥2 labels selected */}
      <div
        role="group"
        aria-label="Filter mode"
        className="flex-shrink-0 flex rounded overflow-hidden border border-slate-700 text-[10px] font-semibold"
      >
        {(["or", "and"] as LabelFilterMode[]).map((m) => (
          <button
            key={m}
            onClick={() => setFilterMode(wsID, m)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                setFilterMode(wsID, m);
              }
            }}
            aria-pressed={mode === m}
            className={
              "px-1.5 py-0.5 focus:outline-none focus-visible:ring-1 focus-visible:ring-slate-400 transition-colors " +
              (mode === m ? "bg-slate-600 text-white" : "bg-transparent text-slate-500 hover:text-slate-300")
            }
          >
            {m.toUpperCase()}
          </button>
        ))}
      </div>

      {/* Label chips — horizontal scroll */}
      <div className="flex items-center gap-1.5 overflow-x-auto flex-1 min-w-0 scrollbar-hide">
        {labels.map((lab) => (
          <LabelChip
            key={lab.name}
            label={lab}
            selected={selected.includes(lab.name)}
            workspaceID={wsID}
          />
        ))}
      </div>

      {/* Clear button — only shown when a selection is active */}
      {hasSelection && (
        <button
          onClick={() => clearFilter(wsID)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              clearFilter(wsID);
            }
          }}
          className="flex-shrink-0 text-[10px] text-slate-500 hover:text-slate-200 px-1.5 py-0.5 rounded border border-slate-800 hover:border-slate-600 focus:outline-none focus-visible:ring-1 focus-visible:ring-slate-400"
          aria-label="Clear label filter"
        >
          ✕ Clear
        </button>
      )}
    </div>
  );
}
