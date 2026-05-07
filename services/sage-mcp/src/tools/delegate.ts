import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import crypto from "node:crypto";
import fs from "node:fs";
import { z } from "zod";

import { RedisClient } from "../redis-client.js";
import {
  ChannelTasks,
  ChannelEvents,
  type A2AEvent,
  type TaskArtifactUpdateEvent,
  type TaskStatusUpdateEvent,
  artifactText,
  buildMessage,
  isPaused,
  isTerminal,
  summarizeStatus,
} from "../a2a/types.js";

function log(msg: string): void {
  process.stderr.write(`[delegate] ${new Date().toISOString()} ${msg}\n`);
}

// ── Soul loading ───────────────────────────────────────────────────────────────

function loadSoul(): string {
  const soulPath = process.env["SAGE_SOUL_PATH"] ?? "/home/node/.openclaw/workspace/SOUL.md";
  try {
    const content = fs.readFileSync(soulPath, "utf-8").trim();
    log(`loaded soul from ${soulPath} (${content.length} chars)`);
    return content;
  } catch {
    log(`soul file not found at ${soulPath}, using inline fallback`);
    return "";
  }
}

const SOUL_CONTENT = loadSoul();

const SOUL_INSTRUCTIONS = SOUL_CONTENT
  ? `IMPORTANT — re-voice this response using your soul/personality as defined below:
${SOUL_CONTENT}

Rules for re-voicing:
- Keep ALL the substance, detail, and structure from the research output — do not summarize or shorten it
- Only change the tone: apply your personality, voice, and language style from above
- If the research output is long and detailed, your response should also be long and detailed — just in your voice
- Do not compress, condense, or drop information to sound more "concise"`
  : `IMPORTANT — response handling:
The response you receive is raw output from the research agent — accurate but unformatted.
You are responsible for re-voicing it in your own tone before presenting it to the user.
Rules:
- Keep ALL the substance, detail, and structure from the research output — do not summarize or shorten it
- Only change the tone: strip corporate filler, make it sound like you
- If the research output is long and detailed, your response should also be long and detailed — just in your voice
- Do not compress, condense, or drop information to sound more "concise"`;

// ── Redis A2A bus client (primary path) ────────────────────────────────────────

let pubClient: RedisClient | null = null;
let subClient: RedisClient | null = null;

// PausedResult is what we return to Sage when the manager pauses a task
// (state: input-required). Sage's prompt/SOUL tells her to surface this
// to the user and call delegate_continue once they decide.
export interface PausedResult {
  paused: true;
  taskId: string;
  contextId?: string;
  prompt: string;
  partial: string;
  metadata?: Record<string, unknown>;
}

type ResolveValue = string | PausedResult;

interface PendingTask {
  artifact: string;
  resolve: (value: ResolveValue) => void;
  reject: (err: Error) => void;
  resetInactivity: () => void;
  forwardProgress: (msg: string) => void;
  cleanup: () => void;
  taskId: string;
  contextId?: string;
}

const pending = new Map<string, PendingTask>();

async function getRedis(): Promise<{ pub: RedisClient; sub: RedisClient } | null> {
  const addr = process.env["REDIS_ADDR"];
  if (!addr) return null;

  const [host, portStr] = addr.split(":");
  const port = parseInt(portStr ?? "6379", 10);
  const password = process.env["REDIS_PASSWORD"] || undefined;

  if (!pubClient) {
    pubClient = new RedisClient(host, port, password);
    await pubClient.connect();
    log(`pub client connected to ${addr}`);
  }
  if (!subClient) {
    subClient = new RedisClient(host, port, password);
    await subClient.connect();
    log(`sub client connected to ${addr}, subscribing to ${ChannelEvents}`);
    await subClient.subscribe(ChannelEvents, (_channel, payload) => {
      let evt: A2AEvent;
      try {
        evt = JSON.parse(payload) as A2AEvent;
      } catch (e) {
        log(`malformed A2A event: ${e instanceof Error ? e.message : String(e)}`);
        return;
      }
      const taskId = (evt as { taskId?: string }).taskId;
      if (!taskId) return;
      const task = pending.get(taskId);
      if (!task) return;

      task.resetInactivity();

      if (evt.kind === "status-update") {
        const status = evt as TaskStatusUpdateEvent;
        task.forwardProgress(summarizeStatus(status));

        if (isPaused(status.status.state)) {
          // Manager hit its round cap (or auth gate, future). Resolve the
          // tool call with a structured paused payload so Sage can present
          // the situation to the user and call delegate_continue.
          task.cleanup();
          pending.delete(taskId);
          const promptText =
            status.status.message?.parts?.find((p) => p.kind === "text")?.text ??
            "Manager paused. Continue?";
          task.resolve({
            paused: true,
            taskId,
            contextId: task.contextId,
            prompt: promptText,
            partial: task.artifact,
            metadata: status.metadata,
          });
          return;
        }

        if (isTerminal(status.status.state)) {
          task.cleanup();
          pending.delete(taskId);
          if (status.status.state === "completed") {
            task.resolve(task.artifact);
          } else {
            const errMsg = status.status.message?.parts?.[0]?.text
              ?? `task ended in ${status.status.state}`;
            task.reject(new Error(errMsg));
          }
        }
      } else if (evt.kind === "artifact-update") {
        const art = evt as TaskArtifactUpdateEvent;
        task.artifact += artifactText(art);
      }
    });
  }
  return { pub: pubClient, sub: subClient };
}

interface DispatchExtra {
  sendNotification?: (n: unknown) => Promise<void> | void;
  _meta?: { progressToken?: string | number };
}

async function dispatchA2A(
  content: string,
  sessionId: string | undefined,
  capability: string,
  resource: string,
  inactivityMs: number,
  maxMs: number,
  extra: DispatchExtra | undefined,
): Promise<ResolveValue> {
  const redis = await getRedis();
  if (!redis) throw new Error("REDIS_ADDR not configured — cannot use A2A bus path");

  const taskId = crypto.randomUUID();
  const contextId = sessionId ?? crypto.randomUUID();

  log(`→ A2A dispatch  taskId=${taskId}  contextId=${contextId}  inactivityMs=${inactivityMs}  maxMs=${maxMs}`);

  const progressToken = extra?._meta?.progressToken;
  const sendNotification = extra?.sendNotification;
  let progressCount = 0;

  return new Promise<ResolveValue>((resolve, reject) => {
    let inactivityTimer: ReturnType<typeof setTimeout> | null = null;
    const absoluteTimer = setTimeout(() => {
      const t = pending.get(taskId);
      if (t) {
        t.cleanup();
        pending.delete(taskId);
      }
      reject(new Error(`task ${taskId} exceeded absolute cap ${maxMs}ms`));
    }, maxMs);

    const resetInactivity = () => {
      if (inactivityTimer) clearTimeout(inactivityTimer);
      inactivityTimer = setTimeout(() => {
        const t = pending.get(taskId);
        if (t) {
          t.cleanup();
          pending.delete(taskId);
        }
        reject(new Error(`no events from manager for ${inactivityMs}ms — task ${taskId}`));
      }, inactivityMs);
    };

    const forwardProgress = (msg: string) => {
      if (!sendNotification || progressToken === undefined) return;
      progressCount++;
      try {
        void sendNotification({
          method: "notifications/progress",
          params: { progressToken, progress: progressCount, message: msg },
        });
      } catch (e) {
        log(`progress forwarding failed (ignored): ${e instanceof Error ? e.message : String(e)}`);
      }
    };

    const cleanup = () => {
      if (inactivityTimer) clearTimeout(inactivityTimer);
      clearTimeout(absoluteTimer);
    };

    pending.set(taskId, {
      artifact: "",
      taskId,
      contextId,
      resolve: (value) => { cleanup(); resolve(value); },
      reject: (err) => { cleanup(); reject(err); },
      resetInactivity,
      forwardProgress,
      cleanup,
    });

    resetInactivity();

    const message = buildMessage(taskId, contextId, content, {
      capability,
      resource,
      content,
    });

    redis.pub.publish(ChannelTasks, JSON.stringify(message)).then(() => {
      log(`  published task ${taskId} → ${ChannelTasks}`);
    }).catch((err) => {
      const t = pending.get(taskId);
      if (t) { t.cleanup(); pending.delete(taskId); }
      reject(err);
    });
  });
}

// ── HTTP fallback (Redis not configured) ──────────────────────────────────────
// Streams NDJSON events from the manager's /dispatch endpoint. Used only when
// REDIS_ADDR is unset (CLI/dev). Production traffic flows through Redis.

interface ProgressEvent {
  type: string;
  request_id?: string;
  agent?: string;
  tool?: string;
  phase?: string;
  duration_ms?: number;
  elapsed_ms?: number;
  message?: string;
  error?: string;
}

async function dispatchHTTPStream(
  content: string,
  sessionId: string | undefined,
  capability: string,
  resource: string,
  inactivityMs: number,
  maxMs: number,
  extra: DispatchExtra | undefined,
): Promise<string> {
  const managerUrl = process.env["MANAGER_URL"] ?? "http://manager:8090";
  const requestId = crypto.randomUUID();

  const body = JSON.stringify({
    request_id: requestId,
    session_id: sessionId,
    content,
    capability,
    resource,
    timestamp: Math.floor(Date.now() / 1000),
  });

  log(`→ HTTP stream dispatch (fallback) to ${managerUrl}/dispatch  requestId=${requestId}`);

  const abortCtrl = new AbortController();
  let inactivityTimer: ReturnType<typeof setTimeout> | null = null;
  let timedOut: string | null = null;

  const resetInactivity = () => {
    if (inactivityTimer) clearTimeout(inactivityTimer);
    inactivityTimer = setTimeout(() => {
      timedOut = `no progress for ${inactivityMs}ms`;
      abortCtrl.abort();
    }, inactivityMs);
  };
  const absoluteTimer = setTimeout(() => {
    timedOut = `exceeded ${maxMs}ms`;
    abortCtrl.abort();
  }, maxMs);
  resetInactivity();

  try {
    const resp = await fetch(`${managerUrl}/dispatch`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "Accept": "application/x-ndjson" },
      body,
      signal: abortCtrl.signal,
    });
    if (!resp.ok) throw new Error(`manager HTTP ${resp.status}`);
    if (!resp.body) throw new Error("empty body");

    const reader = resp.body.getReader();
    const decoder = new TextDecoder("utf-8");
    let buf = "";
    let result = "";
    let errMsg = "";

    // eslint-disable-next-line no-constant-condition
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      resetInactivity();
      buf += decoder.decode(value, { stream: true });
      let idx = buf.indexOf("\n");
      while (idx !== -1) {
        const line = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 1);
        idx = buf.indexOf("\n");
        if (!line) continue;
        try {
          const evt = JSON.parse(line) as ProgressEvent;
          if (evt.type === "done") result = evt.message ?? "";
          else if (evt.type === "error") errMsg = evt.error ?? "unknown error";
          if (extra?.sendNotification && extra._meta?.progressToken !== undefined) {
            void extra.sendNotification({
              method: "notifications/progress",
              params: {
                progressToken: extra._meta.progressToken,
                progress: 1,
                message: evt.type,
              },
            });
          }
        } catch {
          // ignore malformed line
        }
      }
    }
    if (errMsg) throw new Error(errMsg);
    return result;
  } catch (err) {
    if (timedOut) throw new Error(`HTTP dispatch timed out: ${timedOut}`);
    throw err;
  } finally {
    if (inactivityTimer) clearTimeout(inactivityTimer);
    clearTimeout(absoluteTimer);
  }
}

// ── Tool registration ──────────────────────────────────────────────────────────

export function registerDelegateTools(server: McpServer): void {
  server.registerTool(
    "delegate_to_manager",
    {
      title: "Delegate Task to Sage Manager",
      description: `Delegate ANY task to the Sage orchestrator. The orchestrator picks the right specialist automatically — you do not choose which agent handles it.

Specialists available through the manager:
- Project planning, requirements, acceptance criteria, task breakdown
- Senior engineering review and technical quality gates
- Frontend implementation (UI, components, styling, accessibility)
- Backend implementation (APIs, services, business logic)
- DevOps, CI/CD, deployments, infrastructure
- QA, test strategy, regression validation
- Database schema, migrations, query optimization
- Architecture and system design decisions
- Research, web search, documentation lookups, technical deep dives
- Personal finance, budget, spending, savings (live read-only budget access)

The manager enforces ACP admission control before executing any task.

Use this for ALL tasks — code, research, planning, finance, architecture, anything. Do not try to answer technical, factual, or domain-specific questions yourself.

${SOUL_INSTRUCTIONS}

This tool publishes the task to the Redis A2A bus (sage:tasks) and waits for status events on sage:events. Heartbeat events arrive every ~5s while the manager is processing — the inactivity timer is reset on every event for this task, so deep research can run for several minutes without aborting.

Args:
  - content: The task description or question for the research agent
  - session_id: (optional) Current session ID — used as A2A contextId for correlation
  - capability: (optional) ACP capability string (default: acp:cap:skill.agent-delegate)
  - resource: (optional) ACP resource scope (default: sage://workspace/*)
  - inactivity_ms: (optional) Abort if no event arrives for this long (default: 300000)
  - max_ms: (optional) Hard safety cap on total dispatch time (default: 1800000)

Returns raw research output. Reformat in your voice before responding to the user.`,
      inputSchema: z.object({
        content: z.string().min(1).describe("Task description or instruction for the manager"),
        session_id: z.string().optional().describe("Current session ID for A2A contextId correlation"),
        capability: z.string().optional().describe("ACP capability (default: acp:cap:skill.agent-delegate)"),
        resource: z.string().optional().describe("ACP resource scope (default: sage://workspace/*)"),
        inactivity_ms: z.number().int().min(5_000).max(600_000).optional().describe("Silence-based abort threshold in ms (default: 300000)"),
        max_ms: z.number().int().min(30_000).max(1_800_000).optional().describe("Absolute dispatch cap in ms (default: 1800000)"),
      }),
    },
    async ({ content, session_id, capability, resource, inactivity_ms, max_ms }, extra) => {
      log(`→ delegate_to_manager  content="${content.slice(0, 80)}${content.length > 80 ? "…" : ""}"`);

      const cap = capability ?? "acp:cap:skill.agent-delegate";
      const res = resource ?? "sage://workspace/*";
      const inactivityMs = inactivity_ms ?? 300_000;
      const maxMs = max_ms ?? 1_800_000;

      const ex = extra as DispatchExtra | undefined;

      try {
        // Redis A2A bus is the primary path. Falls back to HTTP only if no
        // REDIS_ADDR is configured (CLI/dev environments without Redis).
        const result = process.env["REDIS_ADDR"]
          ? await dispatchA2A(content, session_id, cap, res, inactivityMs, maxMs, ex)
          : await dispatchHTTPStream(content, session_id, cap, res, inactivityMs, maxMs, ex);

        // Paused payload: the manager hit its round cap and is waiting on
        // the human. Return a structured JSON block so Sage's prompt rule
        // (SOUL.md) recognizes this as "ask the user, then call
        // delegate_continue", not as a final answer.
        if (typeof result === "object" && result !== null && "paused" in result) {
          return {
            content: [{
              type: "text" as const,
              text: JSON.stringify(result, null, 2),
            }],
          };
        }
        return { content: [{ type: "text" as const, text: result as string }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        log(`ERROR: ${msg}`);
        return {
          content: [{ type: "text" as const, text: `Manager error: ${msg}` }],
          isError: true,
        };
      }
    },
  );
}
