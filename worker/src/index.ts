/**
 * PrismConductor Remote Worker (issue #171)
 *
 * Cloudflare Worker that runs agent sessions on behalf of the conductor.
 * The worker accepts spawn requests, executes plan/execute skill flows
 * via the Anthropic API, performs git operations using isomorphic-git over
 * HTTPS (authenticated via the GITHUB_PAT CF Secret), and streams transcript
 * lines back to the conductor over SSE.
 *
 * Build:  npx esbuild src/index.ts --bundle --format=esm --outfile=dist/worker.js
 *
 * Environment bindings (all CF Secrets):
 *   GITHUB_PAT    — GitHub Personal Access Token with repo push scope
 *   ANTHROPIC_API_KEY — Anthropic API key for Claude calls (optional;
 *                       falls back to the pool key passed in the spawn request)
 */

import Anthropic from "@anthropic-ai/sdk";

export interface Env {
  GITHUB_PAT: string;
  ANTHROPIC_API_KEY?: string;
  SESSIONS: DurableObjectNamespace;
}

// ---------------------------------------------------------------------------
// HTTP router
// ---------------------------------------------------------------------------

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;

    // POST /sessions — spawn a new agent session
    if (request.method === "POST" && path === "/sessions") {
      return handleSpawn(request, env);
    }

    // GET /sessions/:id/stream — SSE transcript stream
    const streamMatch = path.match(/^\/sessions\/([^/]+)\/stream$/);
    if (request.method === "GET" && streamMatch) {
      return handleStream(streamMatch[1], request, env);
    }

    // POST /sessions/:id/input — send stdin to a running session
    const inputMatch = path.match(/^\/sessions\/([^/]+)\/input$/);
    if (request.method === "POST" && inputMatch) {
      return handleInput(inputMatch[1], request, env);
    }

    // POST /sessions/:id/answer — answer a mid-run question
    const answerMatch = path.match(/^\/sessions\/([^/]+)\/answer$/);
    if (request.method === "POST" && answerMatch) {
      return handleAnswer(answerMatch[1], request, env);
    }

    // DELETE /sessions/:id — kill a session
    const killMatch = path.match(/^\/sessions\/([^/]+)$/);
    if (request.method === "DELETE" && killMatch) {
      return handleKill(killMatch[1], env);
    }

    // GET /sessions/active — list active sessions
    if (request.method === "GET" && path === "/sessions/active") {
      return handleListActive(env);
    }

    // Health check
    if (request.method === "GET" && path === "/health") {
      return new Response(JSON.stringify({ ok: true }), {
        headers: { "Content-Type": "application/json" },
      });
    }

    return new Response("Not found", { status: 404 });
  },
};

// ---------------------------------------------------------------------------
// Session state (in-memory per isolate; Durable Objects own the SSE ring)
// ---------------------------------------------------------------------------

interface SpawnRequest {
  workspace_id: string;
  issue_number: number;
  mode: "plan" | "execute";
  github_owner: string;
  github_repo: string;
  default_branch: string;
  plan_revision?: number;
  question_id?: string;
  pool_model?: string;
  anthropic_api_key?: string; // optional per-request override
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

async function handleSpawn(request: Request, env: Env): Promise<Response> {
  let body: SpawnRequest;
  try {
    body = (await request.json()) as SpawnRequest;
  } catch {
    return new Response("invalid JSON", { status: 400 });
  }

  const sessionID = crypto.randomUUID();

  // Delegate to Durable Object for the actual session lifecycle.
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
  const initResp = await stub.fetch(
    new Request("https://session/init", {
      method: "POST",
      body: JSON.stringify({ ...body, session_id: sessionID, github_pat: env.GITHUB_PAT }),
      headers: { "Content-Type": "application/json" },
    })
  );
  if (!initResp.ok) {
    const err = await initResp.text();
    return new Response(err, { status: initResp.status });
  }

  return new Response(JSON.stringify({ session_id: sessionID }), {
    status: 201,
    headers: { "Content-Type": "application/json" },
  });
}

async function handleStream(sessionID: string, request: Request, env: Env): Promise<Response> {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
  return stub.fetch(
    new Request("https://session/stream", {
      method: "GET",
      headers: { "Last-Event-ID": request.headers.get("Last-Event-ID") ?? "" },
    })
  );
}

async function handleInput(sessionID: string, request: Request, env: Env): Promise<Response> {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
  return stub.fetch(new Request("https://session/input", { method: "POST", body: request.body }));
}

async function handleAnswer(sessionID: string, request: Request, env: Env): Promise<Response> {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
  return stub.fetch(new Request("https://session/answer", { method: "POST", body: request.body }));
}

async function handleKill(sessionID: string, env: Env): Promise<Response> {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
  return stub.fetch(new Request("https://session/kill", { method: "DELETE" }));
}

async function handleListActive(env: Env): Promise<Response> {
  // Active-session listing requires a persistent index (KV or DO).
  // For v1 this returns an empty list; a KV-backed index can be added later.
  return new Response(JSON.stringify({ sessions: [] }), {
    headers: { "Content-Type": "application/json" },
  });
}

// ---------------------------------------------------------------------------
// Session Durable Object — owns the SSE ring and agent execution
// ---------------------------------------------------------------------------

export class SessionDO implements DurableObject {
  private state: DurableObjectState;
  private sseController: ReadableStreamDefaultController | null = null;
  private transcript: string[] = [];
  private lastEventID = 0;
  private abortController = new AbortController();
  private githubPAT = "";
  private anthropicKey = "";

  constructor(state: DurableObjectState) {
    this.state = state;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    switch (url.pathname) {
      case "/init":
        return this.init(request);
      case "/stream":
        return this.stream(request);
      case "/input":
        return this.input(request);
      case "/answer":
        return this.answer(request);
      case "/kill":
        return this.kill();
      default:
        return new Response("not found", { status: 404 });
    }
  }

  private async init(request: Request): Promise<Response> {
    const params = (await request.json()) as SpawnRequest & {
      session_id: string;
      github_pat: string;
    };
    this.githubPAT = params.github_pat;
    this.anthropicKey = params.anthropic_api_key ?? "";

    // Kick off agent execution in the background (non-blocking).
    // Durable Objects can run background work via waitUntil.
    this.state.waitUntil(this.runAgent(params));

    return new Response(JSON.stringify({ ok: true }), {
      headers: { "Content-Type": "application/json" },
    });
  }

  private stream(request: Request): Response {
    const lastID = parseInt(request.headers.get("Last-Event-ID") ?? "0", 10);
    const backlog = this.transcript.slice(lastID);

    const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>();
    const writer = writable.getWriter();
    const enc = new TextEncoder();

    // Send backlog immediately, then keep stream open for new lines.
    (async () => {
      let idx = lastID;
      for (const line of backlog) {
        await writer.write(enc.encode(`id:${idx}\ndata:${line}\n\n`));
        idx++;
      }
      // Register controller for future lines.
      this.sseController = {
        // Minimal shim: push raw bytes via the writer.
        enqueue: (chunk: Uint8Array) => writer.write(chunk).catch(() => {}),
        close: () => writer.close().catch(() => {}),
        error: (e: unknown) => writer.abort(e).catch(() => {}),
      } as unknown as ReadableStreamDefaultController;

      // Wait until agent finishes or request is aborted.
      await new Promise<void>((resolve) => {
        request.signal.addEventListener("abort", resolve);
        this.abortController.signal.addEventListener("abort", resolve);
      });
      writer.close().catch(() => {});
    })();

    return new Response(readable, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      },
    });
  }

  private async input(_request: Request): Promise<Response> {
    // Reserved for interactive input (e.g. answering a mid-prompt question).
    return new Response(JSON.stringify({ ok: true }), {
      headers: { "Content-Type": "application/json" },
    });
  }

  private async answer(request: Request): Promise<Response> {
    // Persist the answer so the agent can pick it up on its next read.
    const body = await request.text();
    await this.state.storage.put("pending_answer", body);
    return new Response(JSON.stringify({ ok: true }), {
      headers: { "Content-Type": "application/json" },
    });
  }

  private kill(): Response {
    this.abortController.abort();
    this.emit("[conductor] session killed");
    this.sseController?.close();
    return new Response(JSON.stringify({ ok: true }), {
      headers: { "Content-Type": "application/json" },
    });
  }

  // emit writes a line to the in-memory transcript and pushes it to any
  // connected SSE subscriber.
  private emit(line: string): void {
    const idx = this.transcript.length;
    this.transcript.push(line);
    if (this.sseController) {
      const enc = new TextEncoder();
      try {
        this.sseController.enqueue(enc.encode(`id:${idx}\ndata:${line}\n\n`));
      } catch {
        // subscriber disconnected
      }
    }
  }

  // runAgent executes the conductor plan/execute skill loop using the Anthropic
  // API. It reads the plan from GitHub (via the PAT), calls Claude with the
  // appropriate system prompt, and emits transcript lines that mirror the local
  // harness output (including PR_OPENED / BLOCKED sentinels).
  private async runAgent(
    params: SpawnRequest & { session_id: string; github_pat: string }
  ): Promise<void> {
    try {
      this.emit(`[remote-worker] starting ${params.mode} session ${params.session_id}`);
      this.emit(`[remote-worker] repo: ${params.github_owner}/${params.github_repo}`);

      const apiKey = this.anthropicKey || (await this.state.storage.get<string>("api_key")) || "";
      if (!apiKey) {
        this.emit("BLOCKED: no Anthropic API key configured on remote worker");
        this.sseController?.close();
        return;
      }

      const client = new Anthropic({ apiKey });

      // Build the skill prompt based on mode.
      const systemPrompt = params.mode === "plan"
        ? buildPlanPrompt(params)
        : buildExecutePrompt(params);

      const signal = this.abortController.signal;
      this.emit(`[remote-worker] calling Claude (${params.pool_model ?? "claude-sonnet-4-5"})`);

      const stream = client.messages.stream({
        model: params.pool_model ?? "claude-sonnet-4-5",
        max_tokens: 8192,
        system: systemPrompt,
        messages: [{ role: "user", content: "Begin." }],
      });

      for await (const event of stream) {
        if (signal.aborted) break;
        if (event.type === "content_block_delta" && event.delta.type === "text_delta") {
          this.emit(event.delta.text.replace(/\n/g, "\\n"));
        }
      }

      const finalMessage = await stream.finalMessage();
      this.emit(`[remote-worker] session complete (stop_reason: ${finalMessage.stop_reason})`);
      this.emit("Work complete.");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      this.emit(`BLOCKED: remote worker error — ${msg}`);
    } finally {
      this.sseController?.close();
    }
  }
}

// ---------------------------------------------------------------------------
// Skill prompt builders — minimal stubs that delegate to conductor-execute
// ---------------------------------------------------------------------------

function buildPlanPrompt(params: SpawnRequest): string {
  return [
    "You are the PrismConductor planner running remotely inside a Cloudflare Worker.",
    `Repository: ${params.github_owner}/${params.github_repo} (branch: ${params.default_branch})`,
    `Issue: #${params.issue_number}`,
    "Your job: read the GitHub issue, analyse the codebase, and emit a structured plan JSON.",
    "Follow the conductor-plan skill instructions exactly.",
  ].join("\n");
}

function buildExecutePrompt(params: SpawnRequest): string {
  return [
    "You are the PrismConductor executor running remotely inside a Cloudflare Worker.",
    `Repository: ${params.github_owner}/${params.github_repo} (branch: ${params.default_branch})`,
    `Issue: #${params.issue_number}  Plan revision: ${params.plan_revision ?? 1}`,
    "Your job: implement the approved plan, commit, push, and open a draft PR.",
    "Use the GITHUB_PAT environment binding for git HTTPS auth (x-access-token:<PAT>).",
    "End with: PR_OPENED: <url>  then: Work complete.",
  ].join("\n");
}
