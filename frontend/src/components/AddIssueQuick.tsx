import { useState } from "react";
import { AddManualIssue } from "../../wailsjs/go/main/App";
import { useIssueStore } from "../stores/issueStore";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

// Lets the user create a fake issue card without waiting on the GitHub poll
// (#2). Removed once #2 lands and real issues populate the board.
export function AddIssueQuick() {
  const refresh = useIssueStore((s) => s.refresh);
  const selectedID = useWorkspaceStore((s) => s.selectedID);
  const workspaces = useWorkspaceStore((s) => s.workspaces);
  const [num, setNum] = useState("");
  const [title, setTitle] = useState("");
  const [labels, setLabels] = useState("");
  const [busy, setBusy] = useState(false);

  const targetWorkspace = selectedID ?? workspaces[0]?.id ?? "";

  async function add() {
    if (!targetWorkspace || !num || !title) return;
    setBusy(true);
    try {
      await AddManualIssue(
        targetWorkspace,
        parseInt(num, 10),
        title,
        "",
        labels.split(",").map((s) => s.trim()).filter(Boolean),
        "todo",
      );
      setNum("");
      setTitle("");
      setLabels("");
      await refresh(selectedID ?? "");
    } finally {
      setBusy(false);
    }
  }

  if (!targetWorkspace) return null;

  return (
    <div className="flex items-center gap-1 text-xs">
      <input
        {...noAutoCorrect}
        value={num}
        onChange={(e) => setNum(e.target.value)}
        placeholder="#"
        className="w-14 bg-slate-900 border border-slate-700 rounded px-1.5 py-0.5"
        type="number"
      />
      <input
        {...noAutoCorrect}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="title"
        className="w-40 bg-slate-900 border border-slate-700 rounded px-1.5 py-0.5"
      />
      <input
        {...noAutoCorrect}
        value={labels}
        onChange={(e) => setLabels(e.target.value)}
        placeholder="labels (csv)"
        className="w-32 bg-slate-900 border border-slate-700 rounded px-1.5 py-0.5"
      />
      <button
        onClick={add}
        disabled={busy || !num || !title}
        className="px-2 py-0.5 bg-slate-800 border border-slate-700 rounded hover:bg-slate-700 disabled:opacity-40"
        title={`Add manual issue to ${targetWorkspace}`}
      >
        + test card
      </button>
    </div>
  );
}
