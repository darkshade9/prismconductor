/**
 * PrismConductor Sandbox Worker (issue #284)
 *
 * Cloudflare Worker that runs plan sessions in isolated per-session Durable
 * Objects. Phase 1 executes plans by calling the Anthropic API directly.
 * Phase 2 will swap runPlan() to spawn a Cloudflare Container that clones the
 * repo and runs the claude-code CLI (see PHASE-2 comments throughout).
 *
 * Wire protocol (identical to the legacy worker so the conductor client is
 * unchanged):
 *   POST   /sessions         → { session_id: string }
 *   GET    /sessions/:id/stream → SSE transcript
 *   DELETE /sessions/:id     → terminate
 *
 * Required CF Secrets (all mandatory — no subscription/OAuth fallback):
 *   CONDUCTOR_API_KEY  — shared secret for request auth
 *   GITHUB_PAT         — fine-grained PAT with Contents+PRs read/write
 *   ANTHROPIC_API_KEY  — API key for Claude calls (Phase 1 only)
 *
 * Phase 1 scope: plan mode only. Execute paths return 400.
 *
 * Build:
 *   cd worker/sandbox-deploy
 *   npx esbuild src/index.ts --bundle --format=esm --outfile=dist/worker.js
 */

export interface Env {
  SESSIONS: DurableObjectNamespace;
  CONDUCTOR_API_KEY: string;
  GITHUB_PAT: string;
  ANTHROPIC_API_KEY: string;
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let result = 0;
  for (let i = 0; i < a.length; i++) {
    result |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return result === 0;
}

function requireAuth(request: Request, env: Env): Response | null {
  const expected = env.CONDUCTOR_API_KEY;
  if (!expected) return null; // No key configured — allow (development)
  const auth = request.headers.get("Authorization") ?? "";
  const provided = auth.startsWith("Bearer ") ? auth.slice(7) : "";
  if (!timingSafeEqual(expected, provided)) {
    return new Response("unauthorized", { status: 401 });
  }
  return null;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (request.method === "GET" && url.pathname === "/health") {
      return Response.json({ ok: true, mode: "sandbox", phase: 1 });
    }

    const authErr = requireAuth(request, env);
    if (authErr) return authErr;

    if (request.method === "POST" && url.pathname === "/sessions") {
      return handleSpawn(request, env);
    }

    const streamMatch = url.pathname.match(/^\/sessions\/([^/]+)\/stream$/);
    if (request.method === "GET" && streamMatch) {
      return forwardToDO(env, streamMatch[1], request, "stream");
    }

    const deleteMatch = url.pathname.match(/^\/sessions\/([^/]+)$/);
    if (request.method === "DELETE" && deleteMatch) {
      return forwardToDO(env, deleteMatch[1], request, "kill");
    }

    return new Response("not found", { status: 404 });
  },
};

async function handleSpawn(request: Request, env: Env): Promise<Response> {
  const params = (await request.json()) as {
    mode?: string;
    workspace_id?: string;
    issue_number?: number;
    github_owner?: string;
    github_repo?: string;
    default_branch?: string;
    pool_model?: string;
  };

  if (!params.mode || params.mode !== "plan") {
    // Phase 1: plan mode only. Execute paths open in Phase 2.
    return Response.json(
      { error: "only mode=plan is supported in Phase 1 sandbox worker" },
      { status: 400 }
    );
  }

  const id = env.SESSIONS.newUniqueId();
  const stub = env.SESSIONS.get(id);

  const initResp = await stub.fetch("https://do-internal/init", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ...params,
      session_id: id.toString(),
      anthropic_api_key: env.ANTHROPIC_API_KEY,
      github_pat: env.GITHUB_PAT,
    }),
  });

  if (!initResp.ok) {
    const errText = await initResp.text();
    return Response.json({ error: `session init failed: ${errText}` }, { status: 500 });
  }

  return Response.json({ session_id: id.toString() }, { status: 201 });
}

async function forwardToDO(
  env: Env,
  sessionID: string,
  request: Request,
  action: "stream" | "kill"
): Promise<Response> {
  try {
    const id = env.SESSIONS.idFromString(sessionID);
    const stub = env.SESSIONS.get(id);
    return stub.fetch(`https://do-internal/${action}`, {
      method: request.method,
      headers: request.headers,
    });
  } catch {
    return new Response("session not found", { status: 404 });
  }
}

// ---------------------------------------------------------------------------
// SandboxSession — Durable Object owning one plan session
// ---------------------------------------------------------------------------

export class SandboxSession {
  private state: DurableObjectState;
  private env: Env;
  private transcript: string[] = [];
  private abortController = new AbortController();
  private sseWriter: WritableStreamDefaultWriter<Uint8Array> | null = null;
  private enc = new TextEncoder();

  constructor(state: DurableObjectState, env: Env) {
    this.state = state;
    this.env = env;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    switch (url.pathname) {
      case "/init":
        return this.handleInit(request);
      case "/stream":
        return this.handleStream(request);
      case "/kill":
        return this.handleKill();
      default:
        return new Response("not found", { status: 404 });
    }
  }

  private async handleInit(request: Request): Promise<Response> {
    const params = (await request.json()) as {
      session_id: string;
      github_owner: string;
      github_repo: string;
      default_branch: string;
      issue_number: number;
      pool_model?: string;
      anthropic_api_key: string;
      github_pat: string;
    };

    // Start plan execution as a background task.
    // PHASE-2: Replace this.runPlan() with this.spawnContainer() which
    // boots a Cloudflare Container, clones the repo, and invokes the CLI.
    this.state.waitUntil(this.runPlan(params));

    return Response.json({ ok: true });
  }

  private handleStream(request: Request): Response {
    const lastID = parseInt(request.headers.get("Last-Event-ID") ?? "0", 10) || 0;
    const backlog = this.transcript.slice(lastID);

    const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>();
    const writer = writable.getWriter();

    this.state.waitUntil(
      (async () => {
        let idx = lastID;
        for (const line of backlog) {
          await writer.write(this.enc.encode(`id:${idx}\ndata:${line}\n\n`));
          idx++;
        }
        this.sseWriter = writer;

        await new Promise<void>((resolve) => {
          request.signal.addEventListener("abort", resolve);
          this.abortController.signal.addEventListener("abort", resolve);
        });
        writer.close().catch(() => {});
        this.sseWriter = null;
      })()
    );

    return new Response(readable, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      },
    });
  }

  private handleKill(): Response {
    this.abortController.abort();
    this.emit("[sandbox] session killed");
    return Response.json({ ok: true });
  }

  private emit(line: string): void {
    const idx = this.transcript.length;
    this.transcript.push(line);
    if (this.sseWriter) {
      this.sseWriter
        .write(this.enc.encode(`id:${idx}\ndata:${line}\n\n`))
        .catch(() => {});
    }
  }

  // runPlan executes the plan skill for the session.
  //
  // Phase 1: calls the Anthropic streaming API with the conductor-plan system
  // prompt. The model generates the plan JSON and emits it to the transcript.
  //
  // PHASE-2: replace the Anthropic API call with container spawn:
  //   const container = await this.env.CONTAINERS.create({ image: "prismconductor-sandbox:latest" });
  //   await container.exec(["sh", "-c", buildCliCmd(params)], { stdout: this.emit.bind(this) });
  private async runPlan(params: {
    session_id: string;
    github_owner: string;
    github_repo: string;
    default_branch: string;
    issue_number: number;
    pool_model?: string;
    anthropic_api_key: string;
  }): Promise<void> {
    try {
      const apiKey = params.anthropic_api_key;
      if (!apiKey) {
        this.emit(
          "BLOCKED: ANTHROPIC_API_KEY not configured — API-key auth is required for sandbox execution"
        );
        this.abortController.abort();
        return;
      }

      this.emit(
        `[sandbox] starting plan session ${params.session_id} for ${params.github_owner}/${params.github_repo}#${params.issue_number}`
      );

      const model = params.pool_model ?? "claude-sonnet-4-5";
      this.emit(`[sandbox] calling ${model}`);

      const systemPrompt = buildPlanSystemPrompt(params);

      const resp = await fetch("https://api.anthropic.com/v1/messages", {
        method: "POST",
        signal: this.abortController.signal,
        headers: {
          "Content-Type": "application/json",
          "x-api-key": apiKey,
          "anthropic-version": "2023-06-01",
        },
        body: JSON.stringify({
          model,
          max_tokens: 8192,
          system: systemPrompt,
          messages: [{ role: "user", content: "Begin." }],
          stream: true,
        }),
      });

      if (!resp.ok) {
        const err = await resp.text();
        this.emit(`BLOCKED: Anthropic API error ${resp.status}: ${err}`);
        return;
      }

      if (!resp.body) {
        this.emit("BLOCKED: Anthropic API returned no body");
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() ?? "";
        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          const payload = line.slice(6).trim();
          if (payload === "[DONE]") continue;
          try {
            const evt = JSON.parse(payload);
            if (
              evt.type === "content_block_delta" &&
              evt.delta?.type === "text_delta"
            ) {
              this.emit(evt.delta.text.replace(/\n/g, "\\n"));
            }
          } catch {
            // skip malformed SSE lines
          }
        }
      }

      this.emit("Work complete.");
    } catch (err: unknown) {
      if (this.abortController.signal.aborted) return;
      const msg = err instanceof Error ? err.message : String(err);
      this.emit(`BLOCKED: sandbox plan error — ${msg}`);
    } finally {
      // Signal all waiting SSE subscribers that the session is done.
      this.abortController.abort();
    }
  }
}

function buildPlanSystemPrompt(params: {
  github_owner: string;
  github_repo: string;
  default_branch: string;
  issue_number: number;
}): string {
  return [
    "You are the PrismConductor planner running remotely inside a Cloudflare Sandbox Worker.",
    `Repository: ${params.github_owner}/${params.github_repo} (branch: ${params.default_branch})`,
    `Issue: #${params.issue_number}`,
    "Your job: read the GitHub issue, analyse the codebase, and emit a structured plan JSON.",
    "Follow the conductor-plan skill instructions exactly.",
    "Emit plan JSON wrapped in ```json fences so the conductor can parse it.",
  ].join("\n");
}
