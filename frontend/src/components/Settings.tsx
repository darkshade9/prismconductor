import { useState } from "react";
import { WorkspacesPanel } from "./WorkspacesPanel";

type Tab = "workspaces" | "agents" | "ollama" | "notify";

export function Settings({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("workspaces");
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[680px] max-h-[80vh] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">Settings</div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="flex flex-1 min-h-0">
          <nav className="w-40 border-r border-slate-800 py-2 text-sm">
            {(
              [
                ["workspaces", "Workspaces"],
                ["agents", "Worker pool"],
                ["ollama", "Ollama"],
                ["notify", "Notifications"],
              ] as [Tab, string][]
            ).map(([k, label]) => (
              <button
                key={k}
                onClick={() => setTab(k)}
                className={
                  "w-full text-left px-4 py-1.5 " +
                  (tab === k ? "text-slate-100 bg-slate-800" : "text-slate-400 hover:text-slate-200")
                }
              >
                {label}
              </button>
            ))}
          </nav>
          <div className="flex-1 p-4 overflow-y-auto">
            {tab === "workspaces" && <WorkspacesPanel />}
            {tab === "agents" && <Stub label="Worker pool capacity (1-5) — Phase 5" />}
            {tab === "ollama" && <Stub label="Ollama URL / model — Phase 3" />}
            {tab === "notify" && <Stub label="Notification preferences — Phase 1 Day 7" />}
          </div>
        </div>
      </div>
    </div>
  );
}

function Stub({ label }: { label: string }) {
  return <div className="text-sm text-slate-500">{label}</div>;
}
