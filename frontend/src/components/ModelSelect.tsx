import { llm } from "../../wailsjs/go/models";
import { noAutoCorrect } from "../lib/inputs";
import { useModelsStore } from "../stores/modelsStore";
import { fitForRole, fitOptionPrefix, fitOptionDescriptor, sortModelsByFit } from "./fitGlyph";

type Props = {
  provider: string;
  endpoint: string;
  apiKey: string;
  value: string;
  onChange: (v: string) => void;
  role?: string;
  hints?: Record<string, llm.ModelHint | null>;
};

function fmtCtx(n: number): string {
  if (!n) return "—";
  if (n >= 1000000) return `${(n / 1000000).toFixed(n % 1000000 === 0 ? 0 : 1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

function hintTooltip(hint: llm.ModelHint, role: string): string {
  const fit = fitForRole(hint, role);
  const toolLabel =
    hint.tool_support === "full"
      ? "Tools: full"
      : hint.tool_support === "partial"
      ? "Tools: partial"
      : "Tools: none";
  return [
    `Fit for ${role}: ${fit || "unknown"}`,
    toolLabel,
    hint.context_window ? `Context: ${fmtCtx(hint.context_window)} tokens` : null,
    hint.cost_tier ? `Cost tier: ${hint.cost_tier}` : null,
    hint.notes || null,
    `Source: ${hint.source || "bundled"}`,
    hint.updated_at ? `Updated: ${hint.updated_at}` : null,
  ]
    .filter(Boolean)
    .join("\n");
}

export function ModelSelect({ provider, endpoint, apiKey, value, onChange, role, hints }: Props) {
  const entry = useModelsStore((s) => s.getEntry(provider, endpoint));
  const { invalidate, fetch } = useModelsStore();

  const models = entry?.models ?? [];
  const loading = entry?.loading ?? false;
  const error = entry?.error ?? null;

  const displayModels =
    role && hints && models.length > 0 ? sortModelsByFit(models, hints, role) : models;

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
          {displayModels.map((m) => {
            const hint = role && hints ? (hints[m] ?? null) : null;
            const prefix = role && hints ? fitOptionPrefix(hint, role) : "";
            const descriptor = role && hints ? fitOptionDescriptor(hint, role) : "";
            const title =
              role && hint ? hintTooltip(hint, role) : undefined;
            return (
              <option key={m} value={m} title={title}>
                {prefix}{m}{descriptor}
              </option>
            );
          })}
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
