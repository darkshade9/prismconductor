import { useEffect, useState } from "react";
import {
  ListCollections,
  ListWorkspaces,
  CreateCollection,
  RenameCollection,
  DeleteCollection,
  AddWorkspaceToCollection,
  RemoveWorkspaceFromCollection,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { types } from "../../wailsjs/go/models";
import { CollectionContextModal } from "./CollectionContextModal";
import { noAutoCorrect } from "../lib/inputs";

export function CollectionsPanel() {
  const [collections, setCollections] = useState<types.Collection[]>([]);
  const [workspaces, setWorkspaces] = useState<types.Workspace[]>([]);
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [editingContext, setEditingContext] = useState<types.Collection | null>(null);
  const [renamingID, setRenamingID] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [err, setErr] = useState("");

  async function refresh() {
    try {
      const [cols, ws] = await Promise.all([ListCollections(), ListWorkspaces()]);
      setCollections(cols ?? []);
      setWorkspaces(ws ?? []);
    } catch (e: any) {
      setErr(String(e));
    }
  }

  useEffect(() => {
    refresh();
    const off = EventsOn("bus.collections_updated", refresh);
    return () => off();
  }, []);

  // Workspace IDs already in any collection.
  const memberedIDs = new Set(collections.flatMap((c) => c.workspace_ids ?? []));

  async function handleCreate() {
    if (!newName.trim()) return;
    setCreating(true);
    setErr("");
    try {
      await CreateCollection(newName.trim());
      setNewName("");
      await refresh();
    } catch (e: any) {
      setErr(String(e));
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(id: string) {
    setErr("");
    try {
      await DeleteCollection(id);
      await refresh();
    } catch (e: any) {
      setErr(String(e));
    }
  }

  async function handleRename(id: string) {
    if (!renameValue.trim()) { setRenamingID(null); return; }
    setErr("");
    try {
      await RenameCollection(id, renameValue.trim());
      setRenamingID(null);
      await refresh();
    } catch (e: any) {
      setErr(String(e));
    }
  }

  async function handleAddMember(collectionID: string, workspaceID: string) {
    setErr("");
    try {
      await AddWorkspaceToCollection(collectionID, workspaceID);
      await refresh();
    } catch (e: any) {
      setErr(String(e));
    }
  }

  async function handleRemoveMember(collectionID: string, workspaceID: string) {
    setErr("");
    try {
      await RemoveWorkspaceFromCollection(collectionID, workspaceID);
      await refresh();
    } catch (e: any) {
      setErr(String(e));
    }
  }

  const wsName = (id: string) =>
    workspaces.find((w) => w.id === id)?.display_name || id;

  return (
    <div className="flex flex-col gap-4 text-sm">
      {err && <div className="text-xs text-red-400 bg-red-950/30 border border-red-900 rounded px-3 py-1.5">{err}</div>}

      {/* Create new */}
      <div className="flex gap-2">
        <input
          type="text"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          placeholder="New collection name…"
          className="flex-1 bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 focus:outline-none focus:border-slate-500"
          {...noAutoCorrect}
        />
        <button
          onClick={handleCreate}
          disabled={creating || !newName.trim()}
          className="px-3 py-1 text-xs bg-slate-700 hover:bg-slate-600 text-slate-100 rounded disabled:opacity-50"
        >
          {creating ? "Creating…" : "Create"}
        </button>
      </div>

      {/* Collection list */}
      {collections.length === 0 && (
        <div className="text-xs text-slate-500">No collections yet. Create one above to group workspaces.</div>
      )}

      {collections.map((col) => {
        // Workspaces not yet in any collection (available to add to this one).
        const available = workspaces.filter(
          (w) => !memberedIDs.has(w.id) && w.enabled !== false
        );

        return (
          <div key={col.id} className="border border-slate-700 rounded-lg overflow-hidden">
            {/* Header */}
            <div className="flex items-center gap-2 px-3 py-2 bg-slate-800/50">
              {renamingID === col.id ? (
                <input
                  autoFocus
                  type="text"
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") handleRename(col.id); if (e.key === "Escape") setRenamingID(null); }}
                  onBlur={() => handleRename(col.id)}
                  className="flex-1 bg-slate-700 border border-slate-600 rounded px-2 py-0.5 text-xs text-slate-100 focus:outline-none"
                  {...noAutoCorrect}
                />
              ) : (
                <span className="flex-1 text-slate-200 font-medium">{col.name}</span>
              )}
              <button
                onClick={() => { setRenamingID(col.id); setRenameValue(col.name); }}
                className="text-xs text-slate-500 hover:text-slate-300"
                title="Rename"
              >
                ✎
              </button>
              <button
                onClick={() => setEditingContext(col)}
                className="text-xs text-slate-500 hover:text-slate-300"
                title="Edit shared context"
              >
                📝 Context
              </button>
              <button
                onClick={() => handleDelete(col.id)}
                className="text-xs text-slate-500 hover:text-red-400"
                title="Delete collection"
              >
                ✕
              </button>
            </div>

            {/* Members */}
            <div className="px-3 py-2 flex flex-wrap gap-1.5">
              {(col.workspace_ids ?? []).map((wsID) => (
                <span
                  key={wsID}
                  className="inline-flex items-center gap-1 px-2 py-0.5 bg-slate-700 rounded text-xs text-slate-200"
                >
                  {wsName(wsID)}
                  <button
                    onClick={() => handleRemoveMember(col.id, wsID)}
                    className="text-slate-400 hover:text-red-400 leading-none"
                    title="Remove from collection"
                  >
                    ×
                  </button>
                </span>
              ))}

              {/* Add member dropdown */}
              {available.length > 0 && (
                <select
                  value=""
                  onChange={(e) => { if (e.target.value) handleAddMember(col.id, e.target.value); }}
                  className="px-2 py-0.5 bg-slate-800 border border-slate-700 rounded text-xs text-slate-400 hover:text-slate-200 cursor-pointer focus:outline-none"
                >
                  <option value="">+ Add member</option>
                  {available.map((w) => (
                    <option key={w.id} value={w.id}>{w.display_name || w.id}</option>
                  ))}
                </select>
              )}

              {(col.workspace_ids ?? []).length === 0 && available.length === 0 && (
                <span className="text-xs text-slate-500">No available workspaces</span>
              )}
            </div>
          </div>
        );
      })}

      {editingContext && (
        <CollectionContextModal
          collection={editingContext}
          onClose={() => { setEditingContext(null); refresh(); }}
        />
      )}
    </div>
  );
}
