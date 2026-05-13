import { useState } from "react";
import { WorkspacesPanel } from "./WorkspacesPanel";
import { BundledSkillsViewer } from "./BundledSkillsViewer";
import { PoolsPanel } from "./PoolsPanel";
import { ProvidersPanel } from "./ProvidersPanel";
import { NotifyPanel } from "./NotifyPanel";
import { LogsPanel } from "./LogsPanel";
import { AppearancePanel } from "./AppearancePanel";
import { CollectionsPanel } from "./CollectionsPanel";
import { SpendingPanel } from "./SpendingPanel";
import { EventDiagnostics } from "./EventDiagnostics";
import { useSettingsStore } from "../stores/useSettingsStore";
import { useWorkspaceStore } from "../stores/workspaceStore";

function AdvancedPanel({
  showDiagnostics,
  setShowDiagnostics,
}: {
  showDiagnostics: boolean;
  setShowDiagnostics: (v: boolean) => void;
}) {
  const reconcilerEnabled = useSettingsStore((s) => s.reconcilerEnabled);
  const setReconcilerEnabled = useSettingsStore((s) => s.setReconcilerEnabled);

  return (
    <div className="space-y-4">
      <div>
        <div className="text-slate-300 text-sm font-medium mb-1">Diagnostics</div>
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={showDiagnostics}
            onChange={(e) => setShowDiagnostics(e.target.checked)}
            className="accent-sky-500"
          />
          <span className="text-slate-400 text-sm">
            Show Diagnostics tab
          </span>
        </label>
        <p className="text-slate-500 text-xs mt-1">
          Exposes the Event Diagnostics panel for on-device debugging. Persists across restarts.
          You can also unlock it for the current session with{" "}
          <kbd className="px-1 py-0.5 bg-slate-800 rounded text-slate-300">
            {typeof navigator !== "undefined" && /Mac|Macintosh/.test(navigator.platform || navigator.userAgent)
              ? "⌘⌥D"
              : "Ctrl+Alt+D"}
          </kbd>.
        </p>
      </div>
      <div>
        <div className="text-slate-300 text-sm font-medium mb-1">IssueView reconciler</div>
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={reconcilerEnabled}
            onChange={(e) => setReconcilerEnabled(e.target.checked)}
            className="accent-sky-500"
          />
          <span className="text-slate-400 text-sm">
            Enable convergence reconciler
          </span>
        </label>
        <p className="text-slate-500 text-xs mt-1">
          Periodically checks visible cards against the backend and silently corrects any state
          that was missed due to a dropped event bus delivery. Corrections are logged in the
          Diagnostics → Reconciler drift tab. Disable if you observe unexpected refreshes.
        </p>
      </div>
    </div>
  );
}

type Tab = "workspaces" | "collections" | "providers" | "pools" | "spending" | "skills" | "notify" | "appearance" | "logs" | "advanced" | "diagnostics";

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
  const selectedWorkspaceID = useWorkspaceStore((s) => s.selectedID);
  const showDiagnostics = useSettingsStore((s) => s.showDiagnostics);
  const diagnosticsUnlocked = useSettingsStore((s) => s.diagnosticsUnlocked);
  const setShowDiagnostics = useSettingsStore((s) => s.setShowDiagnostics);

  const diagnosticsVisible = import.meta.env.DEV || showDiagnostics || diagnosticsUnlocked;

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
                ["providers", "Providers"],
                ["pools", "Pools"],
                ["spending", "Spending"],
                ["skills", "Bundled skills"],
                ["notify", "Notifications"],
                ["appearance", "Appearance"],
                ["logs", "Logs"],
                ["advanced", "Advanced"],
                ...(diagnosticsVisible ? [["diagnostics", "Diagnostics"] as [Tab, string]] : []),
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
            {tab === "providers" && <ProvidersPanel />}
            {tab === "pools" && <PoolsPanel />}
            {tab === "spending" && <SpendingPanel workspaceID={selectedWorkspaceID ?? ""} />}
            {tab === "skills" && <BundledSkillsViewer />}
            {tab === "notify" && <NotifyPanel />}
            {tab === "appearance" && <AppearancePanel />}
            {tab === "logs" && <LogsPanel />}
            {tab === "advanced" && (
              <AdvancedPanel
                showDiagnostics={showDiagnostics}
                setShowDiagnostics={setShowDiagnostics}
              />
            )}
            {tab === "diagnostics" && diagnosticsVisible && <EventDiagnostics />}
          </div>
        </div>
      </div>
    </div>
  );
}
