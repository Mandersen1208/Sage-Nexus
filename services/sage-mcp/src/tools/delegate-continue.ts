import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import { RedisClient } from "../redis-client.js";
import { ChannelControl, type ControlMessage } from "../a2a/types.js";

function log(msg: string): void {
  process.stderr.write(`[delegate-continue] ${new Date().toISOString()} ${msg}\n`);
}

let pubClient: RedisClient | null = null;

async function getPub(): Promise<RedisClient | null> {
  const addr = process.env["REDIS_ADDR"];
  if (!addr) return null;
  if (pubClient) return pubClient;
  const [host, portStr] = addr.split(":");
  const port = parseInt(portStr ?? "6379", 10);
  const password = process.env["REDIS_PASSWORD"] || undefined;
  pubClient = new RedisClient(host, port, password);
  await pubClient.connect();
  log(`pub client connected to ${addr}`);
  return pubClient;
}

export function registerDelegateContinueTool(server: McpServer): void {
  server.registerTool(
    "delegate_continue",
    {
      title: "Continue or Stop a Paused Manager Task",
      description: `Resume or finalize a manager task that hit its round cap.

When delegate_to_manager returns a paused payload (paused: true), the manager
is awaiting your decision. Present the partial work to the user, ask them
whether to continue, then call this tool with their answer.

Args:
  - task_id: The taskId from the paused payload.
  - decision: "continue" to push another round, "stop" to finalize with what
    we have.
  - note: (optional) free-text guidance from the user — e.g. "focus on what
    senior-dev was concerned about" — passed back to the orchestrator to
    steer the next batch of rounds.

This tool returns immediately after publishing the control message. Then call
delegate_to_manager again ONLY if you want a fresh task; the existing paused
task resumes on its own.`,
      inputSchema: z.object({
        task_id: z.string().min(1).describe("The taskId from the paused delegate_to_manager response"),
        decision: z.enum(["continue", "stop"]).describe("continue to keep going; stop to finalize with the partial reply"),
        note: z.string().optional().describe("Optional human guidance for the next batch of orchestration rounds"),
      }),
    },
    async ({ task_id, decision, note }) => {
      log(`→ delegate_continue  taskId=${task_id}  decision=${decision}`);
      const pub = await getPub();
      if (!pub) {
        return {
          content: [{ type: "text" as const, text: "REDIS_ADDR not configured — cannot publish continuation" }],
          isError: true,
        };
      }
      const msg: ControlMessage = { taskId: task_id, decision, note };
      try {
        await pub.publish(ChannelControl, JSON.stringify(msg));
      } catch (err) {
        const m = err instanceof Error ? err.message : String(err);
        return {
          content: [{ type: "text" as const, text: `failed to publish continuation: ${m}` }],
          isError: true,
        };
      }
      return {
        content: [{
          type: "text" as const,
          text: decision === "continue"
            ? `Continuation sent. The manager will resume orchestration; expect more events on the bus for taskId=${task_id}.`
            : `Stop sent. The manager will finalize with the partial reply for taskId=${task_id}.`,
        }],
      };
    },
  );
}
