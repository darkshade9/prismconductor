import { LabelsPanel } from "./LabelsPanel";

export function LabelsModal({
  open,
  onClose,
  workspaceID,
  initialNewName,
}: {
  open: boolean;
  onClose: () => void;
  workspaceID: string;
  initialNewName?: string;
}) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
      <div className="w-[640px] max-h-[80vh] bg-slate-900 border border-slate-700 rounded-lg flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
          <div className="text-slate-200">Labels</div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">
            ✕
          </button>
        </div>
        <div className="flex-1 p-4 overflow-y-auto">
          <LabelsPanel workspaceID={workspaceID} initialNewName={initialNewName} />
        </div>
      </div>
    </div>
  );
}
