import { useEffect, useState } from "react";
import { ListAvailableAgents, StartAgentSession } from "../../wailsjs/go/main/App";
import { useAgentTerminalStore, type AgentInfo } from "../stores/useAgentTerminalStore";

type Props = {
  workspaceID: string;
  onClose: () => void;
};

export function AgentSettingsPane({ workspaceID, onClose }: Props) {
  const setSession = useAgentTerminalStore((s) => s.setSession);
  const availableAgents = useAgentTerminalStore((s) => s.availableAgents);
  const setAvailableAgents = useAgentTerminalStore((s) => s.setAvailableAgents);

  const [selected, setSelected] = useState<AgentInfo | null>(null);
  const [customBin, setCustomBin] = useState("");
  const [args, setArgs] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Probe PATH for known agent binaries.
  useEffect(() => {
    ListAvailableAgents()
      .then((agents) => {
        const list = agents ?? [];
        setAvailableAgents(list);
        if (list.length > 0 && !selected) setSelected(list[0]);
      })
      .catch(() => {});
  }, []);

  async function handleStart() {
    const bin = customBin.trim() || selected?.binary || selected?.name || "";
    if (!bin || !workspaceID) return;
    const argList = args.trim() ? args.trim().split(/\s+/) : [];
    setBusy(true);
    setError("");
    try {
      const sess = await StartAgentSession(workspaceID, bin, argList, 220, 40);
      if (sess) {
        setSession(workspaceID, {
          workspace_id: sess.workspace_id,
          session_id: sess.session_id,
          agent_bin: sess.agent_bin,
          pid: sess.pid,
        });
      }
      onClose();
    } catch (e: unknown) {
      setError(String((e as { message?: string })?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="border-b border-slate-800 bg-slate-900/80 px-3 py-2 flex items-start gap-3 flex-wrap shrink-0">
      {/* Agent picker */}
      <div className="flex flex-col gap-1 min-w-40">
        <label className="text-[10px] text-slate-500 uppercase tracking-wide">agent</label>
        {availableAgents.length > 0 ? (
          <select
            value={selected?.binary ?? ""}
            onChange={(e) => {
              const found = availableAgents.find((a) => a.binary === e.target.value) ?? null;
              setSelected(found);
              setCustomBin("");
            }}
            className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 focus:outline-none focus:border-sky-600"
          >
            {availableAgents.map((a) => (
              <option key={a.binary} value={a.binary}>
                {a.name}
              </option>
            ))}
            <option value="">— custom —</option>
          </select>
        ) : (
          <span className="text-xs text-slate-500 italic">no agents found on PATH</span>
        )}
      </div>

      {/* Custom binary field */}
      <div className="flex flex-col gap-1 min-w-40">
        <label className="text-[10px] text-slate-500 uppercase tracking-wide">
          custom binary (overrides)
        </label>
        <input
          type="text"
          value={customBin}
          onChange={(e) => setCustomBin(e.target.value)}
          placeholder="e.g. /usr/local/bin/aider"
          className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 focus:outline-none focus:border-sky-600 placeholder:text-slate-600"
        />
      </div>

      {/* Args field */}
      <div className="flex flex-col gap-1 flex-1 min-w-40">
        <label className="text-[10px] text-slate-500 uppercase tracking-wide">
          extra args
        </label>
        <input
          type="text"
          value={args}
          onChange={(e) => setArgs(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleStart()}
          placeholder="e.g. --model sonnet"
          className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-xs text-slate-200 focus:outline-none focus:border-sky-600 placeholder:text-slate-600"
        />
      </div>

      {/* Actions */}
      <div className="flex items-end gap-2 pb-0.5">
        <button
          onClick={handleStart}
          disabled={busy || (!selected?.binary && !customBin.trim())}
          className="text-xs px-3 py-1.5 bg-emerald-800 hover:bg-emerald-700 disabled:opacity-40 rounded text-emerald-100"
        >
          {busy ? "starting…" : "start"}
        </button>
        <button
          onClick={onClose}
          className="text-xs px-2 py-1.5 border border-slate-700 text-slate-400 hover:text-slate-200 rounded"
        >
          cancel
        </button>
      </div>

      {error && (
        <div className="w-full text-xs text-red-400 mt-1">{error}</div>
      )}
    </div>
  );
}
