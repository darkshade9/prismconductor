/**
 * Auth tests for the PrismConductor remote worker (issue #177).
 *
 * Tests verify that:
 *   - /health is always accessible without a bearer token.
 *   - All session endpoints return 401 when CONDUCTOR_API_KEY is set but the
 *     Authorization header is missing, empty, or carries the wrong value.
 *   - All session endpoints return the expected response when the correct
 *     Bearer token is sent.
 *
 * Run with: npx vitest run
 * (requires: npm install in worker/ directory)
 */

import { describe, it, expect, beforeAll, vi } from "vitest";

// ---------------------------------------------------------------------------
// Minimal Cloudflare Worker environment mock
// ---------------------------------------------------------------------------

const CORRECT_KEY = "test-conductor-api-key-32-bytes!";

/** Build a minimal Env-like object. Pass undefined to omit CONDUCTOR_API_KEY. */
function makeEnv(key: string | undefined): Record<string, unknown> {
  const sessionStub = {
    fetch: async (_req: Request) =>
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
  };
  const sessions = {
    idFromName: (name: string) => name,
    get: (_id: string) => sessionStub,
  };
  return {
    GITHUB_PAT: "fake-pat",
    CONDUCTOR_API_KEY: key,
    SESSIONS: sessions,
  };
}

/** Import the worker fetch handler from the built dist bundle. */
async function importWorker() {
  // Dynamic import so it can be tree-shaken and mocked per test.
  const mod = await import("../dist/worker.js");
  return mod.default as { fetch: (req: Request, env: unknown) => Promise<Response> };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeRequest(method: string, path: string, authKey?: string): Request {
  const headers: HeadersInit = {};
  if (authKey !== undefined) {
    headers["Authorization"] = `Bearer ${authKey}`;
  }
  return new Request(`https://worker.example${path}`, { method, headers });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("GET /health — always public", () => {
  it("returns 200 without a bearer token when CONDUCTOR_API_KEY is set", async () => {
    const worker = await importWorker();
    const env = makeEnv(CORRECT_KEY);
    const res = await worker.fetch(makeRequest("GET", "/health"), env);
    expect(res.status).toBe(200);
  });

  it("returns 200 without CONDUCTOR_API_KEY configured", async () => {
    const worker = await importWorker();
    const env = makeEnv(undefined);
    const res = await worker.fetch(makeRequest("GET", "/health"), env);
    expect(res.status).toBe(200);
  });
});

describe("Session endpoints — auth enforcement", () => {
  const sessionEndpoints: [string, string][] = [
    ["POST", "/sessions"],
    ["GET", "/sessions/abc/stream"],
    ["POST", "/sessions/abc/input"],
    ["POST", "/sessions/abc/answer"],
    ["DELETE", "/sessions/abc"],
  ];

  for (const [method, path] of sessionEndpoints) {
    it(`${method} ${path} → 401 when header is missing`, async () => {
      const worker = await importWorker();
      const env = makeEnv(CORRECT_KEY);
      const res = await worker.fetch(makeRequest(method, path), env);
      expect(res.status).toBe(401);
    });

    it(`${method} ${path} → 401 when header has wrong key`, async () => {
      const worker = await importWorker();
      const env = makeEnv(CORRECT_KEY);
      const res = await worker.fetch(makeRequest(method, path, "wrong-key"), env);
      expect(res.status).toBe(401);
    });

    it(`${method} ${path} → 401 when header is empty string`, async () => {
      const worker = await importWorker();
      const env = makeEnv(CORRECT_KEY);
      const req = new Request(`https://worker.example${path}`, {
        method,
        headers: { Authorization: "" },
      });
      const res = await worker.fetch(req, env);
      expect(res.status).toBe(401);
    });

    it(`${method} ${path} → not 401 when correct key is sent`, async () => {
      const worker = await importWorker();
      const env = makeEnv(CORRECT_KEY);
      const res = await worker.fetch(makeRequest(method, path, CORRECT_KEY), env);
      expect(res.status).not.toBe(401);
    });
  }

  it("passes through when CONDUCTOR_API_KEY is not configured (no key mode)", async () => {
    const worker = await importWorker();
    const env = makeEnv(undefined);
    // /sessions/active is a safe read endpoint; won't mutate state.
    const res = await worker.fetch(makeRequest("GET", "/sessions/active"), env);
    expect(res.status).not.toBe(401);
  });
});
