import { useEffect, useState } from "react";
import { useWorkspaceStore } from "../stores/workspaceStore";
import {
  GetSlackConfig,
  GetSlackStatus,
  SaveSlackConfig,
  SaveSlackCredentials,
  DisconnectSlack,
} from "../../wailsjs/go/main/App";

interface SlackEventRouting {
  plan_ready: boolean;
  blocked: boolean;
  completed: boolean;
  budget_alert: boolean;
  auto_archive: boolean;
}

interface SlackConfig {
  enabled: boolean;
  bot_token_ref?: string;
  app_level_token_ref?: string;
  app_id?: string;
  team_id?: string;
  team_name?: string;
  default_channel?: string;
  channel_map?: Record<string, string>;
  event_routing?: SlackEventRouting;
  user_map?: Record<string, string>;
  muted?: boolean;
}

interface SlackStatus {
  connected: boolean;
  team_name?: string;
  bot_user_id?: string;
  error?: string;
}

const DEFAULT_ROUTING: SlackEventRouting = {
  plan_ready: true,
  blocked: true,
  completed: false,
  budget_alert: true,
  auto_archive: false,
};

export function SlackIntegrationPanel() {
  const workspaceID = useWorkspaceStore((s) => s.selectedID) ?? "";

  const [cfg, setCfg] = useState<SlackConfig | null>(null);
  const [status, setStatus] = useState<SlackStatus>({ connected: false });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  // Credential form (tokens are write-only — we never show them back).
  const [botToken, setBotToken] = useState("");
  const [appToken, setAppToken] = useState("");
  const [showTokenForm, setShowTokenForm] = useState(false);

  // Editable fields.
  const [defaultChannel, setDefaultChannel] = useState("");
  const [muted, setMuted] = useState(false);
  const [routing, setRouting] = useState<SlackEventRouting>(DEFAULT_ROUTING);
  const [userMapRaw, setUserMapRaw] = useState("");

  useEffect(() => {
    if (!workspaceID) return;
    setLoading(true);
    Promise.all([
      GetSlackConfig(workspaceID).catch(() => null),
      GetSlackStatus(workspaceID).catch(() => ({ connected: false })),
    ]).then(([c, s]) => {
      if (c) {
        setCfg(c as SlackConfig);
        setDefaultChannel((c as SlackConfig).default_channel ?? "");
        setMuted((c as SlackConfig).muted ?? false);
        setRouting((c as SlackConfig).event_routing ?? DEFAULT_ROUTING);
        const um = (c as SlackConfig).user_map ?? {};
        setUserMapRaw(
          Object.entries(um)
            .map(([k, v]) => `${k}:${v}`)
            .join("\n")
        );
      }
      setStatus(s as SlackStatus);
      setLoading(false);
    });
  }, [workspaceID]);

  function parseUserMap(raw: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const line of raw.split("\n")) {
      const [k, v] = line.trim().split(":");
      if (k && v) out[k.trim()] = v.trim();
    }
    return out;
  }

  async function saveCredentials() {
    if (!botToken || !appToken) {
      setMsg("Both bot token and app-level token are required.");
      return;
    }
    setSaving(true);
    setMsg(null);
    try {
      await SaveSlackCredentials(workspaceID, botToken, appToken);
      setBotToken("");
      setAppToken("");
      setShowTokenForm(false);
      setMsg("Credentials saved to keychain.");
      // Reload config so refs are updated.
      const c = await GetSlackConfig(workspaceID).catch(() => null);
      if (c) setCfg(c as SlackConfig);
    } catch (e: any) {
      setMsg(String(e?.message ?? e));
    } finally {
      setSaving(false);
    }
  }

  async function saveConfig() {
    setSaving(true);
    setMsg(null);
    const newCfg: SlackConfig = {
      ...(cfg ?? {}),
      enabled: true,
      default_channel: defaultChannel,
      muted,
      event_routing: routing,
      user_map: parseUserMap(userMapRaw),
    };
    try {
      await SaveSlackConfig(workspaceID, newCfg as any);
      setCfg(newCfg);
      const s = await GetSlackStatus(workspaceID).catch(() => ({ connected: false }));
      setStatus(s as SlackStatus);
      setMsg("Saved.");
    } catch (e: any) {
      setMsg(String(e?.message ?? e));
    } finally {
      setSaving(false);
    }
  }

  async function disconnect() {
    if (!confirm("Disconnect Slack? This will remove stored credentials from the keychain.")) return;
    setSaving(true);
    try {
      await DisconnectSlack(workspaceID);
      setCfg(null);
      setStatus({ connected: false });
      setDefaultChannel("");
      setMuted(false);
      setRouting(DEFAULT_ROUTING);
      setUserMapRaw("");
      setMsg("Disconnected.");
    } catch (e: any) {
      setMsg(String(e?.message ?? e));
    } finally {
      setSaving(false);
    }
  }

  if (!workspaceID) {
    return (
      <div className="text-slate-500 text-sm">Select a workspace to configure Slack.</div>
    );
  }

  if (loading) {
    return <div className="text-slate-500 text-sm">Loading…</div>;
  }

  const isConnected = status.connected;
  const hasCredentials = !!(cfg?.bot_token_ref && cfg?.app_level_token_ref);

  return (
    <div className="space-y-5 text-sm">
      {/* Connection status */}
      <div className="flex items-center gap-3">
        <span
          className={
            "w-2 h-2 rounded-full " + (isConnected ? "bg-emerald-500" : "bg-slate-600")
          }
        />
        <span className="text-slate-300">
          {isConnected
            ? `Connected${status.team_name ? ` to ${status.team_name}` : ""}${status.bot_user_id ? ` (bot: ${status.bot_user_id})` : ""}`
            : hasCredentials
            ? "Credentials saved — start the app to connect."
            : "Not configured"}
        </span>
        {hasCredentials && (
          <button
            onClick={() => setShowTokenForm((v) => !v)}
            className="ml-auto text-xs text-slate-400 hover:text-slate-200 underline"
          >
            Replace tokens
          </button>
        )}
      </div>

      {/* Token form — shown when not configured or user clicks Replace */}
      {(!hasCredentials || showTokenForm) && (
        <div className="bg-slate-800 rounded p-3 space-y-3 border border-slate-700">
          <div className="text-slate-300 font-medium">Slack credentials</div>
          <p className="text-slate-500 text-xs">
            Create a Slack App with Socket Mode enabled. Provide a bot token (
            <code className="text-slate-300">xoxb-</code>) and an app-level token (
            <code className="text-slate-300">xapp-</code>).
          </p>
          <label className="block">
            <span className="text-slate-400 text-xs">Bot token (xoxb-…)</span>
            <input
              type="password"
              value={botToken}
              onChange={(e) => setBotToken(e.target.value)}
              placeholder="xoxb-…"
              className="mt-1 w-full bg-slate-900 border border-slate-600 rounded px-2 py-1 text-slate-200 text-xs font-mono"
            />
          </label>
          <label className="block">
            <span className="text-slate-400 text-xs">App-level token (xapp-…)</span>
            <input
              type="password"
              value={appToken}
              onChange={(e) => setAppToken(e.target.value)}
              placeholder="xapp-…"
              className="mt-1 w-full bg-slate-900 border border-slate-600 rounded px-2 py-1 text-slate-200 text-xs font-mono"
            />
          </label>
          <div className="flex gap-2">
            <button
              onClick={saveCredentials}
              disabled={saving}
              className="px-3 py-1 bg-sky-700 hover:bg-sky-600 rounded disabled:opacity-50 text-xs"
            >
              Save to keychain
            </button>
            {showTokenForm && (
              <button
                onClick={() => setShowTokenForm(false)}
                className="px-3 py-1 text-slate-400 hover:text-slate-200 text-xs"
              >
                Cancel
              </button>
            )}
          </div>
        </div>
      )}

      {/* Config — shown once credentials exist */}
      {hasCredentials && (
        <>
          <div>
            <div className="text-slate-300 font-medium mb-1">Default channel</div>
            <input
              type="text"
              value={defaultChannel}
              onChange={(e) => setDefaultChannel(e.target.value)}
              placeholder="#conductor"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
            />
            <p className="text-slate-500 text-xs mt-1">
              Fallback channel for notifications when no channel mapping matches.
            </p>
          </div>

          <div>
            <div className="text-slate-300 font-medium mb-2">Notifications</div>
            <div className="space-y-1">
              {(
                [
                  ["plan_ready", "Plan ready for approval"],
                  ["blocked", "Session blocked or failed"],
                  ["completed", "Session completed"],
                  ["budget_alert", "Budget cap alert"],
                  ["auto_archive", "Auto-archive sweep summary"],
                ] as [keyof SlackEventRouting, string][]
              ).map(([key, label]) => (
                <label key={key} className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={routing[key]}
                    onChange={(e) =>
                      setRouting((r) => ({ ...r, [key]: e.target.checked }))
                    }
                    className="accent-sky-500"
                  />
                  <span className="text-slate-400">{label}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={muted}
                onChange={(e) => setMuted(e.target.checked)}
                className="accent-sky-500"
              />
              <span className="text-slate-300">Mute all Slack notifications</span>
            </label>
          </div>

          <div>
            <div className="text-slate-300 font-medium mb-1">
              Authorized users{" "}
              <span className="text-slate-500 font-normal">(Slack user ID → permission)</span>
            </div>
            <textarea
              value={userMapRaw}
              onChange={(e) => setUserMapRaw(e.target.value)}
              rows={4}
              placeholder={"U0123456789:full\nU9876543210:read_only"}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 text-xs font-mono"
            />
            <p className="text-slate-500 text-xs mt-1">
              One entry per line: <code>slackUserID:full</code> or{" "}
              <code>slackUserID:read_only</code>. Mutating commands (plan/approve/cancel)
              require <code>full</code>.
            </p>
          </div>

          <div className="flex items-center gap-3 pt-2 border-t border-slate-800">
            <button
              onClick={saveConfig}
              disabled={saving}
              className="px-3 py-1 bg-emerald-700 hover:bg-emerald-600 rounded disabled:opacity-50"
            >
              Save
            </button>
            <button
              onClick={disconnect}
              disabled={saving}
              className="px-3 py-1 bg-red-900 hover:bg-red-800 rounded disabled:opacity-50"
            >
              Disconnect
            </button>
            {msg && <span className="text-xs text-slate-400">{msg}</span>}
          </div>
        </>
      )}

      {!hasCredentials && msg && (
        <div className="text-xs text-slate-400">{msg}</div>
      )}
    </div>
  );
}
