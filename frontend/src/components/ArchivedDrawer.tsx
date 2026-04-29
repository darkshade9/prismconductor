import { useEffect, useMemo } from "react";
import { types } from "../../wailsjs/go/models";
import { useArchivedStore } from "../stores/issueStore";
import { useWorkspaceStore } from "../stores/workspaceStore";

function relativeTime(input: any): string {
  if (!input) return "";
  const d = input instanceof Date ? input : new Date(input);
  if (isNaN(d.getTime())) return "";
  const diffSec = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 30) return `${diffDay}d ago`;
  const diffMo = Math.floor(diffDay / 30);
  if (diffMo < 12) return `${diffMo}mo ago`;
  return `${Math.floor(diffMo / 12)}y ago`;
}

export function ArchivedDrawer({
  open,
  onClose,
  workspaceID,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
}) {
  const { archived, refresh, unarchive, unarchiveAll, loading } = useArchivedStore();
  const workspaces = useWorkspaceStore((s) => s.workspaces);

  useEffect(() => {
    if (open) refresh(workspaceID);
  }, [open, workspaceID, refresh]);

  const wsMeta = useMemo(() => {
    const m = new Map<string, { color: string; label: string }>();
    workspaces.forEach((w) => m.set(w.id, { color: w.color, label: w.display_name || w.id }));
    return m;
  }, [workspaces]);

  if (!open) return null;

  const handleRestoreAll = async () => {
    if (archived.length === 0) return;
    if (
      archived.length > 5 &&
      !window.confirm(`Restore all ${archived.length} archived cards?`)
    ) {
      return;
    }
    await unarchiveAll(workspaceID);
  };

  return (
    <div className="fixed right-0 top-0 h-full w-[420px] bg-slate-950 border-l border-slate-800 flex flex-col z-30">
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800">
        <div className="text-sm text-slate-200">
          🗄 Archived <span className="text-slate-500">({archived.length})</span>
        </div>
        <button className="text-slate-400 hover:text-slate-200" onClick={onClose}>
          ✕
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {loading && archived.length === 0 ? (
          <div className="px-3 py-4 text-xs text-slate-500">loading…</div>
        ) : archived.length === 0 ? (
          <div className="px-3 py-4 text-xs text-slate-500">
            Nothing archived in this workspace yet.
          </div>
        ) : (
          <ul className="divide-y divide-slate-800">
            {archived.map((iss) => (
              <ArchivedRow
                key={`${iss.workspace_id}#${iss.number}`}
                iss={iss}
                wsColor={wsMeta.get(iss.workspace_id)?.color}
                wsLabel={wsMeta.get(iss.workspace_id)?.label}
                onRestore={() => unarchive(iss.workspace_id, iss.number)}
              />
            ))}
          </ul>
        )}
      </div>

      <div className="px-3 py-2 border-t border-slate-800">
        <button
          onClick={handleRestoreAll}
          disabled={archived.length === 0}
          className="text-xs px-2 py-1 border border-slate-700 rounded text-slate-300 hover:bg-slate-800 disabled:opacity-40"
        >
          🔁 Restore all ({archived.length})
        </button>
      </div>
    </div>
  );
}

function ArchivedRow({
  iss,
  wsColor,
  wsLabel,
  onRestore,
}: {
  iss: types.Issue;
  wsColor?: string;
  wsLabel?: string;
  onRestore: () => void;
}) {
  return (
    <li className="px-3 py-2 flex items-center gap-2">
      <span
        className="inline-block w-2 h-2 rounded-full shrink-0"
        style={{ backgroundColor: wsColor || "#64748b" }}
        title={wsLabel ?? iss.workspace_id}
      />
      <div className="flex-1 min-w-0">
        <div className="text-xs text-slate-200 truncate">
          <span className="text-slate-500">#{iss.number}</span> {iss.title}
        </div>
        <div className="text-[10px] text-slate-500">
          archived {relativeTime((iss as any).archived_at)}
        </div>
      </div>
      <button
        onClick={onRestore}
        title="Restore to DONE"
        className="text-xs px-1.5 py-0.5 rounded border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500"
      >
        🔁
      </button>
    </li>
  );
}
