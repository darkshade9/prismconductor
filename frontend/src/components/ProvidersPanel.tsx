import { useEffect, useState } from "react";
import {
  DeleteProviderEntity,
  ListProviderEntities,
  ListProviders,
  ProbeProviderModels,
  SaveProviderEntity,
} from "../../wailsjs/go/main/App";
import { main, types } from "../../wailsjs/go/models";
import { noAutoCorrect } from "../lib/inputs";

export function ProvidersPanel() {
  const [providers, setProviders] = useState<types.ProviderEntity[]>([]);
  const [driverKinds, setDriverKinds] = useState<main.ProviderInfo[]>([]);
  const [editing, setEditing] = useState<types.ProviderEntity | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleteErr, setDeleteErr] = useState<Record<string, string>>({});

  async function refresh() {
    try {
      const [pv, dk] = await Promise.all([ListProviderEntities(), ListProviders()]);
      setProviders(pv ?? []);
      setDriverKinds(dk ?? []);
    } catch (err) {
      console.error("ProvidersPanel refresh", err);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const driverByKind = new Map(driverKinds.map((d) => [d.kind, d]));

  async function onDelete(p: types.ProviderEntity) {
    setDeleteErr((s) => ({ ...s, [p.id]: "" }));
    try {
      await DeleteProviderEntity(p.id);
      await refresh();
    } catch (err: any) {
      const msg = String(err?.message ?? err ?? "");
      const friendly = msg.includes("referenced by") ? msg : (msg || "Delete failed.");
      setDeleteErr((s) => ({ ...s, [p.id]: friendly }));
    }
  }

  return (
    <div className="space-y-3 text-sm">
      {providers.length === 0 && (
        <div className="text-slate-500">
          No provider credentials saved. Add one to share credentials across pools.
        </div>
      )}

      {providers.map((p) => {
        const driver = driverByKind.get(p.kind);
        return (
          <div key={p.id} className="border border-slate-800 rounded p-2 bg-slate-950/40">
            <div className="flex items-center gap-2">
              <div className="flex-1 min-w-0">
                <div className="text-slate-100 font-mono text-xs flex items-center gap-2">
                  <span>{p.name}</span>
                  <span className="text-slate-500">({driver?.display_name ?? p.kind})</span>
                </div>
                <div className="text-slate-500 text-xs">
                  {p.endpoint || driver?.default_endpoint || "—"}
                  {p.api_key ? " · key set" : ""}
                </div>
              </div>
              <button
                className="text-xs text-slate-300 hover:text-slate-100 px-2 py-1 border border-slate-700 rounded"
                onClick={() => setEditing(p)}
              >
                Edit
              </button>
              <button
                className="text-xs text-rose-300 hover:text-rose-200 px-2 py-1 border border-rose-900 rounded"
                onClick={() => onDelete(p)}
              >
                Remove
              </button>
            </div>
            {deleteErr[p.id] && (
              <div className="mt-1 text-xs text-rose-300">{deleteErr[p.id]}</div>
            )}
          </div>
        );
      })}

      <button
        className="text-xs text-slate-200 hover:text-slate-50 px-3 py-1 border border-slate-700 rounded"
        onClick={() => setAdding(true)}
      >
        + Add provider
      </button>

      {(editing || adding) && (
        <ProviderEditModal
          driverKinds={driverKinds}
          initial={editing}
          onClose={() => {
            setEditing(null);
            setAdding(false);
          }}
          onSaved={async () => {
            setEditing(null);
            setAdding(false);
            await refresh();
          }}
        />
      )}
    </div>
  );
}

export function ProviderEditModal({
  driverKinds,
  initial,
  initialKind,
  onClose,
  onSaved,
}: {
  driverKinds: main.ProviderInfo[];
  initial: types.ProviderEntity | null;
  initialKind?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const fallbackKind = initialKind ?? driverKinds[0]?.kind ?? "claude";
  const [kind, setKind] = useState<string>(initial?.kind ?? fallbackKind);
  const [name, setName] = useState<string>(initial?.name ?? "");
  const [endpoint, setEndpoint] = useState<string>(initial?.endpoint ?? "");
  const [apiKey, setApiKey] = useState<string>(initial?.api_key ?? "");
  const [busy, setBusy] = useState(false);
  const [saveErr, setSaveErr] = useState<string>("");
  const [testStatus, setTestStatus] = useState<"idle" | "testing" | "ok" | "err">("idle");
  const [testMsg, setTestMsg] = useState<string>("");

  const driver = driverKinds.find((d) => d.kind === kind);
  const showEndpoint = !!driver?.default_endpoint;

  async function onTest() {
    setTestStatus("testing");
    setTestMsg("");
    try {
      const models = await ProbeProviderModels(kind as any, endpoint.trim(), apiKey);
      setTestStatus("ok");
      setTestMsg(`Connected — ${models.length} model(s) available`);
    } catch (err: any) {
      setTestStatus("err");
      setTestMsg(String(err?.message ?? err ?? "connection failed"));
    }
  }

  async function onSave() {
    setBusy(true);
    setSaveErr("");
    try {
      const finalName = name.trim() || `${kind}-default`;
      const entity = types.ProviderEntity.createFrom({
        id: initial?.id ?? "",
        name: finalName,
        kind,
        endpoint: endpoint.trim(),
        api_key: apiKey,
        created_at: initial?.created_at ?? new Date().toISOString(),
      });
      await SaveProviderEntity(entity);
      onSaved();
    } catch (err: any) {
      setSaveErr(String(err?.message ?? err ?? "save failed"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]">
      <div className="w-[480px] bg-slate-900 border border-slate-700 rounded-lg p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div className="text-slate-200 font-medium">
            {initial ? "Edit provider" : "Add provider"}
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200">✕</button>
        </div>

        <div className="space-y-3 text-sm">
          <div>
            <div className="text-xs text-slate-500 mb-1">Kind</div>
            <select
              value={kind}
              onChange={(e) => { setKind(e.target.value); setTestStatus("idle"); setTestMsg(""); }}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100"
            >
              {driverKinds.map((d) => (
                <option key={d.kind} value={d.kind}>
                  {d.display_name}
                </option>
              ))}
            </select>
          </div>

          {kind === "codex" && (
            <div className="text-xs text-amber-300/80 bg-amber-950/30 border border-amber-900/40 rounded px-2 py-1.5">
              Codex uses a local CLI and browser OAuth — no endpoint or API key needed.
              Run <code className="font-mono">codex login</code> before adding this provider.
            </div>
          )}

          {showEndpoint && (
            <div>
              <div className="text-xs text-slate-500 mb-1">Endpoint URL</div>
              <input
                {...noAutoCorrect}
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
                placeholder={driver!.default_endpoint}
                className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
              />
            </div>
          )}

          {driver?.needs_api_key && (
            <div>
              <div className="text-xs text-slate-500 mb-1 flex items-center gap-2">
                API key (optional)
                {driver.api_key_help_url && (
                  <a
                    href={driver.api_key_help_url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-blue-400 hover:text-blue-300"
                  >
                    Get your API key →
                  </a>
                )}
              </div>
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
            <div className="text-xs text-slate-500 mb-1">Name</div>
            <input
              {...noAutoCorrect}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={`${kind}-default`}
              className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-slate-100 font-mono text-xs"
            />
          </div>

          {testStatus === "ok" && (
            <div className="text-xs text-emerald-400">{testMsg}</div>
          )}
          {testStatus === "err" && (
            <div className="text-xs text-rose-300">{testMsg}</div>
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
            onClick={onTest}
            disabled={testStatus === "testing"}
            className="text-xs px-3 py-1 border border-slate-700 text-slate-300 hover:text-slate-100 rounded disabled:opacity-50"
          >
            {testStatus === "testing" ? "Testing…" : "Test connection"}
          </button>
          <button
            onClick={onSave}
            disabled={busy}
            className="text-xs px-3 py-1 bg-slate-700 hover:bg-slate-600 text-slate-100 rounded disabled:opacity-50"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
