import { useEffect, useState } from "react";
import { UpdateCollectionContext } from "../../wailsjs/go/main/App";
import { types } from "../../wailsjs/go/models";

export function CollectionContextModal({
  collection,
  onClose,
}: {
  collection: types.Collection;
  onClose: () => void;
}) {
  const [body, setBody] = useState(collection.context_md ?? "");
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    setBody(collection.context_md ?? "");
    setErr("");
  }, [collection.id]);

  async function save() {
    setSaving(true);
    setErr("");
    try {
      await UpdateCollectionContext(collection.id, body);
      onClose();
    } catch (e: any) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[640px] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200 text-sm">
            Shared context — <span className="text-slate-400">{collection.name}</span>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200 text-xs">✕</button>
        </div>
        <div className="p-4 flex flex-col gap-3">
          <p className="text-xs text-slate-400">
            This markdown is prepended to every plan and execute worker's prompt for workspaces in this collection.
            Use it for architecture decisions, naming conventions, and API contracts that span all repos.
          </p>
          <textarea
            className="w-full h-64 bg-slate-800 border border-slate-700 rounded text-xs text-slate-200 p-2 font-mono resize-none focus:outline-none focus:border-slate-500"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="# Shared architecture&#10;&#10;e.g. Use gRPC between services. Service names: app, infra, helm."
            spellCheck={false}
          />
          {err && <div className="text-xs text-red-400">{err}</div>}
          <div className="flex justify-end gap-2">
            <button
              onClick={onClose}
              className="px-3 py-1 text-xs text-slate-400 hover:text-slate-200 border border-slate-700 rounded"
            >
              Cancel
            </button>
            <button
              onClick={save}
              disabled={saving}
              className="px-3 py-1 text-xs bg-slate-700 hover:bg-slate-600 text-slate-100 rounded disabled:opacity-50"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
