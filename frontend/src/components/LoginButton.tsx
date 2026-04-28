import { useEffect, useState } from "react";
import {
  GitHubAuthStatus,
  GitHubLoginStart,
  GitHubLoginPoll,
  GitHubLogout,
} from "../../wailsjs/go/main/App";
import { githubauth } from "../../wailsjs/go/models";

type User = githubauth.User;
type DeviceCode = githubauth.DeviceCode;

export function LoginButton() {
  const [user, setUser] = useState<User | null>(null);
  const [device, setDevice] = useState<DeviceCode | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    GitHubAuthStatus()
      .then((u) => setUser(u ?? null))
      .catch(() => setUser(null));
  }, []);

  async function login() {
    setBusy(true);
    setError(null);
    try {
      const dc = await GitHubLoginStart();
      setDevice(dc);
      // PollForToken blocks server-side until success/expiry.
      const u = await GitHubLoginPoll();
      setUser(u ?? null);
      setDevice(null);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    await GitHubLogout();
    setUser(null);
  }

  if (user) {
    return (
      <div className="flex items-center gap-2 text-xs">
        {user.avatar_url && <img src={user.avatar_url} alt="" className="w-5 h-5 rounded-full" />}
        <span className="text-slate-300">{user.login}</span>
        <button onClick={logout} className="text-slate-500 hover:text-slate-300">Logout</button>
      </div>
    );
  }

  if (device) {
    return (
      <div className="flex flex-col items-end text-xs">
        <div className="text-slate-300">
          Code: <span className="font-mono text-amber-300">{device.user_code}</span>
        </div>
        <a href={device.verification_uri} target="_blank" rel="noreferrer" className="text-sky-400 hover:underline">
          {device.verification_uri}
        </a>
        <span className="text-slate-500">Waiting for browser approval…</span>
        {error && <span className="text-red-400">{error}</span>}
      </div>
    );
  }

  return (
    <div className="flex flex-col items-end">
      <button
        onClick={login}
        disabled={busy}
        className="text-xs bg-slate-800 hover:bg-slate-700 border border-slate-600 px-2 py-1 rounded disabled:opacity-50"
      >
        {busy ? "Opening browser…" : "Login with GitHub"}
      </button>
      {error && <span className="text-xs text-red-400 mt-1 max-w-[260px] text-right">{error}</span>}
    </div>
  );
}
