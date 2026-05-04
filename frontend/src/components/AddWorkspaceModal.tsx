import { useState } from "react";
import { AddWorkspaceForm } from "./AddWorkspaceForm";
import { RemoteWorkspaceSetup } from "./RemoteWorkspaceSetup";

type WorkspaceType = "local" | "remote";

export function AddWorkspaceModal({ onDone }: { onDone: () => void }) {
  const [type, setType] = useState<WorkspaceType | null>(null);

  if (type === "local") {
    return <AddWorkspaceForm onDone={onDone} />;
  }

  if (type === "remote") {
    return <RemoteWorkspaceSetup onDone={onDone} />;
  }

  return (
    <div className="space-y-4 text-sm">
      <div className="text-xs text-slate-400 mb-1">Choose workspace type</div>
      <div className="grid grid-cols-2 gap-3">
        <button
          onClick={() => setType("local")}
          className="flex flex-col items-start gap-2 rounded border border-slate-700 bg-slate-900 hover:border-emerald-600 hover:bg-slate-800 p-4 text-left transition-colors"
        >
          <div className="flex items-center gap-2">
            <span className="text-lg">💻</span>
            <span className="text-slate-200 font-medium">Local</span>
          </div>
          <p className="text-xs text-slate-400 leading-relaxed">
            Runs on your machine. Uses a local git checkout. Default for all existing workspaces.
          </p>
          <span className="text-xs text-emerald-400 mt-1">Ready to use</span>
        </button>

        <button
          onClick={() => setType("remote")}
          className="flex flex-col items-start gap-2 rounded border border-slate-700 bg-slate-900 hover:border-sky-600 hover:bg-slate-800 p-4 text-left transition-colors"
        >
          <div className="flex items-center gap-2">
            <span className="text-lg">☁️</span>
            <span className="text-slate-200 font-medium">Remote</span>
            <span className="text-[10px] bg-sky-900 text-sky-300 px-1.5 py-0.5 rounded">Beta</span>
          </div>
          <p className="text-xs text-slate-400 leading-relaxed">
            Runs inside a Cloudflare Worker. Requires a CF API token and GitHub PAT. Work continues even when your laptop is closed.
          </p>
          <span className="text-xs text-sky-400 mt-1">Requires CF account</span>
        </button>
      </div>

      <div className="flex justify-end pt-2 border-t border-slate-800">
        <button onClick={onDone} className="px-3 py-1 text-slate-400 hover:text-slate-200 text-xs">
          Cancel
        </button>
      </div>
    </div>
  );
}
