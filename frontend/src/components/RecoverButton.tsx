import { useState } from "react";
import { RecoverOrphanQuestion } from "../../wailsjs/go/main/App";

export function RecoverButton({
  workspaceID,
  issueNumber,
}: {
  workspaceID: string;
  issueNumber: number;
}) {
  const [busy, setBusy] = useState(false);

  async function recover(e: React.MouseEvent) {
    e.stopPropagation();
    setBusy(true);
    try {
      await RecoverOrphanQuestion(workspaceID, issueNumber);
    } catch (err: any) {
      alert(String(err?.message ?? err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <button
      onClick={recover}
      onMouseDown={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
      disabled={busy}
      className="ml-auto px-1.5 py-0.5 rounded text-[10px] border border-red-700 text-red-300 hover:border-red-500 hover:text-red-200 disabled:opacity-50"
      title="Mark orphaned paused session as failed and release its worker slot"
    >
      {busy ? "Recovering…" : "↺ Recover"}
    </button>
  );
}
