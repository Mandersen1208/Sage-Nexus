import crypto from "node:crypto";
import { Pool } from "pg";
import { listSkills } from "./skills.js";

export type SourceTrust = "trusted" | "untrusted";
export type SkillState = "quarantined" | "released" | "disabled";
export type SourceType = "remote" | "local";

export interface SkillSourceRecord {
  id: string;
  displayName: string;
  endpoint: string;
  trust: SourceTrust;
  enabled: boolean;
  sourceType: SourceType;
  lastSyncAt?: string;
  lastSyncStatus?: string;
  lastSyncError?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CanonicalSkillRecord {
  id: string;
  sourceId: string;
  sourceName: string;
  originalToolName: string;
  canonicalName: string;
  description: string;
  tags: string[];
  metadataQuality: number;
  riskLevel: "low" | "medium" | "high";
  executionType: "stateless" | "stateful";
  requiresSession: boolean;
  skillState: SkillState;
  allowedAgents: string[];
  avgLatencyMs: number;
  successRate: number;
  discoveredAt: string;
  updatedAt: string;
}

interface RemoteToolShape {
  name?: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
  tags?: unknown;
  categories?: unknown;
  metadata?: Record<string, unknown>;
  annotations?: Record<string, unknown>;
  examples?: unknown;
}

interface NormalizedTool {
  sourceId: string;
  sourceName: string;
  originalToolName: string;
  canonicalName: string;
  description: string;
  tags: string[];
  metadataQuality: number;
  riskLevel: "low" | "medium" | "high";
  executionType: "stateless" | "stateful";
  requiresSession: boolean;
  embeddingText: string;
}

interface SearchOptions {
  query: string;
  agentId?: string;
  limit?: number;
}

interface SkillListFilters {
  sourceId?: string;
  state?: SkillState;
  includeLocal?: boolean;
}

interface SkillStatePatch {
  state?: SkillState;
  allowedAgents?: string[];
}

interface SourcePatch {
  displayName?: string;
  endpoint?: string;
  trust?: SourceTrust;
  enabled?: boolean;
}

interface SourceUpsertInput {
  id: string;
  displayName: string;
  endpoint: string;
  trust?: SourceTrust;
  enabled?: boolean;
  sourceType?: SourceType;
}

interface PoolRow {
  [key: string]: unknown;
}

export class SkillDiscoveryRuntime {
  private readonly pool: Pool;
  private readonly embeddingDim: number;
  private readonly retrievalTopK: number;
  private readonly openAiApiKey: string;
  private readonly openAiBaseUrl: string;
  private readonly openAiEmbeddingModel: string;
  private readonly openAiEmbeddingEnabled: boolean;
  private schedulerStarted = false;
  private schedulerTimer: NodeJS.Timeout | null = null;

  constructor() {
    const connectionString = process.env["SKILL_DB_URL"]?.trim() || "";
    this.pool = new Pool(
      connectionString
        ? {
            connectionString,
            ssl: parseBool(process.env["SKILL_DB_SSL"]),
          }
        : {
            host: process.env["SKILL_DB_HOST"] || "skills-db",
            port: parseIntSafe(process.env["SKILL_DB_PORT"], 5432),
            database: process.env["SKILL_DB_NAME"] || "sage",
            user: process.env["SKILL_DB_USER"] || "sage",
            password: process.env["SKILL_DB_PASSWORD"] || "sage",
            ssl: parseBool(process.env["SKILL_DB_SSL"]),
          },
    );
    this.embeddingDim = parseIntSafe(process.env["SKILL_EMBEDDING_DIM"], 64, 16, 1536);
    this.retrievalTopK = parseIntSafe(process.env["SKILL_RETRIEVAL_TOP_K"], 5, 1, 20);
    this.openAiApiKey = (process.env["OPENAI_API_KEY"] || "").trim();
    this.openAiBaseUrl = (process.env["OPENAI_BASE_URL"] || "https://api.openai.com").trim().replace(/\/+$/, "");
    this.openAiEmbeddingModel = (process.env["SKILL_EMBEDDING_MODEL"] || "text-embedding-3-small").trim();
    this.openAiEmbeddingEnabled = this.openAiApiKey.length > 0;
  }

  async init(): Promise<void> {
    await this.ensureSchema();
    await this.seedSourcesFromEnv();
    await this.ensureLocalSource();
  }

  startDailySyncScheduler(): void {
    if (this.schedulerStarted) return;
    this.schedulerStarted = true;
    this.scheduleNextDailyRun();
  }

  stopScheduler(): void {
    if (this.schedulerTimer) {
      clearTimeout(this.schedulerTimer);
      this.schedulerTimer = null;
    }
    this.schedulerStarted = false;
  }

  private scheduleNextDailyRun(): void {
    const now = new Date();
    const next = nextDailyLocalTwoAM(now);
    const delayMs = Math.max(10_000, next.getTime() - now.getTime());
    this.schedulerTimer = setTimeout(() => {
      void this.syncAllSources()
        .catch((err) => {
          process.stderr.write(`[skill-discovery] daily sync failed: ${stringErr(err)}\n`);
        })
        .finally(() => {
          this.scheduleNextDailyRun();
        });
    }, delayMs);
  }

  async listSources(includeLocal = false): Promise<SkillSourceRecord[]> {
    const where = includeLocal ? "" : "WHERE source_type <> 'local'";
    const { rows } = await this.pool.query(
      `SELECT id, display_name, endpoint, trust, enabled, source_type, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at
       FROM mcp_skill_sources
       ${where}
       ORDER BY source_type ASC, display_name ASC, id ASC`,
    );
    return rows.map((row) => mapSourceRow(row));
  }

  async upsertSource(input: SourceUpsertInput): Promise<SkillSourceRecord> {
    const id = normalizeSourceId(input.id);
    if (!id) throw new Error("source id is required");
    const displayName = (input.displayName || id).trim();
    const endpoint = (input.endpoint || "").trim();
    if (!endpoint) throw new Error("endpoint is required");
    const trust = input.trust || "untrusted";
    const enabled = input.enabled ?? true;
    const sourceType = input.sourceType || "remote";

    const { rows } = await this.pool.query(
      `INSERT INTO mcp_skill_sources (id, display_name, endpoint, trust, enabled, source_type)
       VALUES ($1, $2, $3, $4, $5, $6)
       ON CONFLICT (id)
       DO UPDATE SET display_name = EXCLUDED.display_name,
                     endpoint = EXCLUDED.endpoint,
                     trust = EXCLUDED.trust,
                     enabled = EXCLUDED.enabled,
                     source_type = EXCLUDED.source_type,
                     updated_at = NOW()
       RETURNING id, display_name, endpoint, trust, enabled, source_type, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at`,
      [id, displayName, endpoint, trust, enabled, sourceType],
    );
    return mapSourceRow(rows[0] as PoolRow);
  }

  async patchSource(sourceId: string, patch: SourcePatch): Promise<SkillSourceRecord> {
    const id = normalizeSourceId(sourceId);
    if (!id) throw new Error("source id is required");
    const source = await this.getSourceByID(id);
    if (!source) throw new Error(`source ${id} not found`);

    const nextDisplayName = patch.displayName?.trim() || source.displayName;
    const nextEndpoint = patch.endpoint?.trim() || source.endpoint;
    const nextTrust = patch.trust || source.trust;
    const nextEnabled = patch.enabled ?? source.enabled;

    const { rows } = await this.pool.query(
      `UPDATE mcp_skill_sources
       SET display_name = $2,
           endpoint = $3,
           trust = $4,
           enabled = $5,
           updated_at = NOW()
       WHERE id = $1
       RETURNING id, display_name, endpoint, trust, enabled, source_type, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at`,
      [id, nextDisplayName, nextEndpoint, nextTrust, nextEnabled],
    );
    return mapSourceRow(rows[0] as PoolRow);
  }

  async releaseSourceSkills(sourceId: string): Promise<number> {
    const id = normalizeSourceId(sourceId);
    if (!id) throw new Error("source id is required");
    const { rowCount } = await this.pool.query(
      `UPDATE canonical_skills
       SET skill_state = 'released',
           updated_at = NOW()
       WHERE source_id = $1 AND skill_state = 'quarantined'`,
      [id],
    );
    return rowCount || 0;
  }

  async listSkills(filters: SkillListFilters = {}): Promise<CanonicalSkillRecord[]> {
    const where: string[] = [];
    const values: unknown[] = [];

    if (!filters.includeLocal) {
      where.push("src.source_type <> 'local'");
    }
    if (filters.sourceId) {
      values.push(normalizeSourceId(filters.sourceId));
      where.push(`s.source_id = $${values.length}`);
    }
    if (filters.state) {
      values.push(filters.state);
      where.push(`s.skill_state = $${values.length}`);
    }

    const sql = `SELECT s.id, s.source_id, src.display_name AS source_name, s.original_tool_name, s.canonical_name,
                        s.description, s.tags, s.metadata_quality, s.risk_level, s.execution_type,
                        s.requires_session, s.skill_state, s.allowed_agents, s.avg_latency_ms,
                        s.success_rate, s.discovered_at, s.updated_at
                 FROM canonical_skills s
                 INNER JOIN mcp_skill_sources src ON src.id = s.source_id
                 ${where.length ? `WHERE ${where.join(" AND ")}` : ""}
                 ORDER BY src.display_name ASC, s.canonical_name ASC`;

    const { rows } = await this.pool.query(sql, values);
    return rows.map((row) => mapSkillRow(row as PoolRow));
  }

  async patchSkill(skillID: string, patch: SkillStatePatch): Promise<CanonicalSkillRecord> {
    const id = normalizeSkillID(skillID);
    if (!id) throw new Error("skill id is required");
    const current = await this.getSkillByID(id);
    if (!current) throw new Error(`skill ${id} not found`);

    const nextState = patch.state || current.skillState;
    const nextAllowed = patch.allowedAgents ? uniqueStrings(patch.allowedAgents) : current.allowedAgents;

    const { rows } = await this.pool.query(
      `UPDATE canonical_skills
       SET skill_state = $2,
           allowed_agents = $3,
           updated_at = NOW()
       WHERE id = $1
       RETURNING id, source_id, source_name, original_tool_name, canonical_name, description, tags,
                 metadata_quality, risk_level, execution_type, requires_session, skill_state, allowed_agents,
                 avg_latency_ms, success_rate, discovered_at, updated_at`,
      [id, nextState, nextAllowed],
    );

    if (!rows[0]) {
      const fallback = await this.getSkillByID(id);
      if (fallback) return fallback;
      throw new Error(`skill ${id} not found`);
    }
    return mapSkillRow(rows[0] as PoolRow);
  }

  async searchSkills(options: SearchOptions): Promise<CanonicalSkillRecord[]> {
    const query = options.query.trim();
    if (!query) return [];
    const limit = clampInt(options.limit ?? this.retrievalTopK, 1, 20);
    const embedding = await this.embedText(query);
    const embeddingLiteral = vectorLiteral(embedding);
    const args: unknown[] = [embeddingLiteral, limit];
    let agentClause = "";
    if (options.agentId && options.agentId.trim()) {
      args.push(options.agentId.trim());
      agentClause = ` AND (COALESCE(array_length(s.allowed_agents, 1), 0) = 0 OR $${args.length} = ANY(s.allowed_agents))`;
    }

    const { rows } = await this.pool.query(
      `SELECT s.id, s.source_id, src.display_name AS source_name, s.original_tool_name, s.canonical_name,
              s.description, s.tags, s.metadata_quality, s.risk_level, s.execution_type,
              s.requires_session, s.skill_state, s.allowed_agents, s.avg_latency_ms, s.success_rate,
              s.discovered_at, s.updated_at
       FROM canonical_skills s
       INNER JOIN mcp_skill_sources src ON src.id = s.source_id
       WHERE src.enabled = TRUE
         AND s.skill_state = 'released'
         ${agentClause}
       ORDER BY s.embedding <=> $1::vector ASC
       LIMIT $2`,
      args,
    );
    return rows.map((row) => mapSkillRow(row as PoolRow));
  }

  async syncAllSources(): Promise<{ synced: number; failed: number }> {
    await this.ensureLocalSource();
    await this.syncLocalSkills();

    const sources = await this.listSources(false);
    let synced = 0;
    let failed = 0;
    for (const source of sources) {
      if (!source.enabled) continue;
      try {
        await this.syncSource(source.id);
        synced += 1;
      } catch (err) {
        failed += 1;
        await this.markSyncStatus(source.id, "error", stringErr(err));
      }
    }
    return { synced, failed };
  }

  async syncSource(sourceID: string): Promise<{ sourceId: string; upserted: number }> {
    const sourceId = normalizeSourceId(sourceID);
    if (!sourceId) throw new Error("source id is required");
    if (sourceId === "local") {
      const upserted = await this.syncLocalSkills();
      return { sourceId, upserted };
    }
    const source = await this.getSourceByID(sourceId);
    if (!source) throw new Error(`source ${sourceId} not found`);
    if (!source.enabled) return { sourceId, upserted: 0 };

    const tools = await listRemoteTools(source.endpoint);
    let upserted = 0;
    for (const tool of tools) {
      const normalized = normalizeToolRecord(source, tool);
      const defaultState: SkillState = source.trust === "trusted" ? "released" : "quarantined";
      await this.upsertCanonicalSkill(normalized, defaultState);
      upserted += 1;
    }
    await this.markSyncStatus(sourceId, "ok", "");
    return { sourceId, upserted };
  }

  private async syncLocalSkills(): Promise<number> {
    const localSource = await this.getSourceByID("local");
    if (!localSource || !localSource.enabled) return 0;
    const local = listSkills();
    let upserted = 0;
    for (const skill of local) {
      const canonicalName = `local.${skill.id}`;
      const tags = uniqueStrings(skill.tags || []);
      const embeddingText = [
        canonicalName,
        skill.name,
        skill.description,
        ...tags,
      ].join("\n");
      const normalized: NormalizedTool = {
        sourceId: "local",
        sourceName: localSource.displayName,
        originalToolName: skill.id,
        canonicalName,
        description: skill.description || `Local skill ${skill.id}`,
        tags: tags.length > 0 ? tags : ["local", "skill"],
        metadataQuality: 1,
        riskLevel: "low",
        executionType: "stateless",
        requiresSession: false,
        embeddingText,
      };
      await this.upsertCanonicalSkill(normalized, "released");
      upserted += 1;
    }
    await this.markSyncStatus("local", "ok", "");
    return upserted;
  }

  private async upsertCanonicalSkill(record: NormalizedTool, defaultState: SkillState): Promise<void> {
    const embedding = await this.embedText(record.embeddingText);
    const embeddingLiteral = vectorLiteral(embedding);
    const skillID = normalizeSkillID(`${record.sourceId}:${record.originalToolName}`);
    const now = new Date().toISOString();

    await this.pool.query(
      `INSERT INTO canonical_skills (
         id, source_id, source_name, original_tool_name, canonical_name, description, tags,
         metadata_quality, risk_level, execution_type, requires_session, embedding_text,
         embedding, skill_state, allowed_agents, avg_latency_ms, success_rate, discovered_at, updated_at
       )
       VALUES (
         $1, $2, $3, $4, $5, $6, $7,
         $8, $9, $10, $11, $12,
         $13::vector, $14, $15, $16, $17, $18::timestamptz, $19::timestamptz
       )
       ON CONFLICT (id)
       DO UPDATE SET
         source_name = EXCLUDED.source_name,
         original_tool_name = EXCLUDED.original_tool_name,
         canonical_name = EXCLUDED.canonical_name,
         description = EXCLUDED.description,
         tags = EXCLUDED.tags,
         metadata_quality = EXCLUDED.metadata_quality,
         risk_level = EXCLUDED.risk_level,
         execution_type = EXCLUDED.execution_type,
         requires_session = EXCLUDED.requires_session,
         embedding_text = EXCLUDED.embedding_text,
         embedding = EXCLUDED.embedding,
         updated_at = EXCLUDED.updated_at,
         skill_state = CASE
           WHEN canonical_skills.skill_state = 'disabled' THEN canonical_skills.skill_state
           WHEN canonical_skills.skill_state = 'released' THEN canonical_skills.skill_state
           ELSE EXCLUDED.skill_state
         END`,
      [
        skillID,
        record.sourceId,
        record.sourceName,
        record.originalToolName,
        record.canonicalName,
        record.description,
        record.tags,
        record.metadataQuality,
        record.riskLevel,
        record.executionType,
        record.requiresSession,
        record.embeddingText,
        embeddingLiteral,
        defaultState,
        [],
        0,
        1,
        now,
        now,
      ],
    );
  }

  private async ensureSchema(): Promise<void> {
    await this.pool.query(`CREATE EXTENSION IF NOT EXISTS vector`);
    await this.pool.query(
      `CREATE TABLE IF NOT EXISTS mcp_skill_sources (
         id TEXT PRIMARY KEY,
         display_name TEXT NOT NULL,
         endpoint TEXT NOT NULL,
         trust TEXT NOT NULL DEFAULT 'untrusted',
         enabled BOOLEAN NOT NULL DEFAULT TRUE,
         source_type TEXT NOT NULL DEFAULT 'remote',
         last_sync_at TIMESTAMPTZ,
         last_sync_status TEXT,
         last_sync_error TEXT,
         created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
         updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
       )`,
    );
    await this.pool.query(
      `CREATE TABLE IF NOT EXISTS canonical_skills (
         id TEXT PRIMARY KEY,
         source_id TEXT NOT NULL REFERENCES mcp_skill_sources(id) ON DELETE CASCADE,
         source_name TEXT NOT NULL,
         original_tool_name TEXT NOT NULL,
         canonical_name TEXT NOT NULL,
         description TEXT NOT NULL,
         tags TEXT[] NOT NULL DEFAULT '{}',
         metadata_quality DOUBLE PRECISION NOT NULL DEFAULT 0,
         risk_level TEXT NOT NULL DEFAULT 'medium',
         execution_type TEXT NOT NULL DEFAULT 'stateless',
         requires_session BOOLEAN NOT NULL DEFAULT FALSE,
         embedding_text TEXT NOT NULL,
         embedding vector(${this.embeddingDim}) NOT NULL,
         skill_state TEXT NOT NULL DEFAULT 'quarantined',
         allowed_agents TEXT[] NOT NULL DEFAULT '{}',
         avg_latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
         success_rate DOUBLE PRECISION NOT NULL DEFAULT 1,
         discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
         updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
       )`,
    );
    await this.pool.query(
      `CREATE INDEX IF NOT EXISTS idx_canonical_skills_source_state
       ON canonical_skills(source_id, skill_state)`,
    );
    await this.pool.query(
      `CREATE INDEX IF NOT EXISTS idx_canonical_skills_embedding
       ON canonical_skills
       USING hnsw (embedding vector_cosine_ops)`,
    );
  }

  private async ensureLocalSource(): Promise<void> {
    await this.upsertSource({
      id: "local",
      displayName: "Local Skills",
      endpoint: "local://skills",
      trust: "trusted",
      enabled: true,
      sourceType: "local",
    });
  }

  private async seedSourcesFromEnv(): Promise<void> {
    const raw = (process.env["MCP_SKILL_SOURCES_JSON"] || "").trim();
    if (!raw) return;
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (!Array.isArray(parsed)) return;
      for (const item of parsed) {
        if (!item || typeof item !== "object") continue;
        const source = item as Record<string, unknown>;
        const id = normalizeSourceId(stringValue(source["id"]));
        const endpoint = stringValue(source["endpoint"]);
        if (!id || !endpoint) continue;
        await this.upsertSource({
          id,
          displayName: stringValue(source["displayName"]) || id,
          endpoint,
          trust: normalizeTrust(stringValue(source["trust"])),
          enabled: boolValue(source["enabled"], true),
          sourceType: "remote",
        });
      }
    } catch (err) {
      process.stderr.write(`[skill-discovery] failed parsing MCP_SKILL_SOURCES_JSON: ${stringErr(err)}\n`);
    }
  }

  private async markSyncStatus(sourceID: string, status: "ok" | "error", errMsg: string): Promise<void> {
    await this.pool.query(
      `UPDATE mcp_skill_sources
       SET last_sync_at = NOW(),
           last_sync_status = $2,
           last_sync_error = $3,
           updated_at = NOW()
       WHERE id = $1`,
      [sourceID, status, errMsg || ""],
    );
  }

  private async getSourceByID(id: string): Promise<SkillSourceRecord | null> {
    const { rows } = await this.pool.query(
      `SELECT id, display_name, endpoint, trust, enabled, source_type, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at
       FROM mcp_skill_sources WHERE id = $1 LIMIT 1`,
      [id],
    );
    if (!rows[0]) return null;
    return mapSourceRow(rows[0] as PoolRow);
  }

  private async getSkillByID(id: string): Promise<CanonicalSkillRecord | null> {
    const { rows } = await this.pool.query(
      `SELECT id, source_id, source_name, original_tool_name, canonical_name, description, tags,
              metadata_quality, risk_level, execution_type, requires_session, skill_state, allowed_agents,
              avg_latency_ms, success_rate, discovered_at, updated_at
       FROM canonical_skills
       WHERE id = $1
       LIMIT 1`,
      [id],
    );
    if (!rows[0]) return null;
    return mapSkillRow(rows[0] as PoolRow);
  }

  private async embedText(text: string): Promise<number[]> {
    const trimmed = text.trim().slice(0, 6000);
    if (this.openAiEmbeddingEnabled) {
      try {
        const vector = await this.embedViaOpenAi(trimmed);
        if (vector.length === this.embeddingDim) return vector;
        return resizeVector(vector, this.embeddingDim);
      } catch (err) {
        process.stderr.write(`[skill-discovery] external embeddings failed, using local fallback: ${stringErr(err)}\n`);
      }
    }
    return deterministicEmbedding(trimmed, this.embeddingDim);
  }

  private async embedViaOpenAi(text: string): Promise<number[]> {
    const body = {
      model: this.openAiEmbeddingModel,
      input: text,
      encoding_format: "float",
    };
    const resp = await fetch(`${this.openAiBaseUrl}/v1/embeddings`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${this.openAiApiKey}`,
      },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      throw new Error(`embeddings http ${resp.status}`);
    }
    const payload = (await resp.json()) as {
      data?: Array<{ embedding?: number[] }>;
    };
    const vector = payload.data?.[0]?.embedding;
    if (!Array.isArray(vector) || vector.length === 0) {
      throw new Error("embedding payload missing vector");
    }
    return vector.map((v) => Number(v));
  }
}

function normalizeToolRecord(source: SkillSourceRecord, tool: RemoteToolShape): NormalizedTool {
  const originalToolName = (tool.name || "").trim();
  const description = (tool.description || "").trim() || `Tool ${originalToolName}`;
  const metaTags = uniqueStrings([
    ...arrayStrings(tool.tags),
    ...arrayStrings(tool.categories),
    ...arrayStrings(tool.metadata?.["tags"]),
    ...arrayStrings(tool.metadata?.["categories"]),
  ]);
  const inferredTags = inferTags(originalToolName, description, metaTags);
  const canonicalName = canonicalToolName(source.id, originalToolName);
  const metadataQuality = scoreMetadataQuality(tool, description, inferredTags);
  const riskLevel = inferRiskLevel(originalToolName, description);
  const executionType = inferExecutionType(originalToolName, description);
  const requiresSession = inferRequiresSession(originalToolName, description, inferredTags, executionType);
  const embeddingText = [
    canonicalName,
    `source:${source.displayName}`,
    `tool:${originalToolName}`,
    `description:${description}`,
    `risk:${riskLevel}`,
    `execution:${executionType}`,
    `session:${requiresSession}`,
    `tags:${inferredTags.join(", ")}`,
    "Upstream metadata is hint-level and normalized for orchestration routing.",
  ].join("\n");

  return {
    sourceId: source.id,
    sourceName: source.displayName,
    originalToolName,
    canonicalName,
    description,
    tags: inferredTags,
    metadataQuality,
    riskLevel,
    executionType,
    requiresSession,
    embeddingText,
  };
}

async function listRemoteTools(endpoint: string): Promise<RemoteToolShape[]> {
  const payload = {
    jsonrpc: "2.0",
    id: 1,
    method: "tools/list",
    params: {},
  };
  const response = await fetch(endpoint.replace(/\/+$/, "") + "/mcp", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "application/json, text/event-stream",
    },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(`tools/list http ${response.status}`);
  }
  const raw = (await response.json()) as {
    result?: { tools?: RemoteToolShape[] };
    error?: { message?: string };
  };
  if (raw.error?.message) {
    throw new Error(raw.error.message);
  }
  const tools = raw.result?.tools;
  if (!Array.isArray(tools)) return [];
  return tools
    .filter((tool) => typeof tool?.name === "string" && tool.name.trim() !== "")
    .map((tool) => ({
      name: tool.name,
      description: tool.description,
      inputSchema: tool.inputSchema,
      tags: tool.tags,
      categories: tool.categories,
      metadata: tool.metadata,
      annotations: tool.annotations,
      examples: tool.examples,
    }));
}

function inferTags(name: string, description: string, existing: string[]): string[] {
  const tags = new Set(existing.map((t) => t.toLowerCase()));
  const text = `${name} ${description}`.toLowerCase();
  const addIf = (tag: string, needles: string[]) => {
    if (needles.some((needle) => text.includes(needle))) tags.add(tag);
  };
  addIf("browser", ["browser", "dom", "page", "click", "navigate"]);
  addIf("filesystem", ["file", "folder", "path", "directory", "read", "write"]);
  addIf("database", ["database", "sql", "query", "postgres", "table", "schema"]);
  addIf("web", ["http", "request", "url", "web", "search", "crawl"]);
  addIf("runtime", ["runtime", "inventory", "config", "environment"]);
  addIf("orchestration", ["agent", "orchestr", "delegate", "task", "workflow"]);
  addIf("office", ["xlsx", "spreadsheet", "docx", "document", "ppt", "slide"]);
  if (tags.size === 0) tags.add("general");
  return [...tags].sort();
}

function inferRiskLevel(name: string, description: string): "low" | "medium" | "high" {
  const text = `${name} ${description}`.toLowerCase();
  if (containsAny(text, ["delete", "drop", "remove", "truncate", "reset", "kill", "terminate"])) return "high";
  if (containsAny(text, ["write", "update", "create", "insert", "modify", "execute", "run"])) return "medium";
  return "low";
}

function inferExecutionType(name: string, description: string): "stateless" | "stateful" {
  const text = `${name} ${description}`.toLowerCase();
  if (containsAny(text, ["session", "state", "browser", "stream", "subscribe", "interactive", "socket"])) {
    return "stateful";
  }
  return "stateless";
}

function inferRequiresSession(
  name: string,
  description: string,
  tags: string[],
  executionType: "stateless" | "stateful",
): boolean {
  if (executionType === "stateful") return true;
  const text = `${name} ${description}`.toLowerCase();
  return tags.includes("browser") || containsAny(text, ["session", "auth", "login", "cookie"]);
}

function scoreMetadataQuality(tool: RemoteToolShape, description: string, tags: string[]): number {
  let score = 0.2;
  if (description.length >= 24) score += 0.25;
  if (tool.inputSchema && typeof tool.inputSchema === "object") score += 0.2;
  if (tags.length > 0) score += 0.15;
  if (arrayStrings(tool.examples).length > 0) score += 0.1;
  if (tool.metadata && Object.keys(tool.metadata).length > 0) score += 0.1;
  return Math.max(0, Math.min(1, score));
}

function mapSourceRow(row: PoolRow): SkillSourceRecord {
  return {
    id: String(row["id"] || ""),
    displayName: String(row["display_name"] || row["displayName"] || ""),
    endpoint: String(row["endpoint"] || ""),
    trust: normalizeTrust(String(row["trust"] || "untrusted")),
    enabled: Boolean(row["enabled"]),
    sourceType: normalizeSourceType(String(row["source_type"] || "remote")),
    lastSyncAt: toIso(row["last_sync_at"]),
    lastSyncStatus: stringOrEmpty(row["last_sync_status"]),
    lastSyncError: stringOrEmpty(row["last_sync_error"]),
    createdAt: toIso(row["created_at"]) || new Date().toISOString(),
    updatedAt: toIso(row["updated_at"]) || new Date().toISOString(),
  };
}

function mapSkillRow(row: PoolRow): CanonicalSkillRecord {
  return {
    id: String(row["id"] || ""),
    sourceId: String(row["source_id"] || ""),
    sourceName: String(row["source_name"] || ""),
    originalToolName: String(row["original_tool_name"] || ""),
    canonicalName: String(row["canonical_name"] || ""),
    description: String(row["description"] || ""),
    tags: arrayStrings(row["tags"]),
    metadataQuality: numValue(row["metadata_quality"], 0),
    riskLevel: normalizeRisk(String(row["risk_level"] || "medium")),
    executionType: normalizeExecutionType(String(row["execution_type"] || "stateless")),
    requiresSession: Boolean(row["requires_session"]),
    skillState: normalizeSkillState(String(row["skill_state"] || "quarantined")),
    allowedAgents: arrayStrings(row["allowed_agents"]),
    avgLatencyMs: numValue(row["avg_latency_ms"], 0),
    successRate: numValue(row["success_rate"], 1),
    discoveredAt: toIso(row["discovered_at"]) || new Date().toISOString(),
    updatedAt: toIso(row["updated_at"]) || new Date().toISOString(),
  };
}

function normalizeSourceId(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9._:-]+/g, "-");
}

function normalizeSkillID(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9._:-]+/g, "-");
}

function canonicalToolName(sourceId: string, name: string): string {
  const suffix = name.trim().toLowerCase().replace(/[^a-z0-9._:-]+/g, ".");
  return `${sourceId}.${suffix}`;
}

function normalizeTrust(value: string): SourceTrust {
  return value === "trusted" ? "trusted" : "untrusted";
}

function normalizeSourceType(value: string): SourceType {
  return value === "local" ? "local" : "remote";
}

function normalizeSkillState(value: string): SkillState {
  if (value === "released") return "released";
  if (value === "disabled") return "disabled";
  return "quarantined";
}

function normalizeRisk(value: string): "low" | "medium" | "high" {
  if (value === "low" || value === "high") return value;
  return "medium";
}

function normalizeExecutionType(value: string): "stateless" | "stateful" {
  return value === "stateful" ? "stateful" : "stateless";
}

function deterministicEmbedding(text: string, dim: number): number[] {
  const vec = new Array<number>(dim).fill(0);
  const normalized = text.toLowerCase();
  if (!normalized) return vec;

  const tokens = normalized.split(/\s+/).filter(Boolean);
  for (const token of tokens) {
    const digest = crypto.createHash("sha256").update(token).digest();
    for (let i = 0; i < dim; i += 1) {
      const b = digest[i % digest.length] ?? 0;
      const centered = (b - 127.5) / 127.5;
      vec[i] += centered;
    }
  }

  const norm = Math.sqrt(vec.reduce((sum, value) => sum + value * value, 0));
  if (!Number.isFinite(norm) || norm <= 0) {
    return vec;
  }
  return vec.map((value) => value / norm);
}

function resizeVector(values: number[], dim: number): number[] {
  if (values.length === dim) return values.map((v) => Number(v));
  if (values.length > dim) return values.slice(0, dim).map((v) => Number(v));
  const out = values.map((v) => Number(v));
  while (out.length < dim) out.push(0);
  return out;
}

function vectorLiteral(values: number[]): string {
  return `[${values.map((v) => Number(v).toFixed(6)).join(",")}]`;
}

function containsAny(text: string, needles: string[]): boolean {
  return needles.some((needle) => text.includes(needle));
}

function uniqueStrings(values: unknown[]): string[] {
  const out = new Set<string>();
  for (const value of values) {
    const str = String(value || "").trim();
    if (!str) continue;
    out.add(str);
  }
  return [...out].sort();
}

function arrayStrings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return uniqueStrings(value);
}

function stringOrEmpty(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function boolValue(value: unknown, def: boolean): boolean {
  if (typeof value === "boolean") return value;
  return def;
}

function numValue(value: unknown, def: number): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : def;
}

function parseBool(value: string | undefined): boolean | undefined {
  const v = (value || "").trim().toLowerCase();
  if (!v) return undefined;
  if (["1", "true", "yes", "on"].includes(v)) return true;
  if (["0", "false", "no", "off"].includes(v)) return false;
  return undefined;
}

function parseIntSafe(value: string | undefined, def: number, min?: number, max?: number): number {
  const n = Number.parseInt((value || "").trim(), 10);
  if (!Number.isFinite(n)) return def;
  return clampInt(n, min ?? Number.MIN_SAFE_INTEGER, max ?? Number.MAX_SAFE_INTEGER);
}

function clampInt(n: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, Math.floor(n)));
}

function toIso(value: unknown): string {
  if (!value) return "";
  if (typeof value === "string") return value;
  if (value instanceof Date) return value.toISOString();
  return "";
}

function stringErr(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

function nextDailyLocalTwoAM(now: Date): Date {
  const next = new Date(now);
  next.setHours(2, 0, 0, 0);
  if (next.getTime() <= now.getTime()) {
    next.setDate(next.getDate() + 1);
  }
  return next;
}

let singleton: SkillDiscoveryRuntime | null = null;

export async function getSkillDiscoveryRuntime(): Promise<SkillDiscoveryRuntime> {
  if (singleton) return singleton;
  singleton = new SkillDiscoveryRuntime();
  await singleton.init();
  return singleton;
}

