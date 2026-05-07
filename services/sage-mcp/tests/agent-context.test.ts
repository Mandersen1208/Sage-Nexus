import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { appendAgentContext, readAgentContext, searchAgentContext } from "../src/tools/agent-context.js";

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

const seen: string[] = [];
const server = createServer(async (req, res) => {
  const auth = req.headers["authorization"];
  if (auth !== "Bearer good-token") {
    json(res, 401, { error: "bad token" });
    return;
  }
  seen.push(`${req.method} ${req.url}`);

  if (req.method === "GET" && req.url?.startsWith("/work-context/wc-test")) {
    json(res, 200, {
      meta: { id: "wc-test", taskId: "task-1" },
      events: [
        { id: "1", kind: "finding", actor: "AGT-runtime-librarian-agent", summary: "Redis context exists" },
        { id: "2", kind: "decision", actor: "AGT-senior-dev-agent", summary: "Use manager-owned schema" },
        { id: "3", kind: "research_brief", actor: "AGT-research-agent", summary: "RedisVL can support vector search" },
      ],
    });
    return;
  }

  if (req.method === "POST" && req.url === "/work-context/wc-test/events") {
    const body = JSON.parse(await readBody(req)) as Record<string, unknown>;
    assert.equal(body["kind"], "finding");
    assert.equal(body["summary"], "captured note");
    json(res, 200, { id: "3", kind: body["kind"], summary: body["summary"] });
    return;
  }

  json(res, 404, { error: "not found" });
});

await new Promise<void>((resolve) => server.listen(0, resolve));
const addr = server.address();
assert.equal(typeof addr, "object");
process.env["MANAGER_URL"] = `http://127.0.0.1:${(addr as AddressInfo).port}`;

const detail = await readAgentContext({ work_context_id: "wc-test", token: "good-token", kind: "finding", limit: 10 });
assert.equal(detail.events?.length, 3);
assert.equal(seen.some((entry) => entry.includes("kind=finding")), true);

const appended = await appendAgentContext({
  work_context_id: "wc-test",
  token: "good-token",
  kind: "finding",
  summary: "captured note",
});
assert.equal(appended.summary, "captured note");

const search = await searchAgentContext({ work_context_id: "wc-test", token: "good-token", query: "context exists", limit: 5 });
assert.equal(search.count, 1);
assert.equal(search.results[0]?.summary, "Redis context exists");

const research = await searchAgentContext({ work_context_id: "wc-test", token: "good-token", query: "vector", kind: "research_brief", limit: 5 });
assert.equal(research.count, 1);
assert.equal(research.results[0]?.summary, "RedisVL can support vector search");

await assert.rejects(
  () => readAgentContext({ work_context_id: "wc-test", token: "bad-token" }),
  /manager returned 401/,
);

server.close();
console.log("agent context tests passed");
