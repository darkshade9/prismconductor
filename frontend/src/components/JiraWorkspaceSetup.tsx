import { useState } from "react";
import { AddJiraWorkspace, TestJiraConnection } from "../../wailsjs/go/main/App";
import { useWorkspaceStore } from "../stores/workspaceStore";
import { noAutoCorrect } from "../lib/inputs";

const COLOR_PALETTE = ["#22c55e", "#06b6d4", "#a855f7", "#f59e0b", "#ef4444", "#ec4899", "#14b8a6", "#eab308"];

interface Props {
  onDone: () => void;
}

export function JiraWorkspaceSetup({ onDone }: Props) {
  const refresh = useWorkspaceStore((s) => s.refresh);

  const [id, setID] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [color, setColor] = useState(COLOR_PALETTE[2]);
  const [instanceURL, setInstanceURL] = useState("");
  const [email, setEmail] = useState("");
  const [apiToken, setAPIToken] = useState("");
  const [projectKey, setProjectKey] = useState("");
  const [jql, setJQL] = useState("");
  const [busy, setBusy] = useState(false);
  const [testStatus, setTestStatus] = useState<"idle" | "ok" | "error">("idle");
  const [testError, setTestError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  function normalizeURL(u: string): string {
    return u.replace(/\/$/, "").trim();
  }

  async function testConnection() {
    setBusy(true);
    setTestStatus("idle");
    setTestError(null);
    setError(null);
    try {
      await TestJiraConnection(normalizeURL(instanceURL), email, apiToken, projectKey);
      setTestStatus("ok");
    } catch (e: any) {
      setTestStatus("error");
      setTestError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!id || !instanceURL || !email || !apiToken || !projectKey) return;
    setBusy(true);
    setError(null);
    try {
      await AddJiraWorkspace({
        id,
        display_name: displayName || id,
        color,
        instance_url: normalizeURL(instanceURL),
        email,
        api_token: apiToken,
        project_key: projectKey.toUpperCase(),
        jql,
      });
      await refresh();
      onDone();
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  const canTest = instanceURL.trim() !== "" && email.trim() !== "" && apiToken.trim() !== "";
  const canSave = canTest && projectKey.trim() !== "" && id.trim() !== "" && !busy;

  return (
    <div className="space-y-4 text-sm">
      <div className="rounded border border-blue-800 bg-blue-950/40 p-3 text-blue-300 text-xs">
        Connect to a Jira Cloud project. Issues in the project will appear as cards on the board.
        The API token is stored in your OS keyring and never written to disk.
      </div>

      {/* Jira Cloud credentials */}
      <div className="space-y-2">
        <div className="text-xs text-slate-500 font-medium uppercase tracking-wide">Jira Cloud credentials</div>

        <label className="block">
          <div className="text-xs text-slate-500 mb-1">Instance URL</div>
          <input
            {...noAutoCorrect}
            type="url"
            value={instanceURL}
            onChange={(e) => { setInstanceURL(e.target.value); setTestStatus("idle"); }}
            placeholder="https://myorg.atlassian.net"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          />
        </label>

        <label className="block">
          <div className="text-xs text-slate-500 mb-1">Account email</div>
          <input
            {...noAutoCorrect}
            type="email"
            value={email}
            onChange={(e) => { setEmail(e.target.value); setTestStatus("idle"); }}
            placeholder="you@example.com"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          />
        </label>

        <label className="block">
          <div className="text-xs text-slate-500 mb-1">
            API token{" "}
            <span className="text-slate-600">(create at id.atlassian.com → Security → API tokens)</span>
          </div>
          <input
            {...noAutoCorrect}
            type="password"
            value={apiToken}
            onChange={(e) => { setAPIToken(e.target.value); setTestStatus("idle"); }}
            placeholder="••••••••••••••••"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
          />
        </label>

        <label className="block">
          <div className="text-xs text-slate-500 mb-1">Project key</div>
          <input
            {...noAutoCorrect}
            value={projectKey}
            onChange={(e) => { setProjectKey(e.target.value.toUpperCase()); setTestStatus("idle"); }}
            placeholder="PROJ"
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 font-mono"
          />
        </label>

        <label className="block">
          <div className="text-xs text-slate-500 mb-1">
            Custom JQL <span className="text-slate-600">(optional — defaults to open issues in project)</span>
          </div>
          <input
            {...noAutoCorrect}
            value={jql}
            onChange={(e) => setJQL(e.target.value)}
            placeholder='project = PROJ AND statusCategory != Done ORDER BY updated DESC'
            className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 font-mono text-xs"
          />
        </label>

        <div className="flex items-center gap-2 pt-1">
          <button
            onClick={testConnection}
            disabled={!canTest || busy}
            className="px-3 py-1 border border-slate-600 rounded hover:bg-slate-800 disabled:opacity-40 disabled:cursor-not-allowed text-slate-300"
          >
            {busy && testStatus === "idle" ? "Testing…" : "Test connection"}
          </button>
          {testStatus === "ok" && (
            <span className="text-emerald-400 text-xs">Connection successful</span>
          )}
          {testStatus === "error" && testError && (
            <span className="text-red-400 text-xs truncate max-w-[280px]" title={testError}>{testError}</span>
          )}
        </div>
      </div>

      {/* Workspace identity */}
      <div className="space-y-2">
        <div className="text-xs text-slate-500 font-medium uppercase tracking-wide">Workspace settings</div>

        <div className="grid grid-cols-2 gap-2">
          <label className="block">
            <div className="text-xs text-slate-500 mb-1">ID</div>
            <input
              {...noAutoCorrect}
              value={id}
              onChange={(e) => setID(e.target.value)}
              placeholder="my-jira-project"
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
            />
          </label>
          <label className="block">
            <div className="text-xs text-slate-500 mb-1">Display name</div>
            <input
              {...noAutoCorrect}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder={id || "My Jira Project"}
              className="w-full bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200"
            />
          </label>
        </div>

        <div>
          <div className="text-xs text-slate-500 mb-1">Card color</div>
          <div className="flex gap-1">
            {COLOR_PALETTE.map((c) => (
              <button
                key={c}
                onClick={() => setColor(c)}
                style={{ backgroundColor: c }}
                className={"w-6 h-6 rounded-full border-2 " + (color === c ? "border-white" : "border-transparent")}
              />
            ))}
          </div>
        </div>
      </div>

      {error && <div className="text-red-400 text-xs">{error}</div>}

      <div className="flex justify-end gap-2 pt-2 border-t border-slate-800">
        <button onClick={onDone} className="px-3 py-1 text-slate-400 hover:text-slate-200">Cancel</button>
        <button
          onClick={save}
          disabled={!canSave}
          className="px-3 py-1 bg-blue-700 hover:bg-blue-600 rounded disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {busy ? "Adding…" : "Add Jira workspace"}
        </button>
      </div>
    </div>
  );
}
