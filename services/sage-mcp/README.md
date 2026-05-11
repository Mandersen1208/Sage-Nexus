# Sage MCP

Standalone MCP server for Sage Nexus runtime tools.

It exposes runtime inventory, Agent Work Context, document/artifact tools, web/search helpers, and skill access to manager-owned workers. It does not require OpenClaw at runtime.

## Running

```bash
npm install
npm start
```

HTTP mode for Docker/local manager integration:

```bash
TRANSPORT=http PORT=3030 npm start
```

Health and discovery/governance HTTP routes:

```text
GET /health
POST /mcp
GET /skills/discovery/servers
POST /skills/discovery/servers
PATCH /skills/discovery/servers/{id}
POST /skills/discovery/servers/{id}/release
POST /skills/discovery/servers/{id}/sync
POST /skills/discovery/sync
GET /skills/discovery/skills
PATCH /skills/discovery/skills/{id}
GET /skills/discovery/search
```

## Skill Discovery Behavior

- Canonical registry tables are maintained in Postgres/pgvector:
  - `mcp_skill_sources`
  - `canonical_skills`
- Local skills are auto-registered as trusted source `local://skills`.
- External source defaults:
  - `trusted` -> discovered skills default `released`
  - `untrusted` -> discovered skills default `quarantined`
- Retrieval/search returns only released skills from enabled sources, with optional agent allowlist filtering.
- Scheduler runs `syncAllSources()` once per day at local `2:00 AM`.
- Manager-side manual sync and release operations are supported through manager proxy endpoints.

## Runtime Env

- `MANAGER_URL`, default `http://manager:8090`
- `SAGE_SOUL_PATH`, default `/home/node/.openclaw/workspace/SOUL.md`
- `SAGE_AGENT_REGISTRY_FILE`, default `/sage-state/workspace/sage/agents.registry.json`
- `SAGE_ARTIFACTS_DIR`, default `/sage-artifacts`
- `RUNTIME_REPO_ROOT`, default current repo root when running from `services/sage-mcp`
- `RUNTIME_CONFIG_FILE`, default `/sage-state/sage.json`
- `RUNTIME_INVENTORY_FILE`, default `/tmp/sage-runtime-inventory.json`
- `SKILL_DB_URL` or (`SKILL_DB_HOST`, `SKILL_DB_PORT`, `SKILL_DB_NAME`, `SKILL_DB_USER`, `SKILL_DB_PASSWORD`) for pgvector skill index storage
- `SKILL_EMBEDDING_DIM`, default `64`
- `SKILL_RETRIEVAL_TOP_K`, default `5`
- `SKILL_EMBEDDING_MODEL`, default `text-embedding-3-small`
- `OPENAI_API_KEY` + optional `OPENAI_BASE_URL` enables provider embeddings; local deterministic embeddings are used as fallback
- `MCP_SKILL_SOURCES_JSON` optional bootstrap list for remote source registration

## Validation

```bash
npm test
npm run build
```
