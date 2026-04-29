import { useEffect, useMemo, useRef, useState } from "react";
import { SetIssueLabels } from "../../wailsjs/go/main/App";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { getContrastText } from "../lib/contrast";

type Props = {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  issueNumber: number;
  currentLabels: string[];
  // Position the popover near a click target. Coordinates are clientX/clientY.
  anchor?: { x: number; y: number } | null;
};

export function LabelManagePopover({
  open,
  onClose,
  workspaceID,
  issueNumber,
  currentLabels,
  anchor,
}: Props) {
  const labels = useLabelsStore((s) => s.byWorkspace[workspaceID] ?? EMPTY_LABELS);
  const refresh = useLabelsStore((s) => s.refresh);
  const [selected, setSelected] = useState<Set<string>>(() => new Set(currentLabels));
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (open) {
      setSelected(new Set(currentLabels));
      refresh(workspaceID);
    }
  }, [open, workspaceID, refresh, currentLabels.join("|")]);

  useEffect(() => {
    if (!open) return;
    function onDocClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, onClose]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return labels;
    return labels.filter(
      (l) => l.name.toLowerCase().includes(q) || (l.description ?? "").toLowerCase().includes(q),
    );
  }, [labels, filter]);

  if (!open) return null;

  async function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    setSelected(next);
    setBusy(true);
    try {
      await SetIssueLabels(workspaceID, issueNumber, Array.from(next));
    } catch (e) {
      // Roll back on failure so the UI matches GitHub state.
      setSelected(selected);
      // eslint-disable-next-line no-console
      console.error("SetIssueLabels failed", e);
    } finally {
      setBusy(false);
    }
  }

  const style: React.CSSProperties = anchor
    ? { position: "fixed", top: anchor.y + 4, left: anchor.x }
    : { position: "fixed", top: "50%", left: "50%", transform: "translate(-50%, -50%)" };

  return (
    <div
      ref={ref}
      style={style}
      className="z-50 w-72 max-h-80 bg-slate-900 border border-slate-700 rounded-lg shadow-xl flex flex-col overflow-hidden"
      onClick={(e) => e.stopPropagation()}
    >
      <div className="px-2 py-1.5 border-b border-slate-800">
        <input
          autoFocus
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter labels…"
          className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200"
        />
      </div>
      <div className="flex-1 overflow-y-auto py-1">
        {filtered.length === 0 && (
          <div className="px-3 py-2 text-xs text-slate-500">
            No labels match. Manage labels at the board level to add new ones.
          </div>
        )}
        {filtered.map((l) => {
          const checked = selected.has(l.name);
          const fg = getContrastText(l.color);
          return (
            <button
              key={l.name}
              onClick={() => toggle(l.name)}
              disabled={busy}
              className="w-full flex items-center gap-2 px-2 py-1 hover:bg-slate-800 text-left text-xs disabled:opacity-60"
            >
              <span className={"w-3 h-3 rounded-sm border " + (checked ? "border-slate-200" : "border-slate-700")}>
                {checked && (
                  <svg viewBox="0 0 16 16" className="w-full h-full text-slate-200">
                    <path
                      fill="currentColor"
                      d="M6.5 11.5L3 8l1.4-1.4L6.5 8.7l5.1-5.1L13 5z"
                    />
                  </svg>
                )}
              </span>
              <span
                className="px-1.5 py-0.5 rounded font-medium"
                style={{ backgroundColor: `#${l.color}`, color: fg }}
              >
                {l.name}
              </span>
              {l.description && (
                <span className="text-slate-500 truncate flex-1">{l.description}</span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
