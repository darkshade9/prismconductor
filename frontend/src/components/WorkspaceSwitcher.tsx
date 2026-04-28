export function WorkspaceSwitcher() {
  // Stub: hard-coded chips. Phase 1 Day 2 wires this to ListWorkspaces().
  const workspaces = ["All", "prismengine", "prismeditor", "pe_ai_agents"];
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800 text-sm">
      <span className="text-slate-500">Workspace:</span>
      {workspaces.map((w, i) => (
        <button
          key={w}
          className={
            "px-2 py-0.5 rounded border " +
            (i === 0
              ? "bg-slate-800 border-slate-600 text-slate-200"
              : "border-slate-700 text-slate-400 hover:text-slate-200")
          }
        >
          {w}
        </button>
      ))}
    </div>
  );
}
