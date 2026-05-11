import { noAutoCorrect } from "../lib/inputs";
import { useModelsStore } from "../stores/modelsStore";

type Props = {
  provider: string;
  endpoint: string;
  apiKey: string;
  value: string;
  onChange: (v: string) => void;
};

export function ModelSelect({ provider, endpoint, apiKey, value, onChange }: Props) {
  const entry = useModelsStore((s) => s.getEntry(provider, endpoint));
  const { invalidate, fetch } = useModelsStore();

  const models = entry?.models ?? [];
  const loading = entry?.loading ?? false;
  const error = entry?.error ?? null;

  async function handleRefresh() {
    invalidate(provider, endpoint);
    try {
      await fetch(provider, endpoint, apiKey);
    } catch {
      // error stored in the cache entry
    }
  }

  return (
    <div className="flex items-center gap-1">
      {models.length > 0 ? (
        <select
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100"
        >
          <option value="">— pick a model —</option>
          {models.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      ) : (
        <input
          {...noAutoCorrect}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={loading ? "Loading models…" : "model id (free text)"}
          disabled={loading}
          className="flex-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs disabled:opacity-60"
        />
      )}
      <button
        type="button"
        disabled={loading}
        onClick={handleRefresh}
        title="Refresh model list"
        className="shrink-0 text-slate-400 hover:text-slate-200 disabled:opacity-50 px-1 text-sm"
      >
        {loading ? "…" : "↻"}
      </button>
      {!loading && error && (
        <span className="text-xs text-amber-400" title={error}>
          ⚠
        </span>
      )}
    </div>
  );
}
