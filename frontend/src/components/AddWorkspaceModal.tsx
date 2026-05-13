import { useState } from "react";
import { AddWorkspaceForm } from "./AddWorkspaceForm";
import { JiraWorkspaceSetup } from "./JiraWorkspaceSetup";

type TrackerKind = "github" | "jira";

export function AddWorkspaceModal({ onDone }: { onDone: () => void }) {
  const [tracker, setTracker] = useState<TrackerKind | null>(null);

  if (tracker === "github") {
    return <AddWorkspaceForm onDone={onDone} />;
  }
  if (tracker === "jira") {
    return <JiraWorkspaceSetup onDone={onDone} />;
  }

  return (
    <div className="space-y-3 text-sm">
      <div className="text-xs text-slate-400 mb-1">Choose where your issues live:</div>
      <div className="grid grid-cols-2 gap-3">
        <button
          onClick={() => setTracker("github")}
          className="flex flex-col items-center gap-2 p-4 rounded border border-slate-700 hover:border-slate-500 hover:bg-slate-800 text-slate-300 transition-colors"
        >
          <svg className="w-8 h-8" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
          </svg>
          <span className="font-medium">GitHub Issues</span>
          <span className="text-xs text-slate-500 text-center">Connect a GitHub repository</span>
        </button>

        <button
          onClick={() => setTracker("jira")}
          className="flex flex-col items-center gap-2 p-4 rounded border border-slate-700 hover:border-blue-600 hover:bg-slate-800 text-slate-300 transition-colors"
        >
          <svg className="w-8 h-8 text-blue-400" viewBox="0 0 24 24" fill="currentColor">
            <path d="M11.571 11.513H0a5.218 5.218 0 0 0 5.232 5.215h2.13v2.057A5.215 5.215 0 0 0 12.575 24V12.518a1.005 1.005 0 0 0-1.004-1.005zm5.216-5.215H5.232a5.218 5.218 0 0 0 5.215 5.215h2.129v2.057a5.218 5.218 0 0 0 5.218 5.218V7.303a1.005 1.005 0 0 0-1.007-1.005zM22.003 1.298H10.432a5.218 5.218 0 0 0 5.215 5.215h2.129v2.057a5.218 5.218 0 0 0 5.218 5.218V2.303a1.005 1.005 0 0 0-1.991-.005z" />
          </svg>
          <span className="font-medium">Jira Cloud</span>
          <span className="text-xs text-slate-500 text-center">Connect a Jira Cloud project</span>
        </button>
      </div>

      <div className="flex justify-end pt-2 border-t border-slate-800">
        <button onClick={onDone} className="px-3 py-1 text-slate-400 hover:text-slate-200">Cancel</button>
      </div>
    </div>
  );
}
