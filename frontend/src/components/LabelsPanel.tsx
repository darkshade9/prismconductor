import { useEffect, useMemo, useState } from "react";
import { CreateLabel, DeleteLabel, UpdateLabel } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";
import { useLabelsStore, EMPTY_LABELS } from "../stores/labelsStore";
import { getContrastText, normalizeHex } from "../lib/contrast";
import { noAutoCorrect } from "../lib/inputs";

type Props = {
  workspaceID: string;
  // Pre-fill the new-label form with this name (used by PlanModal suggestions).
  initialNewName?: string;
};

const DEFAULT_COLOR = "#94a3b8";

export function LabelsPanel({ workspaceID, initialNewName }: Props) {
  const labels = useLabelsStore((s) => s.byWorkspace[workspaceID] ?? EMPTY_LABELS);
  const refresh = useLabelsStore((s) => s.refresh);
  const loading = useLabelsStore((s) => s.loading[workspaceID] ?? false);

  useEffect(() => {
    refresh(workspaceID);
  }, [workspaceID, refresh]);

  return (
    <div className="space-y-3">
      <NewLabelForm workspaceID={workspaceID} initialName={initialNewName} />
      <ul className="divide-y divide-slate-800 rounded border border-slate-800">
        {loading && labels.length === 0 && (
          <li className="px-3 py-3 text-xs text-slate-500">loading…</li>
        )}
        {!loading && labels.length === 0 && (
          <li className="px-3 py-3 text-xs text-slate-500">
            No labels yet. Add one above to get started.
          </li>
        )}
        {labels.map((l) => (
          <LabelRow key={l.name} workspaceID={workspaceID} label={l} />
        ))}
      </ul>
    </div>
  );
}

function NewLabelForm({
  workspaceID,
  initialName,
}: {
  workspaceID: string;
  initialName?: string;
}) {
  const refresh = useLabelsStore((s) => s.refresh);
  const [name, setName] = useState(initialName ?? "");
  const [color, setColor] = useState(DEFAULT_COLOR);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If parent passes a fresh initialName (e.g. user clicks a different
  // suggestion chip while the modal is open), update the input.
  useEffect(() => {
    if (initialName !== undefined) setName(initialName);
  }, [initialName]);

  const trimmedName = name.trim();
  const cleanColor = normalizeHex(color);
  const valid = trimmedName.length > 0 && /^[0-9a-f]{6}$/.test(cleanColor);

  async function submit() {
    if (!valid) return;
    setBusy(true);
    setError(null);
    try {
      await CreateLabel(
        workspaceID,
        new types.Label({ name: trimmedName, color: cleanColor, description }),
      );
      await refresh(workspaceID);
      setName("");
      setColor(DEFAULT_COLOR);
      setDescription("");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded border border-slate-700 bg-slate-950 p-3 space-y-2">
      <div className="text-xs text-slate-400">+ New label</div>
      <div className="flex items-center gap-2">
        <input
          {...noAutoCorrect}
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="name (e.g. backend, good-first-issue)"
          className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-sm text-slate-200"
        />
        <input
          type="color"
          value={`#${cleanColor}`}
          onChange={(e) => setColor(e.target.value)}
          className="h-7 w-10 cursor-pointer bg-slate-900 border border-slate-700 rounded"
          title="Pick a color"
        />
        <button
          onClick={submit}
          disabled={busy || !valid}
          className="px-3 py-1 text-xs bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
        >
          {busy ? "…" : "Create"}
        </button>
      </div>
      <input
        {...noAutoCorrect}
        type="text"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="description (optional)"
        className="w-full bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-300"
      />
      {error && <div className="text-xs text-red-400">{error}</div>}
    </div>
  );
}

function LabelRow({ workspaceID, label }: { workspaceID: string; label: types.Label }) {
  const refresh = useLabelsStore((s) => s.refresh);
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [name, setName] = useState(label.name);
  const [color, setColor] = useState(`#${label.color}`);
  const [description, setDescription] = useState(label.description ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const cleanColor = useMemo(() => normalizeHex(color), [color]);
  const fg = useMemo(() => getContrastText(cleanColor), [cleanColor]);
  const validEdit = name.trim().length > 0 && /^[0-9a-f]{6}$/.test(cleanColor);

  async function saveEdit() {
    if (!validEdit) return;
    setBusy(true);
    setError(null);
    try {
      await UpdateLabel(
        workspaceID,
        label.name,
        new types.Label({ name: name.trim(), color: cleanColor, description }),
      );
      await refresh(workspaceID);
      setEditing(false);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setError(null);
    try {
      await DeleteLabel(workspaceID, label.name);
      await refresh(workspaceID);
    } catch (e: any) {
      setError(String(e?.message ?? e));
      setConfirmDelete(false);
    } finally {
      setBusy(false);
    }
  }

  if (editing) {
    return (
      <li className="px-3 py-2 space-y-2">
        <div className="flex items-center gap-2">
          <input
            type="color"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="h-7 w-10 cursor-pointer bg-slate-900 border border-slate-700 rounded"
          />
          <input
            {...noAutoCorrect}
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-sm text-slate-200"
          />
          <button
            onClick={saveEdit}
            disabled={busy || !validEdit}
            className="px-2 py-0.5 text-xs bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50"
          >
            Save
          </button>
          <button
            onClick={() => setEditing(false)}
            className="px-2 py-0.5 text-xs text-slate-400 hover:text-slate-200"
          >
            Cancel
          </button>
        </div>
        <input
          {...noAutoCorrect}
          type="text"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="description (optional)"
          className="w-full bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-slate-300"
        />
        {error && <div className="text-xs text-red-400">{error}</div>}
      </li>
    );
  }

  return (
    <li className="px-3 py-2">
      <div className="flex items-center gap-3">
        <span
          className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium"
          style={{ backgroundColor: `#${label.color}`, color: fg }}
        >
          {label.name}
        </span>
        <div className="flex-1 min-w-0 text-xs text-slate-500 truncate">
          {label.description || ""}
        </div>
        <button
          onClick={() => setEditing(true)}
          className="text-xs text-slate-400 hover:text-slate-200"
        >
          Edit
        </button>
        {confirmDelete ? (
          <div className="flex gap-1 text-xs">
            <button onClick={remove} disabled={busy} className="text-red-400 hover:text-red-300 px-2">
              Confirm
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              className="text-slate-500 hover:text-slate-300 px-2"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            onClick={() => setConfirmDelete(true)}
            className="text-xs text-slate-500 hover:text-red-400"
          >
            Delete
          </button>
        )}
      </div>
      {error && <div className="text-xs text-red-400 mt-1">{error}</div>}
    </li>
  );
}
