import { useEffect, useMemo, useState } from "react";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { LabelsModal } from "./LabelsModal";
import { ListCollections } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { types } from "../../wailsjs/go/models";

export function WorkspaceSwitcher({
  archivedCount = 0,
  onOpenArchived,
}: {
  archivedCount?: number;
  onOpenArchived?: () => void;
}) {
  const { workspaces, selectedID, setSelected, refresh, loading } = useWorkspaceStore();
  const [labelsOpen, setLabelsOpen] = useState(false);
  const [collections, setCollections] = useState<types.Collection[]>([]);
  const labelsTarget = selectedID ?? workspaces[0]?.id ?? "";

  useEffect(() => {
    if (workspaces.length === 0 && !loading) refresh();
  }, [workspaces.length, loading, refresh]);

  useEffect(() => {
    ListCollections().then((cols) => setCollections(cols ?? [])).catch(() => {});
    const off = EventsOn("bus.collections_updated", () => {
      ListCollections().then((cols) => setCollections(cols ?? [])).catch(() => {});
    });
    return () => off();
  }, []);

  // Group workspaces: collections first (with header), then "Other" for uncollected.
  const groups = useMemo(() => {
    const memberedIDs = new Set(collections.flatMap((c) => c.workspace_ids ?? []));
    const wsMap = new Map(workspaces.map((w) => [w.id, w]));

    const result: { header: string | null; workspaces: types.Workspace[] }[] = [];

    // One group per collection (ordered by collection creation).
    collections.forEach((col) => {
      const members = (col.workspace_ids ?? [])
        .map((id) => wsMap.get(id))
        .filter((w): w is types.Workspace => w !== undefined);
      if (members.length > 0) {
        result.push({ header: col.name, workspaces: members });
      }
    });

    // "Other" group for workspaces not in any collection.
    const others = workspaces.filter((w) => !memberedIDs.has(w.id));
    if (others.length > 0) {
      result.push({ header: result.length > 0 ? "Other" : null, workspaces: others });
    }

    // If no collections at all, return a flat group with no header.
    if (result.length === 0) {
      result.push({ header: null, workspaces });
    }

    return result;
  }, [workspaces, collections]);

  const WorkspaceButton = ({ w }: { w: types.Workspace }) => (
    <button
      key={w.id}
      data-testid="workspace-chip"
      onClick={() => setSelected(w.id)}
      className={
        "px-2 py-0.5 rounded border inline-flex items-center gap-1.5 " +
        (selectedID === w.id
          ? "bg-slate-800 border-slate-600 text-slate-200"
          : "border-slate-700 text-slate-400 hover:text-slate-200")
      }
    >
      <span className="inline-block w-2 h-2 rounded-full" style={{ backgroundColor: w.color || "#64748b" }} />
      {w.display_name || w.id}
    </button>
  );

  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800 text-sm flex-wrap">
      <span className="text-slate-500">Workspace:</span>
      <button
        onClick={() => setSelected(null)}
        className={
          "px-2 py-0.5 rounded border " +
          (selectedID === null
            ? "bg-slate-800 border-slate-600 text-slate-200"
            : "border-slate-700 text-slate-400 hover:text-slate-200")
        }
      >
        All
      </button>

      {groups.map((group, gi) => (
        <span key={gi} className="flex items-center gap-1.5">
          {group.header && (
            <span className="text-[10px] text-slate-600 pl-1 border-l border-dotted border-slate-700 ml-0.5" style={{ opacity: 0.7 }}>
              {group.header}
            </span>
          )}
          {group.workspaces.map((w) => (
            <WorkspaceButton key={w.id} w={w} />
          ))}
        </span>
      ))}

      {workspaces.length === 0 && !loading && (
        <span className="text-xs text-slate-600">— no workspaces yet, add one in Settings</span>
      )}
      {labelsTarget && (
        <button
          onClick={() => setLabelsOpen(true)}
          className="ml-auto px-2 py-0.5 rounded border border-slate-700 text-xs text-slate-400 hover:text-slate-200 hover:border-slate-500"
          title="Manage labels for the active workspace"
        >
          🏷  Manage labels
        </button>
      )}
      {onOpenArchived && (
        <button
          onClick={onOpenArchived}
          className={
            "px-2 py-0.5 rounded border text-xs " +
            (labelsTarget ? "" : "ml-auto ") +
            (archivedCount > 0
              ? "border-slate-700 text-slate-300 hover:text-slate-100 hover:border-slate-500"
              : "border-slate-800 text-slate-600 hover:text-slate-400")
          }
          title="Show archived issues"
        >
          🗄 Archived ({archivedCount})
        </button>
      )}
      <LabelsModal
        open={labelsOpen}
        onClose={() => setLabelsOpen(false)}
        workspaceID={labelsTarget}
      />
    </div>
  );
}
