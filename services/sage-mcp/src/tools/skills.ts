import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { CHARACTER_LIMIT } from "../constants.js";
import {
  listSkills,
  getSkillContent,
  listSkillReferences,
  getSkillReference,
  searchSkills,
} from "../services/skills.js";
import { getSkillDiscoveryRuntime } from "../services/skill-discovery.js";

export function registerSkillTools(server: McpServer): void {

  // ── skill_list ─────────────────────────────────────────────────────
  server.registerTool(
    "skill_list",
    {
      title: "List Available Skills",
      description: `List all available dev skills in Sage's knowledge library.

Returns each skill's id, name, description, and tags. Use this to discover what's available before calling skill_get or skill_search.

Returns:
  Array of skill summaries:
  [
    {
      "id": string,          // Unique identifier (use in other tool calls)
      "name": string,        // Human-readable name
      "description": string, // What the skill covers
      "tags": string[]       // Topic tags for filtering
    }
  ]

Examples:
  - "What skills are available?" → call skill_list
  - "What do you know about React?" → call skill_list then skill_search with query="react"`,
      inputSchema: z.object({}).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async () => {
      const skills = listSkills();
      const output = { count: skills.length, skills };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    }
  );

  // ── skill_search ───────────────────────────────────────────────────
  server.registerTool(
    "skill_search",
    {
      title: "Search Skills by Keyword",
      description: `Search for skills by keyword. Matches against skill name, description, and tags.

Args:
  - query (string): Keyword to search for (e.g. "react", "postgres", "index", "hooks")

Returns:
  Array of matching skill summaries (same format as skill_list). Empty array if nothing matches.

Examples:
  - Writing SQL queries → skill_search({ query: "postgres" })
  - Working on React components → skill_search({ query: "react" })
  - Need indexing guidance → skill_search({ query: "index" })`,
      inputSchema: z.object({
        query: z.string().min(1).max(100).describe("Search keyword to match against skill names, descriptions, and tags"),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ query }) => {
      const results = searchSkills(query);
      const output = { query, count: results.length, skills: results };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    }
  );

  // ── skill_get ──────────────────────────────────────────────────────
  server.registerTool(
    "skill_get",
    {
      title: "Get Skill Content",
      description: `Read the main content of a skill by its id.

For skills with many reference files, the main SKILL.md gives an overview and rule categories.
Use skill_list_references + skill_get_reference to drill into specific topics.

Args:
  - id (string): Skill id from skill_list (e.g. "react-best-practices", "supabase-postgres-best-practices")

Returns:
  {
    "id": string,
    "name": string,
    "content": string,   // Full markdown content of the skill
    "truncated": boolean // True if content was cut off — use skill_list_references to explore further
  }

Error: Returns error message if skill id is not found.`,
      inputSchema: z.object({
        id: z.string().min(1).describe("Skill id — get valid ids from skill_list"),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ id }) => {
      const result = getSkillContent(id, CHARACTER_LIMIT);
      if (!result) {
        return {
          content: [{ type: "text", text: `Error: Skill '${id}' not found. Call skill_list to see available skills.` }],
        };
      }
      return {
        content: [{ type: "text", text: result.content }],
        structuredContent: { id: result.id, name: result.name, truncated: result.truncated },
      };
    }
  );

  // ── skill_list_references ──────────────────────────────────────────
  server.registerTool(
    "skill_list_references",
    {
      title: "List Skill Reference Files",
      description: `List all reference files available for a skill.

Many skills (like supabase-postgres-best-practices) have a references/ directory with detailed
per-topic files. Use this to discover what's available, then call skill_get_reference to read one.

Args:
  - id (string): Skill id from skill_list

Returns:
  {
    "id": string,
    "references": [
      { "name": string }  // Reference file name (use as ref_name in skill_get_reference)
    ]
  }

Returns empty references array if the skill has no reference files.`,
      inputSchema: z.object({
        id: z.string().min(1).describe("Skill id — get valid ids from skill_list"),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ id }) => {
      const refs = listSkillReferences(id);
      const output = { id, references: refs.map(r => ({ name: r.name })) };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    }
  );

  // ── skill_get_reference ────────────────────────────────────────────
  server.registerTool(
    "skill_get_reference",
    {
      title: "Get Skill Reference File",
      description: `Read a specific reference file from a skill's references/ directory.

Use skill_list_references first to see what ref_name values are available.

Args:
  - id (string): Skill id (e.g. "supabase-postgres-best-practices")
  - ref_name (string): Reference file name without .md extension (e.g. "query-missing-indexes", "schema-foreign-key-indexes")

Returns the full markdown content of the reference file.

Examples:
  - Need index advice → skill_get_reference({ id: "supabase-postgres-best-practices", ref_name: "query-missing-indexes" })
  - React hooks patterns → skill_get_reference({ id: "react-best-practices", ref_name: "hooks-patterns" })

Error: Returns error if skill id or ref_name is not found.`,
      inputSchema: z.object({
        id: z.string().min(1).describe("Skill id"),
        ref_name: z.string().min(1).describe("Reference file name without .md extension — get valid names from skill_list_references"),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ id, ref_name }) => {
      const result = getSkillReference(id, ref_name, CHARACTER_LIMIT);
      if (!result) {
        return {
          content: [{ type: "text", text: `Error: Reference '${ref_name}' not found in skill '${id}'. Call skill_list_references to see available references.` }],
        };
      }
      return {
        content: [{ type: "text", text: result.content }],
        structuredContent: { id: result.id, name: result.name, truncated: result.truncated },
      };
    }
  );

  server.registerTool(
    "skill_registry_sources",
    {
      title: "List Canonical Skill Sources",
      description: "List registered external/local skill sources in Sage MCP's canonical registry.",
      inputSchema: z.object({
        include_local: z.boolean().optional(),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ include_local }) => {
      const runtime = await getSkillDiscoveryRuntime();
      const sources = await runtime.listSources(include_local ?? false);
      const output = { count: sources.length, sources };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "skill_registry_sync",
    {
      title: "Sync Canonical Skill Registry",
      description: "Poll one source or all enabled sources and update canonical skills in pgvector.",
      inputSchema: z.object({
        source_id: z.string().optional(),
      }).strict(),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ source_id }) => {
      const runtime = await getSkillDiscoveryRuntime();
      if (source_id && source_id.trim() !== "") {
        const result = await runtime.syncSource(source_id.trim());
        const output = { mode: "single", ...result };
        return {
          content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
          structuredContent: output,
        };
      }
      const result = await runtime.syncAllSources();
      const output = { mode: "all", ...result };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "skill_registry_search",
    {
      title: "Search Canonical Skills",
      description: "Semantic search over released canonical skills indexed in pgvector.",
      inputSchema: z.object({
        query: z.string().min(1).max(200),
        agent_id: z.string().optional(),
        limit: z.number().int().min(1).max(20).optional(),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ query, agent_id, limit }) => {
      const runtime = await getSkillDiscoveryRuntime();
      const skills = await runtime.searchSkills({
        query,
        agentId: agent_id?.trim(),
        limit,
      });
      const output = { query, count: skills.length, skills };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "skill_registry_get",
    {
      title: "Get Canonical Skill",
      description: "Read one canonical skill by id from the discovery registry.",
      inputSchema: z.object({
        id: z.string().min(1),
      }).strict(),
      annotations: {
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async ({ id }) => {
      const runtime = await getSkillDiscoveryRuntime();
      const skills = await runtime.listSkills();
      const found = skills.find((item) => item.id === id);
      if (!found) {
        return {
          content: [{ type: "text", text: `Error: canonical skill '${id}' not found.` }],
        };
      }
      return {
        content: [{ type: "text", text: JSON.stringify(found, null, 2) }],
        structuredContent: found as unknown as Record<string, unknown>,
      };
    },
  );
}
