import { useEffect, useState } from "react";
import { GetOllamaConfig, RunOrchestrator, SetOllamaConfig } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";

export function OllamaPanel() {
  const [url, setUrl] = useState("");
  const [model, setModel] = useState("");
  const [available, setAvailable] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  async function refresh() {
    try {
      const cfg = await GetOllamaConfig();
      setUrl(cfg.url);
      setModel(cfg.model);
      setAvailable(cfg.available);
    } catch {
      setAvailable(false);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  async function save() {
    setBusy(true);
    setStatus(null);
    try {
      await SetOllamaConfig(new main.OllamaConfig({ url, model, available: false }));
      await refresh();
      setStatus("Saved.");
    } catch (e: any) {
      setStatus(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function rerank() {
    setBusy(true);
    setStatus(null);
    try {
      await RunOrchestrator();
      setStatus("Rank pass triggered.");
    } catch (e: any) {
      setStatus(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3 text-sm">
      <div className="flex items-center gap-2">
        <span className="text-slate-500">Status:</span>
        {available === null ? (
          <span className="text-slate-500">checking…</span>
        ) : available ? (
          <span className="text-emerald-400">● {model} available</span>
        ) : (
          <span className="text-amber-300">● not available — install model with <code>ollama pull {model}</code></span>
        )}
      </div>

      <label className="block">
        <div className="text-xs text-slate-500 mb-1">Endpoint URL</div>
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          placeholder="http://localhost:11434"
        />
      </label>
      <label className="block">
        <div className="text-xs text-slate-500 mb-1">Model</div>
        <input
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          placeholder="qwen2.5:14b-instruct"
        />
      </label>

      <div className="flex items-center gap-2 pt-2 border-t border-slate-800">
        <button
          onClick={save}
          disabled={busy}
          className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
        >
          Save
        </button>
        <button
          onClick={rerank}
          disabled={busy}
          className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50"
          title="Run rank+deps against the active goal now"
        >
          Re-rank now
        </button>
        {status && <span className="text-xs text-slate-400">{status}</span>}
      </div>
    </div>
  );
}
