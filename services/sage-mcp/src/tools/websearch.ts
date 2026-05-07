import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const SEARXNG_URL = process.env.SEARXNG_URL ?? "http://searxng:8080";

interface SearXNGResult {
  title: string;
  url: string;
  content?: string;
  engine?: string;
}

interface SearXNGResponse {
  results: SearXNGResult[];
  suggestions?: string[];
  unresponsive_engines?: Array<[string, string]>;
  number_of_results?: number;
}

interface SearchOutcome {
  text: string;
  isError: boolean;
}

async function searchWeb(query: string, numResults = 8): Promise<SearchOutcome> {
  const url = `${SEARXNG_URL}/search?q=${encodeURIComponent(query)}&format=json&categories=general`;

  console.error(`[web_search] Searching: ${query} → ${url}`);

  let resp: Response;
  try {
    resp = await fetch(url, {
      headers: {
        "Accept": "application/json",
        "User-Agent": "sage-mcp/1.0",
      },
      signal: AbortSignal.timeout(15_000),
    });
  } catch (err) {
    throw new Error(`SearXNG request failed: ${err instanceof Error ? err.message : String(err)}`);
  }

  if (!resp.ok) {
    throw new Error(`SearXNG returned HTTP ${resp.status}`);
  }

  const data = (await resp.json()) as SearXNGResponse;
  const allResults = data.results ?? [];
  const results = allResults.slice(0, numResults);
  const failures = data.unresponsive_engines ?? [];
  const failureSummary = failures.map(([name, reason]) => `${name} (${reason})`).join(", ");

  if (failures.length > 0) {
    console.error(`[web_search] unresponsive engines: ${failureSummary}`);
  }

  // Hard-fail when the search produced nothing usable. Returning isError=true
  // tells the research agent's tool loop this is a real failure — don't proceed
  // to fabricate results from an empty well.
  if (allResults.length === 0) {
    const reason = failures.length > 0
      ? `All queryable engines failed or returned nothing. Unresponsive: ${failureSummary}.`
      : `SearXNG returned zero results for this query.`;
    return {
      isError: true,
      text: `search failed: ${reason} Do not fabricate citations. Tell the user the search came back empty and stop.`,
    };
  }

  const lines: string[] = [];
  if (failures.length > 0) {
    lines.push(`NOTE: ${failures.length} search engine(s) failed or were rate-limited: ${failureSummary}`);
    lines.push("");
  }
  lines.push(`${allResults.length} result(s) returned (showing ${results.length}):`);
  for (let i = 0; i < results.length; i++) {
    const r = results[i];
    lines.push(`[${i + 1}] ${r.title}`);
    lines.push(`    URL: ${r.url}`);
    if (r.engine) lines.push(`    engine: ${r.engine}`);
    if (r.content) {
      lines.push(`    ${r.content.trim().slice(0, 300)}`);
    }
  }

  return { isError: false, text: lines.join("\n") };
}

export function registerWebSearchTools(server: McpServer): void {
  server.tool(
    "searxng_search",
    "Search the web using SearXNG. Use this for current information, documentation lookups, recent releases, or anything not in the skill library.",
    {
      query: z.string().describe("The search query"),
      num_results: z
        .number()
        .int()
        .min(1)
        .max(20)
        .optional()
        .describe("Number of results to return (default 8, max 20)"),
    },
    async ({ query, num_results }) => {
      try {
        const { text, isError } = await searchWeb(query, num_results ?? 8);
        return { content: [{ type: "text", text }], isError };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[web_search] Error:`, msg);
        return {
          content: [{ type: "text", text: `web_search error: ${msg}` }],
          isError: true,
        };
      }
    }
  );
}
