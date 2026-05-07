import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

function log(msg: string): void {
  process.stderr.write(`[agent-call] ${new Date().toISOString()} ${msg}\n`);
}

function managerUrl(): string {
  return process.env["MANAGER_URL"] ?? "http://manager:8090";
}

export interface AgentCallInput {
  caller_agent_id?: string;
  agent_id: string;
  query: string;
  reason: string;
  depth?: number;
  work_context_id?: string;
  token?: string;
}

export interface AgentCallResult {
  reply?: string;
  error?: string;
  agent?: string;
}

export async function callAgent(input: AgentCallInput, fetchImpl: typeof fetch = fetch): Promise<AgentCallResult> {
  log(`call_agent ${input.caller_agent_id ?? "unknown"} -> ${input.agent_id} depth=${input.depth ?? 0} reason="${input.reason.slice(0, 80)}"`);
  const body = JSON.stringify({
    caller_agent_id: input.caller_agent_id,
    target_agent_id: input.agent_id,
    content: input.query,
    reason: input.reason,
    depth: input.depth ?? 0,
    work_context_id: input.work_context_id,
    token: input.token,
  });

  const res = await fetchImpl(`${managerUrl()}/agent-dispatch`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  }).catch((e) => {
    throw new Error(`Could not reach manager: ${e instanceof Error ? e.message : String(e)}`);
  });

  const data = (await res.json()) as AgentCallResult;
  if (!res.ok || data.error) {
    return { error: data.error ?? String(res.status), agent: data.agent };
  }
  log(`call_agent <- ${input.agent_id} replyLen=${data.reply?.length ?? 0}`);
  return data;
}

export function registerAgentCallTools(server: McpServer): void {
  server.registerTool(
    "list_agents",
    {
      title: "List Available Agents",
      description:
        "Returns the IDs of all specialist agents registered with the manager. " +
        "Call this before call_agent if you are unsure which agent to use.",
      inputSchema: z.object({}),
    },
    async () => {
      log("list_agents called");
      const res = await fetch(`${managerUrl()}/agents/list`).catch((e) => {
        throw new Error(`Could not reach manager: ${e instanceof Error ? e.message : String(e)}`);
      });
      if (!res.ok) throw new Error(`Manager returned ${res.status}`);
      const ids = (await res.json()) as string[];
      return {
        content: [
          {
            type: "text" as const,
            text: ids.length
              ? `Available agents:\n${ids.map((id) => `  - ${id}`).join("\n")}`
              : "No agents registered.",
          },
        ],
      };
    },
  );

  server.registerTool(
    "call_agent",
    {
      title: "Call a Specialist Agent",
      description:
        "Directly invoke an allowlisted peer specialist agent by ID. Use this for bounded peer input, " +
        "contract clarification, domain pushback, or research evidence. The manager enforces caller/target policy, " +
        "depth, and work-context recording.",
      inputSchema: z.object({
        caller_agent_id: z.string().optional().describe("Injected by the manager-side worker runtime; leave unset"),
        agent_id: z.string().min(1).describe("The target agent ID, e.g. AGT-backend-dev-agent"),
        query: z.string().min(1).describe("The full question or task for the target agent"),
        reason: z.string().min(1).describe("Why this peer's domain authority is needed"),
        depth: z.number().int().min(0).max(10).optional().describe("Current call depth, injected by the worker runtime"),
        work_context_id: z.string().optional().describe("Injected work-context id for shared task memory"),
        token: z.string().optional().describe("Injected scoped work-context token"),
      }).strict(),
    },
    async (input) => {
      const data = await callAgent(input);
      if (data.error) {
        return {
          content: [{ type: "text" as const, text: `Agent error: ${data.error}` }],
          isError: true,
        };
      }
      return {
        content: [{ type: "text" as const, text: data.reply ?? "" }],
      };
    },
  );
}
