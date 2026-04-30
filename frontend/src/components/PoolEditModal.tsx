import { useEffect, useMemo, useState } from "react";
import {
  ProbeProviderModels,
  SavePool,
} from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";

type Role = "plan" | "work" | "orchestrator";

const ROLE_OPTIONS: { value: Role; label: string; help: string }[] = [
  {
    value: "plan",
    label: "Plan",
    help: "Runs /conductor-plan against this pool's model.",
  },
  {
    value: "work",
    label: "Work",
    help: "Runs /conductor-execute (the heavy implementation pass).",
  },
  {
    value: "orchestrator",
    label: "Orchestrator",
    help: "Runs the rank+deps backlog pass. At most one enabled.",
  },
];

type Props = {
  providers: main.ProviderInfo[];
  existingPools: types.Pool[];
  initial: types.Pool | null;
  onClose: () => void;
  onSaved: () => void;
};

function modelTail(model: string): string {
  if (!model) return "";
  const last = model.split("/").pop() ?? model;
  return last.split(":")[0] ?? last;
}

export function PoolEditModal({
  providers,
  existingPools,
  initial,
  onClose,
  onSaved,
}: Props) {
  const fallbackProvider = (providers[0]?.kind ?? "claude") as string;
  const [role, setRole] = useState<Role>(((initial?.role as Role) || "work") as Role);
  const [providerKind, setProviderKind] = useState<string>(
    initial?.provider ?? fallbackProvider,
  );
  const [name, setName] = useState<string>(initial?.name ?? "");
  const [endpoint, setEndpoint] = useState<string>(initial?.endpoint ?? "");
  const [apiKey, setApiKey] = useState<string>(initial?.api_key ?? "");
  const [model, setModel] = useState<string>(initial?.model ?? "");
  const [capacity, setCapacity] = useState<number>(initial?.capacity ?? 1);
  const [enabled, setEnabled] = useState<boolean>(initial?.enabled ?? true);
  const [temperatureStr, setTemperatureStr] = useState<string>(
    initial?.temperature != null ? String(initial.temperature) : "",
  );
  const [models, setModels] = useState<string[]>([]);
  const [probing, setProbing] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [saveErr, setSaveErr] = useState<string>("");

  const providerInfo = useMemo(
    () => providers.find((p) => p.kind === providerKind),
    [providers, providerKind],
  );

  // Disable Save when picking role=orchestrator while another enabled
  // orchestrator pool already exists (and isn't the row we're editing).
  const orchestratorBlock = useMemo(() => {
    if (role !== "orchestrator") return false;
    return existingPools.some(
      (p) =>
        p.role === "orchestrator" &&
        p.enabled &&
        p.id !== (initial?.id ?? ""),
    );
  }, [role, existingPools, initial]);

  useEffect(() => {
    if (initial) return;
    if (!providerInfo) return;
    setEndpoint((current) => (current ? current : providerInfo.default_endpoint));
  }, [providerInfo, initial]);

  useEffect(() => {
    if (!name && model) {
      setName(`${providerKind}-${modelTail(model)}`);
    }
  }, [model, providerKind]);

  async function probe(): Promise<string[]> {
    if (!providerInfo) return [];
    setProbing(true);
    try {
      const list = await ProbeProviderModels(
        providerKind,
        endpoint || providerInfo.default_endpoint,
        apiKey,
      );
      setModels(list ?? []);
      return list ?? [];
    } catch (err) {
      setModels([]);
      throw err;
    } finally {
      setProbing(false);
    }
  }

  async function onEndpointBlur() {
    if (!providerInfo) return;
    try {
      await probe();
    } catch {
      // Silent — UI keeps the model field as free-text.
    }
  }

  async function onTestConnection() {
    setTestResult(null);
    try {
      const list = await probe();
      const sample = list.slice(0, 3).join(", ");
      setTestResult({
        ok: true,
        msg: list.length === 0
          ? "Connected (no models reported)."
          : `Loaded ${list.length} model${list.length === 1 ? "" : "s"}${sample ? `: ${sample}` : ""}`,
      });
    } catch (err: any) {
      setTestResult({ ok: false, msg: String(err?.message ?? err ?? "probe failed") });
    }
  }

  async function onSave() {
    if (!providerInfo) return;
    if (orchestratorBlock) return;
    setBusy(true);
    setSaveErr("");
    try {
      const finalName = name.trim() || `${providerKind}-${modelTail(model) || "pool"}`;
      // Orchestrator runs are per-call HTTP, capacity is irrelevant. Persist 1.
      const finalCapacity = role === "orchestrator" ? 1 : capacity;
      const parsedTemp = temperatureStr.trim() === "" ? undefined : parseFloat(temperatureStr);
      const pool = types.Pool.createFrom({
        id: initial?.id ?? "",
        name: finalName,
        provider: providerKind,
        endpoint: endpoint.trim(),
        model: model.trim(),
        capacity: finalCapacity,
        enabled,
        api_key: apiKey,
        role,
        created_at: initial?.created_at ?? new Date().toISOString(),
        temperature: (parsedTemp != null && !isNaN(parsedTemp)) ? parsedTemp : undefined,
      });
      await SavePool(pool);
      onSaved();
    } catch (err: any) {
      setSaveErr(String(err?.message ?? err ?? "save failed"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]">
      <div className="w-[520px] bg-slate-900 border border-slate-700 rounded-lg p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div className="text-slate-200 font-medium">
            {initial ? "Edit pool" : "Add pool"}
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>

        <div className="space-y-3 text-sm">
          <div>
            <div className="text-xs text-slate-500 mb-1">Role</div>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as Role)}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100"
            >
              {ROLE_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
            <div className="text-[11px] text-slate-500 mt-1">
              {ROLE_OPTIONS.find((o) => o.value === role)?.help}
            </div>
          </div>

          <div>
            <div className="text-xs text-slate-500 mb-1">Provider</div>
            <select
              value={providerKind}
              onChange={(e) => {
                setProviderKind(e.target.value);
                setModels([]);
                setTestResult(null);
                const info = providers.find((p) => p.kind === e.target.value);
                if (info && !initial) setEndpoint(info.default_endpoint);
              }}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100"
            >
              {providers.map((p) => (
                <option key={p.kind} value={p.kind}>
                  {p.display_name}
                </option>
              ))}
            </select>
          </div>

          {providerInfo && providerKind !== "claude" && (
            <div>
              <div className="text-xs text-slate-500 mb-1">Endpoint URL</div>
              <input
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
                onBlur={onEndpointBlur}
                placeholder={providerInfo.default_endpoint}
                className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
              />
            </div>
          )}

          {providerInfo?.needs_api_key && (
            <div>
              <div className="text-xs text-slate-500 mb-1">API key (optional)</div>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="leave empty to use the provider's environment variable"
                className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
              />
            </div>
          )}

          <div>
            <div className="text-xs text-slate-500 mb-1">Model</div>
            {models.length > 0 ? (
              <select
                value={model}
                onChange={(e) => setModel(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100"
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
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="model id (free text)"
                className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
              />
            )}
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={probing || !providerInfo}
              onClick={onTestConnection}
              className="text-xs px-2 py-1 border border-slate-700 rounded text-slate-200 hover:text-slate-50 disabled:opacity-50"
            >
              {probing ? "Testing…" : "Test connection"}
            </button>
            {testResult && (
              <span
                className={
                  "text-xs " + (testResult.ok ? "text-emerald-300" : "text-rose-300")
                }
              >
                {testResult.msg}
              </span>
            )}
          </div>

          {role !== "orchestrator" && (
            <div>
              <div className="text-xs text-slate-500 mb-1">Capacity (1–10)</div>
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  min={1}
                  max={10}
                  value={capacity}
                  onChange={(e) => setCapacity(parseInt(e.target.value, 10))}
                  className="flex-1"
                />
                <span className="font-mono text-slate-200 w-6 text-right">{capacity}</span>
              </div>
            </div>
          )}

          <div>
            <div className="text-xs text-slate-500 mb-1">Name</div>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={`${providerKind}-${modelTail(model) || "pool"}`}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
            />
          </div>

          <div>
            <div className="text-xs text-slate-500 mb-1">Temperature (optional)</div>
            <input
              type="number"
              min={0}
              max={2}
              step={0.01}
              value={temperatureStr}
              onChange={(e) => setTemperatureStr(e.target.value)}
              placeholder="leave empty to use provider default"
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
            />
            <div className="text-[11px] text-slate-500 mt-1">
              Leave empty for models that reject explicit temperature (e.g. gpt-5-codex). Set 0.0 for deterministic output.
            </div>
          </div>

          <label className="flex items-center gap-2 text-xs text-slate-300">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            Enabled
          </label>

          {orchestratorBlock && (
            <div className="text-xs text-rose-300">
              Only one orchestrator pool can be enabled at a time. Disable the
              existing one first.
            </div>
          )}

          {saveErr && <div className="text-xs text-rose-300">{saveErr}</div>}
        </div>

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-slate-800">
          <button
            onClick={onClose}
            className="text-xs px-3 py-1 text-slate-400 hover:text-slate-200"
          >
            Cancel
          </button>
          <button
            onClick={onSave}
            disabled={busy || orchestratorBlock}
            className="text-xs px-3 py-1 bg-slate-700 hover:bg-slate-600 text-slate-100 rounded disabled:opacity-50"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
