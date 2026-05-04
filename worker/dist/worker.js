/**
 * PrismConductor Remote Worker — pre-built bundle (issue #171)
 * Source: worker/src/index.ts
 * Build: cd worker && npx esbuild src/index.ts --bundle --format=esm --outfile=dist/worker.js
 *
 * This file is embedded in the Go binary via go:embed and deployed to
 * Cloudflare Workers by DeployRemoteWorker. Rebuild after editing index.ts.
 */

// ---------------------------------------------------------------------------
// Session Durable Object
// ---------------------------------------------------------------------------
class SessionDO {
  constructor(state) {
    this.state = state;
    this.sseController = null;
    this.transcript = [];
    this.lastEventID = 0;
    this.abortController = new AbortController();
    this.githubPAT = "";
    this.anthropicKey = "";
  }

  async fetch(request) {
    const url = new URL(request.url);
    switch (url.pathname) {
      case "/init":   return this.init(request);
      case "/stream": return this.stream(request);
      case "/input":  return this.input(request);
      case "/answer": return this.answer(request);
      case "/kill":   return this.kill();
      default:        return new Response("not found", { status: 404 });
    }
  }

  async init(request) {
    const params = await request.json();
    this.githubPAT = params.github_pat || "";
    this.anthropicKey = params.anthropic_api_key || "";
    this.state.waitUntil(this.runAgent(params));
    return jsonOK({ ok: true });
  }

  stream(request) {
    const lastID = parseInt(request.headers.get("Last-Event-ID") || "0", 10);
    const backlog = this.transcript.slice(lastID);
    const { readable, writable } = new TransformStream();
    const writer = writable.getWriter();
    const enc = new TextEncoder();

    (async () => {
      let idx = lastID;
      for (const line of backlog) {
        await writer.write(enc.encode(`id:${idx}\ndata:${line}\n\n`));
        idx++;
      }
      this.sseController = {
        enqueue: (chunk) => writer.write(chunk).catch(() => {}),
        close: () => writer.close().catch(() => {}),
        error: (e) => writer.abort(e).catch(() => {}),
      };
      await new Promise((resolve) => {
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

  async input(_request) {
    return jsonOK({ ok: true });
  }

  async answer(request) {
    const body = await request.text();
    await this.state.storage.put("pending_answer", body);
    return jsonOK({ ok: true });
  }

  kill() {
    this.abortController.abort();
    this.emit("[conductor] session killed");
    if (this.sseController) this.sseController.close();
    return jsonOK({ ok: true });
  }

  emit(line) {
    const idx = this.transcript.length;
    this.transcript.push(line);
    if (this.sseController) {
      const enc = new TextEncoder();
      try {
        this.sseController.enqueue(enc.encode(`id:${idx}\ndata:${line}\n\n`));
      } catch (_) { /* subscriber disconnected */ }
    }
  }

  async runAgent(params) {
    try {
      this.emit(`[remote-worker] starting ${params.mode} session ${params.session_id}`);
      this.emit(`[remote-worker] repo: ${params.github_owner}/${params.github_repo}`);

      const apiKey = this.anthropicKey || await this.state.storage.get("api_key") || "";
      if (!apiKey) {
        this.emit("BLOCKED: no Anthropic API key configured on remote worker");
        if (this.sseController) this.sseController.close();
        return;
      }

      const systemPrompt = params.mode === "plan"
        ? buildPlanPrompt(params)
        : buildExecutePrompt(params);

      this.emit(`[remote-worker] calling Claude (${params.pool_model || "claude-sonnet-4-5"})`);

      const resp = await fetch("https://api.anthropic.com/v1/messages", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "x-api-key": apiKey,
          "anthropic-version": "2023-06-01",
        },
        body: JSON.stringify({
          model: params.pool_model || "claude-sonnet-4-5",
          max_tokens: 8192,
          system: systemPrompt,
          messages: [{ role: "user", content: "Begin." }],
          stream: true,
        }),
        signal: this.abortController.signal,
      });

      if (!resp.ok) {
        const err = await resp.text();
        this.emit(`BLOCKED: Anthropic API error (${resp.status}): ${err.slice(0, 200)}`);
        if (this.sseController) this.sseController.close();
        return;
      }

      const reader = resp.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() || "";
        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          const payload = line.slice(6).trim();
          if (payload === "[DONE]") continue;
          try {
            const ev = JSON.parse(payload);
            if (ev.type === "content_block_delta" && ev.delta?.type === "text_delta") {
              this.emit(ev.delta.text.replace(/\n/g, "\\n"));
            }
          } catch (_) { /* ignore parse errors on partial chunks */ }
        }
      }

      this.emit("[remote-worker] session complete");
      this.emit("Work complete.");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      this.emit(`BLOCKED: remote worker error — ${msg}`);
    } finally {
      if (this.sseController) this.sseController.close();
    }
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function jsonOK(body) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
  });
}

function buildPlanPrompt(params) {
  return [
    "You are the PrismConductor planner running remotely inside a Cloudflare Worker.",
    `Repository: ${params.github_owner}/${params.github_repo} (branch: ${params.default_branch})`,
    `Issue: #${params.issue_number}`,
    "Your job: read the GitHub issue, analyse the codebase, and emit a structured plan JSON.",
    "Follow the conductor-plan skill instructions exactly.",
  ].join("\n");
}

function buildExecutePrompt(params) {
  return [
    "You are the PrismConductor executor running remotely inside a Cloudflare Worker.",
    `Repository: ${params.github_owner}/${params.github_repo} (branch: ${params.default_branch})`,
    `Issue: #${params.issue_number}  Plan revision: ${params.plan_revision || 1}`,
    "Your job: implement the approved plan, commit, push, and open a draft PR.",
    "Use the GITHUB_PAT environment binding for git HTTPS auth (x-access-token:<PAT>).",
    "End with: PR_OPENED: <url>  then: Work complete.",
  ].join("\n");
}

// ---------------------------------------------------------------------------
// Main export
// ---------------------------------------------------------------------------

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;

    if (request.method === "POST" && path === "/sessions") {
      let body;
      try { body = await request.json(); } catch (_) {
        return new Response("invalid JSON", { status: 400 });
      }
      const sessionID = crypto.randomUUID();
      const stub = env.SESSIONS.get(env.SESSIONS.idFromName(sessionID));
      const initResp = await stub.fetch(new Request("https://session/init", {
        method: "POST",
        body: JSON.stringify({ ...body, session_id: sessionID, github_pat: env.GITHUB_PAT }),
        headers: { "Content-Type": "application/json" },
      }));
      if (!initResp.ok) return new Response(await initResp.text(), { status: initResp.status });
      return new Response(JSON.stringify({ session_id: sessionID }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      });
    }

    const streamMatch = path.match(/^\/sessions\/([^/]+)\/stream$/);
    if (request.method === "GET" && streamMatch) {
      const stub = env.SESSIONS.get(env.SESSIONS.idFromName(streamMatch[1]));
      return stub.fetch(new Request("https://session/stream", {
        method: "GET",
        headers: { "Last-Event-ID": request.headers.get("Last-Event-ID") || "" },
      }));
    }

    const inputMatch = path.match(/^\/sessions\/([^/]+)\/input$/);
    if (request.method === "POST" && inputMatch) {
      const stub = env.SESSIONS.get(env.SESSIONS.idFromName(inputMatch[1]));
      return stub.fetch(new Request("https://session/input", { method: "POST", body: request.body }));
    }

    const answerMatch = path.match(/^\/sessions\/([^/]+)\/answer$/);
    if (request.method === "POST" && answerMatch) {
      const stub = env.SESSIONS.get(env.SESSIONS.idFromName(answerMatch[1]));
      return stub.fetch(new Request("https://session/answer", { method: "POST", body: request.body }));
    }

    const killMatch = path.match(/^\/sessions\/([^/]+)$/);
    if (request.method === "DELETE" && killMatch) {
      const stub = env.SESSIONS.get(env.SESSIONS.idFromName(killMatch[1]));
      return stub.fetch(new Request("https://session/kill", { method: "DELETE" }));
    }

    if (request.method === "GET" && path === "/sessions/active") {
      return new Response(JSON.stringify({ sessions: [] }), {
        headers: { "Content-Type": "application/json" },
      });
    }

    if (request.method === "GET" && path === "/health") {
      return new Response(JSON.stringify({ ok: true }), {
        headers: { "Content-Type": "application/json" },
      });
    }

    return new Response("Not found", { status: 404 });
  },
};

export { SessionDO };
