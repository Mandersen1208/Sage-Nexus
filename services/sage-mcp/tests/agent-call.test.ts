import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { callAgent } from "../src/tools/agent-call.js";

async function readBody(req: IncomingMessage): Promise<string> {
  return await new Promise((resolve) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf-8")));
  });
}

function json(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

let received: Record<string, unknown> | undefined;
const server = createServer(async (req, res) => {
  if (req.method !== "POST" || req.url !== "/agent-dispatch") {
    json(res, 404, { error: "not found" });
    return;
  }
  received = JSON.parse(await readBody(req)) as Record<string, unknown>;
  if (received["target_agent_id"] === "AGT-runtime-librarian-agent") {
    json(res, 403, { error: "peer call not allowed" });
    return;
  }
  json(res, 200, { reply: "backend contract confirmed", agent: received["target_agent_id"] });
});

await new Promise<void>((resolve) => server.listen(0, resolve));
const addr = server.address();
assert.equal(typeof addr, "object");
process.env["MANAGER_URL"] = `http://127.0.0.1:${(addr as AddressInfo).port}`;

const ok = await callAgent({
  caller_agent_id: "AGT-frontend-dev-agent",
  agent_id: "AGT-backend-dev-agent",
  query: "Confirm the session API contract.",
  reason: "frontend needs backend contract",
  depth: 1,
  work_context_id: "wc-test",
  token: "wct-test",
});
assert.equal(ok.reply, "backend contract confirmed");
assert.equal(received?.["caller_agent_id"], "AGT-frontend-dev-agent");
assert.equal(received?.["target_agent_id"], "AGT-backend-dev-agent");
assert.equal(received?.["reason"], "frontend needs backend contract");
assert.equal(received?.["depth"], 1);
assert.equal(received?.["work_context_id"], "wc-test");
assert.equal(received?.["token"], "wct-test");

const denied = await callAgent({
  caller_agent_id: "AGT-frontend-dev-agent",
  agent_id: "AGT-runtime-librarian-agent",
  query: "Take over this task.",
  reason: "invalid ownership transfer",
});
assert.equal(denied.error, "peer call not allowed");

server.close();
console.log("agent call tests passed");
