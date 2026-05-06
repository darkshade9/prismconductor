import { useState } from "react";
import { WorkspacesPanel } from "./WorkspacesPanel";
import { BundledSkillsViewer } from "./BundledSkillsViewer";
import { PoolsPanel } from "./PoolsPanel";
import { NotifyPanel } from "./NotifyPanel";
import { LogsPanel } from "./LogsPanel";
import { AppearancePanel } from "./AppearancePanel";
import { CollectionsPanel } from "./CollectionsPanel";

type Tab = "workspaces" | "collections" | "pools" | "skills" | "notify" | "appearance" | "logs";

export function Settings({
  open,
  onClose,
  initialTab,
}: {
  open: boolean;
  onClose: () => void;
  initialTab?: Tab;
}) {
  const [tab, setTab] = useState<Tab>(initialTab ?? "workspaces");
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[760px] max-h-[80vh] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">Settings</div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>
        <div className="flex flex-1 min-h-0">
          <nav className="w-40 border-r border-slate-800 py-2 text-sm">
            {(
              [
                ["workspaces", "Workspaces"],
                ["collections", "Collections"],
                ["pools", "Pools"],
                ["skills", "Bundled skills"],
                ["notify", "Notifications"],
                ["appearance", "Appearance"],
                ["logs", "Logs"],
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
            {tab === "collections" && <CollectionsPanel />}
            {tab === "pools" && <PoolsPanel />}
            {tab === "skills" && <BundledSkillsViewer />}
            {tab === "notify" && <NotifyPanel />}
            {tab === "appearance" && <AppearancePanel />}
            {tab === "logs" && <LogsPanel />}
          </div>
        </div>
      </div>
    </div>
  );
}
