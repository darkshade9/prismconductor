import { useEffect, useMemo, useState } from "react";
import {
  CreatePool,
  ListWorkspaces,
  SavePool,
} from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { noAutoCorrect } from "../lib/inputs";
import { ModelSelect } from "./ModelSelect";
import { useModelsStore } from "../stores/modelsStore";

type Role = "plan" | "work" | "orchestrator" | "architect";
type Scope = "shared" | "workspace";

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
  {
    value: "architect",
    label: "Architect",
    help: "Peer-agent consultant: auto-answers technical questions from implementers so the human user is not interrupted. Defaults to capacity 1.",
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
  const [scope, setScope] = useState<Scope>(((initial?.scope as Scope) || "shared") as Scope);
  const [scopeWorkspaceID, setScopeWorkspaceID] = useState<string>(initial?.workspace_id ?? "");
  const [workspaces, setWorkspaces] = useState<types.Workspace[]>([]);
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
  const [budgetOpen, setBudgetOpen] = useState(false);
  const [maxTurnsStr, setMaxTurnsStr] = useState<string>(
    initial?.max_turns != null ? String(initial.max_turns) : "",
  );
  const [maxInputTokensStr, setMaxInputTokensStr] = useState<string>(
    initial?.max_input_tokens != null ? String(initial.max_input_tokens) : "",
  );
  // bash_timeout is nanoseconds in JSON (time.Duration); UI shows seconds.
  const [bashTimeoutStr, setBashTimeoutStr] = useState<string>(
    initial?.bash_timeout != null ? String(Math.round((initial.bash_timeout as number) / 1e9)) : "",
  );
  // output_cap is bytes in JSON; UI shows KB.
  const [outputCapKBStr, setOutputCapKBStr] = useState<string>(
    initial?.output_cap != null ? String(Math.round((initial.output_cap as number) / 1024)) : "",
  );
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [saveErr, setSaveErr] = useState<string>("");

  const providerInfo = useMemo(
    () => providers.find((p) => p.kind === providerKind),
    [providers, providerKind],
  );

  const resolvedEndpoint = endpoint || (providerInfo?.default_endpoint ?? "");
  const fetchModels = useModelsStore((s) => s.fetch);
  const invalidateModels = useModelsStore((s) => s.invalidate);
  const modelEntry = useModelsStore((s) => s.getEntry(providerKind, resolvedEndpoint));
  const probing = modelEntry?.loading ?? false;

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

  // Disable Save when workspace-specific scope is chosen but no workspace selected (q3).
  const workspaceScopeBlock = scope === "workspace" && !scopeWorkspaceID;

  useEffect(() => {
    ListWorkspaces().then((ws) => setWorkspaces(ws ?? [])).catch(() => {});
  }, []);

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

  // Auto-probe when provider changes.
  useEffect(() => {
    if (!providerInfo) return;
    fetchModels(providerKind, resolvedEndpoint, apiKey).catch(() => {});
  }, [providerKind]);

  async function onEndpointBlur() {
    if (!providerInfo) return;
    try {
      await fetchModels(providerKind, resolvedEndpoint, apiKey);
    } catch {
      // Silent — UI keeps the model field as free-text.
    }
  }

  async function onTestConnection() {
    setTestResult(null);
    try {
      invalidateModels(providerKind, resolvedEndpoint);
      const list = await fetchModels(providerKind, resolvedEndpoint, apiKey);
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
    if (orchestratorBlock || workspaceScopeBlock) return;
    setBusy(true);
    setSaveErr("");
    try {
      const finalName = name.trim() || `${providerKind}-${modelTail(model) || "pool"}`;
      // Orchestrator runs are per-call HTTP, capacity is irrelevant. Persist 1.
      const finalCapacity = role === "orchestrator" || role === "architect" ? 1 : capacity;
      const parsedTemp = temperatureStr.trim() === "" ? undefined : parseFloat(temperatureStr);
      const parsedMaxTurns = maxTurnsStr.trim() === "" ? undefined : parseInt(maxTurnsStr, 10);
      const parsedMaxInputTokens = maxInputTokensStr.trim() === "" ? undefined : parseInt(maxInputTokensStr, 10);
      const parsedBashTimeoutSec = bashTimeoutStr.trim() === "" ? undefined : parseInt(bashTimeoutStr, 10);
      const parsedOutputCapKB = outputCapKBStr.trim() === "" ? undefined : parseInt(outputCapKBStr, 10);
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
        max_turns: (parsedMaxTurns != null && !isNaN(parsedMaxTurns)) ? parsedMaxTurns : undefined,
        max_input_tokens: (parsedMaxInputTokens != null && !isNaN(parsedMaxInputTokens)) ? parsedMaxInputTokens : undefined,
        bash_timeout: (parsedBashTimeoutSec != null && !isNaN(parsedBashTimeoutSec)) ? parsedBashTimeoutSec * 1e9 : undefined,
        output_cap: (parsedOutputCapKB != null && !isNaN(parsedOutputCapKB)) ? parsedOutputCapKB * 1024 : undefined,
        scope,
        workspace_id: scope === "workspace" ? scopeWorkspaceID : "",
      });
      if (initial) {
        await SavePool(pool);
      } else {
        await CreatePool(pool);
      }
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
            <div className="text-xs text-slate-500 mb-1">Scope</div>
            <div className="flex items-center gap-4">
              {(["shared", "workspace"] as Scope[]).map((s) => (
                <label key={s} className="flex items-center gap-1.5 text-xs text-slate-300 cursor-pointer">
                  <input
                    type="radio"
                    name="pool-scope"
                    value={s}
                    checked={scope === s}
                    onChange={() => {
                      setScope(s);
                      if (s === "shared") setScopeWorkspaceID("");
                    }}
                  />
                  {s === "shared" ? "Shared (all workspaces)" : "Workspace-specific"}
                </label>
              ))}
            </div>
            {scope === "workspace" && (
              <div className="mt-2">
                <select
                  value={scopeWorkspaceID}
                  onChange={(e) => setScopeWorkspaceID(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 text-xs"
                >
                  <option value="">— pick a workspace —</option>
                  {workspaces.map((ws) => (
                    <option key={ws.id} value={ws.id}>
                      {ws.display_name || ws.id}
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>

          <div>
            <div className="text-xs text-slate-500 mb-1">Provider</div>
            <select
              value={providerKind}
              onChange={(e) => {
                setProviderKind(e.target.value);
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
                {...noAutoCorrect}
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
                {...noAutoCorrect}
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
            <ModelSelect
              provider={providerKind}
              endpoint={resolvedEndpoint}
              apiKey={apiKey}
              value={model}
              onChange={setModel}
            />
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

          {role !== "orchestrator" && role !== "architect" && (
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
              {...noAutoCorrect}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={`${providerKind}-${modelTail(model) || "pool"}`}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
            />
          </div>

          <div>
            <div className="text-xs text-slate-500 mb-1">Temperature (optional)</div>
            <input
              {...noAutoCorrect}
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

          <div>
            <button
              type="button"
              onClick={() => setBudgetOpen((o) => !o)}
              className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-200"
            >
              <span>{budgetOpen ? "▾" : "▸"}</span>
              <span>Budget overrides</span>
              {(maxTurnsStr || maxInputTokensStr || bashTimeoutStr || outputCapKBStr) && (
                <span className="ml-1 text-[10px] text-amber-400">●</span>
              )}
            </button>
            {budgetOpen && (
              <div className="mt-2 space-y-2 pl-3 border-l border-slate-700">
                <div className="text-[11px] text-slate-500">
                  Leave any field empty to use the global default (120 turns / 1M tokens / 300s / 50KB).
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <div className="text-xs text-slate-500 mb-1">Max turns</div>
                    <input
                      {...noAutoCorrect}
                      type="number"
                      min={1}
                      step={1}
                      value={maxTurnsStr}
                      onChange={(e) => setMaxTurnsStr(e.target.value)}
                      placeholder="120"
                      className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
                    />
                  </div>
                  <div>
                    <div className="text-xs text-slate-500 mb-1">Max input tokens</div>
                    <input
                      {...noAutoCorrect}
                      type="number"
                      min={1}
                      step={1}
                      value={maxInputTokensStr}
                      onChange={(e) => setMaxInputTokensStr(e.target.value)}
                      placeholder="1000000"
                      className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
                    />
                  </div>
                  <div>
                    <div className="text-xs text-slate-500 mb-1">Bash timeout (seconds)</div>
                    <input
                      {...noAutoCorrect}
                      type="number"
                      min={1}
                      step={1}
                      value={bashTimeoutStr}
                      onChange={(e) => setBashTimeoutStr(e.target.value)}
                      placeholder="300"
                      className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
                    />
                  </div>
                  <div>
                    <div className="text-xs text-slate-500 mb-1">Output cap (KB)</div>
                    <input
                      {...noAutoCorrect}
                      type="number"
                      min={1}
                      step={1}
                      value={outputCapKBStr}
                      onChange={(e) => setOutputCapKBStr(e.target.value)}
                      placeholder="50"
                      className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
                    />
                  </div>
                </div>
              </div>
            )}
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

          {workspaceScopeBlock && (
            <div className="text-xs text-amber-300">
              Select a workspace to save a workspace-specific pool.
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
            disabled={busy || orchestratorBlock || workspaceScopeBlock}
            className="text-xs px-3 py-1 bg-slate-700 hover:bg-slate-600 text-slate-100 rounded disabled:opacity-50"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
