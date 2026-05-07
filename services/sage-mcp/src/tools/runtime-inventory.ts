import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import fs from "node:fs";
import path from "node:path";
import { z } from "zod";
import { ChannelControl, ChannelEvents, ChannelTasks } from "../a2a/types.js";
import { RedisClient } from "../redis-client.js";

type EntityType =
  | "service"
  | "route"
  | "agent"
  | "prompt"
  | "mcp_tool"
  | "config"
  | "mount"
  | "workspace_file"
  | "architecture_doc"
  | "redis_channel"
  | "finding";

export interface RuntimeEntity {
  id: string;
  type: EntityType;
  name: string;
  source: string;
  metadata?: Record<string, unknown>;
}

export interface RuntimeRelationship {
  from: string;
  to: string;
  kind: "serves" | "calls" | "reads" | "mounts" | "publishes" | "subscribes" | "configures" | "falls_back_to";
  metadata?: Record<string, unknown>;
}

export interface RuntimeInventorySnapshot {
  generatedAt: string;
  repoRoot: string;
  configFile: string;
  entities: RuntimeEntity[];
  relationships: RuntimeRelationship[];
  findings: RuntimeEntity[];
}

interface RuntimeEvent {
  channel: string;
  observedAt: string;
  taskId?: string;
  contextId?: string;
  kind?: string;
  state?: string;
  activity?: string;
  agent?: string;
  tool?: string;
  decision?: string;
  summary?: string;
}

const eventBuffer: RuntimeEvent[] = [];
const maxEvents = 500;
let observerStarted = false;

const secretPattern = /(token|secret|password|cookie|key|authorization|api[_-]?key|ct_file|key_file)/i;

function redactValue(key: string, value: unknown): unknown {
  if (secretPattern.test(key)) {
    return "[REDACTED]";
  }
  if (Array.isArray(value)) {
    return value.map((item) => redactValue(key, item));
  }
  if (value && typeof value === "object") {
    return redactObject(value as Record<string, unknown>);
  }
  return value;
}

export function redactObject(input: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    out[key] = redactValue(key, value);
  }
  return out;
}

function readText(filePath: string): string {
  try {
    return fs.readFileSync(filePath, "utf-8");
  } catch {
    return "";
  }
}

function readJson(filePath: string): Record<string, unknown> | undefined {
  const raw = readText(filePath);
  if (!raw.trim()) return undefined;
  try {
    return JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return undefined;
  }
}

function defaultRepoRoot(): string {
  const cwd = process.cwd();
  if (path.basename(cwd) === "sage-mcp" && path.basename(path.dirname(cwd)) === "services") {
    return path.dirname(path.dirname(cwd));
  }
  return cwd;
}

function runtimePaths() {
  const repoRoot = process.env["RUNTIME_REPO_ROOT"] ?? defaultRepoRoot();
  const configFile = process.env["RUNTIME_CONFIG_FILE"] ?? "/sage-state/sage.json";
  const inventoryFile = process.env["RUNTIME_INVENTORY_FILE"] ?? "/tmp/sage-runtime-inventory.json";
  const soulPath = process.env["SAGE_SOUL_PATH"] ?? "/home/node/.openclaw/workspace/SOUL.md";
  const agentRegistryFile = process.env["SAGE_AGENT_REGISTRY_FILE"] ?? "/sage-state/workspace/sage/agents.registry.json";
  const workspaceDir = path.dirname(soulPath);
  return { repoRoot, configFile, inventoryFile, soulPath, workspaceDir, agentRegistryFile };
}

function addEntity(target: RuntimeEntity[], entity: RuntimeEntity): void {
  if (!target.some((existing) => existing.id === entity.id)) {
    target.push(entity);
  }
}

function parseServicesFromCompose(filePath: string): RuntimeEntity[] {
  const raw = readText(filePath);
  if (!raw.trim()) return [];
  const lines = raw.split(/\r?\n/);
  const services: RuntimeEntity[] = [];
  let inServices = false;
  let currentName = "";
  let current: string[] = [];

  const flush = () => {
    if (!currentName) return;
    const block = current.join("\n");
    services.push({
      id: `service:${currentName}`,
      type: "service",
      name: currentName,
      source: filePath,
      metadata: {
        image: firstMatch(block, /^\s+image:\s*(.+)$/m),
        build: firstMatch(block, /^\s+build:\s*\n\s+context:\s*(.+)$/m) ?? firstMatch(block, /^\s+context:\s*(.+)$/m),
        ports: listItemsAfterKey(block, "ports"),
        volumes: listItemsAfterKey(block, "volumes"),
        environment: parseEnvironmentBlock(block),
        dependsOn: listItemsAfterKey(block, "depends_on"),
      },
    });
  };

  for (const line of lines) {
    if (/^services:\s*$/.test(line)) {
      inServices = true;
      continue;
    }
    if (!inServices) continue;
    if (/^\S/.test(line) && !/^services:\s*$/.test(line)) {
      flush();
      break;
    }
    const serviceMatch = /^  ([A-Za-z0-9_.-]+):\s*$/.exec(line);
    if (serviceMatch) {
      flush();
      currentName = serviceMatch[1] ?? "";
      current = [];
      continue;
    }
    if (currentName) current.push(line);
  }
  flush();
  return services;
}

function firstMatch(text: string, pattern: RegExp): string | undefined {
  const match = pattern.exec(text);
  return match?.[1]?.trim().replace(/^["']|["']$/g, "");
}

function listItemsAfterKey(block: string, key: string): string[] {
  const lines = block.split(/\r?\n/);
  const result: string[] = [];
  let collecting = false;
  let baseIndent = 0;
  for (const line of lines) {
    const keyMatch = new RegExp(`^(\\s+)${key}:\\s*$`).exec(line);
    if (keyMatch) {
      collecting = true;
      baseIndent = keyMatch[1]?.length ?? 0;
      continue;
    }
    if (!collecting) continue;
    const indent = line.match(/^\s*/)?.[0].length ?? 0;
    if (line.trim() && indent <= baseIndent) break;
    const item = /^\s+-\s+(.+)$/.exec(line)?.[1]?.trim();
    if (item) result.push(item.replace(/^["']|["']$/g, ""));
  }
  return result;
}

function parseEnvironmentBlock(block: string): Record<string, unknown> {
  const lines = block.split(/\r?\n/);
  const env: Record<string, unknown> = {};
  let collecting = false;
  let baseIndent = 0;
  for (const line of lines) {
    const keyMatch = /^(\s+)environment:\s*$/.exec(line);
    if (keyMatch) {
      collecting = true;
      baseIndent = keyMatch[1]?.length ?? 0;
      continue;
    }
    if (!collecting) continue;
    const indent = line.match(/^\s*/)?.[0].length ?? 0;
    if (line.trim() && indent <= baseIndent) break;
    const pair = /^\s+([A-Za-z_][A-Za-z0-9_]*):\s*(.*)$/.exec(line);
    if (pair) {
      const key = pair[1] ?? "";
      const value = (pair[2] ?? "").trim().replace(/^["']|["']$/g, "");
      env[key] = redactValue(key, value);
    }
  }
  return env;
}

function scanCompose(repoRoot: string): RuntimeEntity[] {
  return ["docker-compose.yml", "docker-compose.override.yml"].flatMap((name) =>
    parseServicesFromCompose(path.join(repoRoot, name)),
  );
}

function scanManagerRoutes(repoRoot: string): RuntimeEntity[] {
  const filePath = path.join(repoRoot, "services", "manager", "cmd", "manager", "main.go");
  const raw = readText(filePath);
  const routes = [...raw.matchAll(/HandleFunc\("([^"]+)"/g)].map((match) => match[1] ?? "");
  return routes.filter(Boolean).map((route) => ({
    id: `route:manager:${route}`,
    type: "route" as const,
    name: route,
    source: filePath,
    metadata: { service: "manager" },
  }));
}

interface RegistryAgentConfig {
  id?: string;
  enabled?: boolean;
  model?: string;
  systemPromptFile?: string;
  routable?: boolean;
  routeToolName?: string;
  routeDescription?: string;
  toolBundles?: string[];
  tools?: string[];
  peerTargets?: string[];
  maxPeerDepth?: number;
  seniorGate?: string;
  authority?: string;
  mustNotOwn?: string[];
}

interface LoadedRegistryAgent {
  config: RegistryAgentConfig;
  source: string;
}

function scanSageAgents(repoRoot: string, workspaceRegistryFile: string): { entities: RuntimeEntity[]; relationships: RuntimeRelationship[] } {
  const bundledConfigPath = path.join(repoRoot, "services", "manager", "config", "agents.json");
  const candidates = [bundledConfigPath];
  if (workspaceRegistryFile && workspaceRegistryFile !== bundledConfigPath && fs.existsSync(workspaceRegistryFile)) {
    candidates.push(workspaceRegistryFile);
  }
  const entities: RuntimeEntity[] = [];
  const relationships: RuntimeRelationship[] = [];
  const agents: Record<string, LoadedRegistryAgent> = {};
  const toolBundles: Record<string, string[]> = {};
  const loadedSources: string[] = [];

  candidates.forEach((configPath, index) => {
    const cfg = readJson(configPath);
    if (!cfg) return;
    loadedSources.push(configPath);
    addEntity(entities, {
      id: `config:agent-registry:${index}`,
      type: "config",
      name: index === 0 ? "bundled agent registry" : "workspace agent registry",
      source: configPath,
      metadata: redactObject({
        version: cfg["version"],
        sourceOrder: index,
        toolBundles: Object.keys((cfg["toolBundles"] as Record<string, unknown> | undefined) ?? {}).sort(),
        agentCount: Object.keys((cfg["agents"] as Record<string, unknown> | undefined) ?? {}).length,
      }),
    });
    const cfgBundles = cfg["toolBundles"] && typeof cfg["toolBundles"] === "object"
      ? (cfg["toolBundles"] as Record<string, string[]>)
      : {};
    for (const [bundleName, bundleTools] of Object.entries(cfgBundles)) {
      toolBundles[bundleName] = Array.isArray(bundleTools) ? bundleTools : [];
    }
    const cfgAgents = cfg["agents"] && typeof cfg["agents"] === "object"
      ? (cfg["agents"] as Record<string, RegistryAgentConfig>)
      : {};
    for (const [key, agentCfg] of Object.entries(cfgAgents)) {
      const agentId = (agentCfg.id ?? key).trim();
      if (!agentId) continue;
      agents[agentId] = { config: { ...agentCfg, id: agentId }, source: configPath };
    }
  });

  addEntity(entities, {
    id: "config:agent-registry:active",
    type: "config",
    name: "active agent registry",
    source: loadedSources.join("; "),
    metadata: redactObject({
      sources: loadedSources,
      toolBundles: Object.keys(toolBundles).sort(),
      agentCount: Object.keys(agents).length,
      workspaceRegistryFile,
      workspaceRegistryLoaded: loadedSources.includes(workspaceRegistryFile),
    }),
  });

  for (const [agentId, loaded] of Object.entries(agents)) {
    const agentCfg = loaded.config;
    const enabled = agentCfg.enabled !== false;
    const toolNames = resolveRegistryTools(agentCfg, toolBundles);
    addEntity(entities, {
      id: `agent:${agentId}`,
      type: "agent",
      name: agentId,
      source: loaded.source,
      metadata: redactObject({
        model: agentCfg.model,
        systemPromptFile: agentCfg.systemPromptFile,
        enabled,
        routable: agentCfg.routable ?? false,
        routeToolName: agentCfg.routeToolName,
        routeDescription: agentCfg.routeDescription,
        toolBundles: agentCfg.toolBundles ?? [],
        tools: toolNames,
        peerTargets: agentCfg.peerTargets ?? [],
        maxPeerDepth: agentCfg.maxPeerDepth,
        seniorGate: agentCfg.seniorGate ?? "off",
        authority: agentCfg.authority,
        mustNotOwn: agentCfg.mustNotOwn ?? [],
      }),
    });
    relationships.push({ from: "config:agent-registry:active", to: `agent:${agentId}`, kind: "configures" });
    if (agentCfg.systemPromptFile) {
      const promptPath = path.normalize(path.join(path.dirname(loaded.source), agentCfg.systemPromptFile));
      addEntity(entities, {
        id: `prompt:${agentId}`,
        type: "prompt",
        name: path.basename(promptPath),
        source: promptPath,
        metadata: { agent: agentId, exists: fs.existsSync(promptPath) },
      });
      relationships.push({ from: `agent:${agentId}`, to: `prompt:${agentId}`, kind: "reads" });
    }
    if (enabled && agentCfg.routable && agentCfg.routeToolName) {
      const routeId = `route:orchestrator:${agentCfg.routeToolName}`;
      addEntity(entities, {
        id: routeId,
        type: "route",
        name: agentCfg.routeToolName,
        source: loaded.source,
        metadata: {
          service: "manager",
          targetAgent: agentId,
          description: agentCfg.routeDescription,
        },
      });
      relationships.push(
        { from: "agent:AGT-sage-orchestrator", to: routeId, kind: "serves" },
        { from: routeId, to: `agent:${agentId}`, kind: "calls" },
      );
    }
    for (const toolName of toolNames) {
      relationships.push({ from: `agent:${agentId}`, to: `mcp_tool:${toolName}`, kind: "calls", metadata: { source: "agent registry tool exposure" } });
    }
    for (const target of agentCfg.peerTargets ?? []) {
      relationships.push({ from: `agent:${agentId}`, to: `agent:${target}`, kind: "calls", metadata: { source: "peer policy", maxDepth: agentCfg.maxPeerDepth } });
    }
  }
  return { entities, relationships };
}

function resolveRegistryTools(agentCfg: RegistryAgentConfig, toolBundles: Record<string, string[]>): string[] {
  const seen = new Set<string>();
  const add = (name: string | undefined) => {
    const trimmed = (name ?? "").trim();
    if (trimmed) seen.add(trimmed);
  };
  for (const bundleName of agentCfg.toolBundles ?? []) {
    for (const toolName of toolBundles[bundleName] ?? []) add(toolName);
  }
  for (const toolName of agentCfg.tools ?? []) add(toolName);
  return [...seen].sort();
}

function scanMcpTools(repoRoot: string): RuntimeEntity[] {
  const toolsDir = path.join(repoRoot, "services", "sage-mcp", "src", "tools");
  let files: string[] = [];
  try {
    files = fs.readdirSync(toolsDir).filter((name) => name.endsWith(".ts"));
  } catch {
    return [];
  }
  const entities: RuntimeEntity[] = [];
  for (const name of files) {
    const filePath = path.join(toolsDir, name);
    const raw = readText(filePath);
    const tools = [
      ...raw.matchAll(/registerTool\(\s*["']([^"']+)["']/g),
      ...raw.matchAll(/\.tool\(\s*["']([^"']+)["']/g),
    ].map((match) => match[1] ?? "");
    for (const tool of tools.filter(Boolean)) {
      addEntity(entities, {
        id: `mcp_tool:${tool}`,
        type: "mcp_tool",
        name: tool,
        source: filePath,
        metadata: { module: name },
      });
    }
  }
  return entities;
}

function scanWorkspaceFiles(workspaceDir: string): RuntimeEntity[] {
  const names = ["AGENTS.md", "SOUL.md", "TOOLS.md", "IDENTITY.md", "USER.md", "HEARTBEAT.md", "BOOTSTRAP.md", "MEMORY.md", "memory.md"];
  return names.map((name) => {
    const filePath = path.join(workspaceDir, name);
    let size: number | undefined;
    let updatedAtMs: number | undefined;
    try {
      const stat = fs.statSync(filePath);
      if (stat.isFile()) {
        size = stat.size;
        updatedAtMs = Math.floor(stat.mtimeMs);
      }
    } catch {
      // missing is recorded below
    }
    return {
      id: `workspace_file:${name}`,
      type: "workspace_file" as const,
      name,
      source: filePath,
      metadata: { exists: size !== undefined, size, updatedAtMs },
    };
  });
}

function scanArchitectureDocs(workspaceDir: string): RuntimeEntity[] {
  const projectsDir = path.join(workspaceDir, "projects");
  const entities: RuntimeEntity[] = [];

  let projects: fs.Dirent[] = [];
  try {
    projects = fs.readdirSync(projectsDir, { withFileTypes: true }).filter((entry) => entry.isDirectory());
  } catch {
    return entities;
  }

  const visit = (projectId: string, dir: string) => {
    let entries: fs.Dirent[] = [];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const filePath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        visit(projectId, filePath);
        continue;
      }
      if (!entry.isFile() || !entry.name.endsWith(".md")) continue;
      const raw = readText(filePath);
      let stat: fs.Stats | undefined;
      try {
        stat = fs.statSync(filePath);
      } catch {
        // best effort metadata only
      }
      const relativePath = path.relative(workspaceDir, filePath).replace(/\\/g, "/");
      addEntity(entities, {
        id: `architecture_doc:${relativePath}`,
        type: "architecture_doc",
        name: relativePath,
        source: filePath,
        metadata: {
          projectId,
          title: firstMatch(raw, /^#\s+(.+)$/m),
          status: firstMatch(raw, /^Status:\s*(.+)$/m),
          size: stat?.size,
          updatedAtMs: stat ? Math.floor(stat.mtimeMs) : undefined,
        },
      });
    }
  };

  for (const project of projects) {
    visit(project.name, path.join(projectsDir, project.name, "architecture"));
  }

  return entities;
}

function scanSageConfig(configFile: string): { entities: RuntimeEntity[]; relationships: RuntimeRelationship[]; soulFinding?: RuntimeEntity } {
  const cfg = readJson(configFile);
  const entities: RuntimeEntity[] = [{
    id: "config:sage",
    type: "config",
    name: "sage.json",
    source: configFile,
    metadata: cfg ? redactObject({
      agents: cfg["agents"],
      mcp: cfg["mcp"],
      gateway: cfg["gateway"],
    }) : { missing: true },
  }];
  const relationships: RuntimeRelationship[] = [];
  const servers = (cfg?.["mcp"] as { servers?: Record<string, { env?: Record<string, string> }> } | undefined)?.servers ?? {};
  for (const [serverName, server] of Object.entries(servers)) {
    addEntity(entities, {
      id: `config:mcp:${serverName}`,
      type: "config",
      name: `mcp.${serverName}`,
      source: configFile,
      metadata: redactObject(server as unknown as Record<string, unknown>),
    });
    relationships.push({ from: "config:sage", to: `config:mcp:${serverName}`, kind: "configures" });
  }

  const configuredSoul = servers["sage-knowledge"]?.env?.["SAGE_SOUL_PATH"] ?? servers["sage-knowledge"]?.env?.["SOUL_FILE"];
  const sageSoulPath = process.env["SAGE_SOUL_PATH"] ?? "/home/node/.openclaw/workspace/SOUL.md";
  let soulFinding: RuntimeEntity | undefined;
  if (configuredSoul && configuredSoul !== sageSoulPath) {
    soulFinding = {
      id: "finding:soul-path-conflict",
      type: "finding",
      name: "SOUL path conflict",
      source: configFile,
      metadata: {
        severity: "warning",
        message: "Sage MCP config soul path differs from manager SAGE_SOUL_PATH; MCP-triggered Sage sessions may read a different soul file.",
        soulFile: configuredSoul,
        sageSoulPath,
      },
    };
  }
  return { entities, relationships, soulFinding };
}

function baseRelationships(entities: RuntimeEntity[]): RuntimeRelationship[] {
  const rels: RuntimeRelationship[] = [];
  for (const route of entities.filter((e) => e.type === "route")) {
    rels.push({ from: "service:manager", to: route.id, kind: "serves" });
  }
  for (const tool of entities.filter((e) => e.type === "mcp_tool")) {
    rels.push({ from: "service:sage-mcp", to: tool.id, kind: "serves" });
  }
  rels.push(
    { from: "service:sage-mcp", to: `redis_channel:${ChannelTasks}`, kind: "publishes" },
    { from: "service:sage-mcp", to: `redis_channel:${ChannelEvents}`, kind: "subscribes" },
    { from: "service:sage-mcp", to: `redis_channel:${ChannelControl}`, kind: "publishes" },
    { from: "service:manager", to: `redis_channel:${ChannelTasks}`, kind: "subscribes" },
    { from: "service:manager", to: `redis_channel:${ChannelEvents}`, kind: "publishes" },
    { from: "service:manager", to: `redis_channel:${ChannelControl}`, kind: "subscribes" },
  );
  return rels;
}

export function buildRuntimeInventorySnapshot(): RuntimeInventorySnapshot {
  const paths = runtimePaths();
  const entities: RuntimeEntity[] = [];
  const relationships: RuntimeRelationship[] = [];

  for (const service of scanCompose(paths.repoRoot)) {
    addEntity(entities, service);
    const volumes = Array.isArray(service.metadata?.volumes) ? service.metadata.volumes as string[] : [];
    for (const volume of volumes) {
      const mountId = `mount:${service.name}:${volume}`;
      addEntity(entities, { id: mountId, type: "mount", name: volume, source: service.source });
      relationships.push({ from: service.id, to: mountId, kind: "mounts" });
    }
  }
  for (const route of scanManagerRoutes(paths.repoRoot)) addEntity(entities, route);
  const sageAgents = scanSageAgents(paths.repoRoot, paths.agentRegistryFile);
  for (const entity of sageAgents.entities) addEntity(entities, entity);
  relationships.push(...sageAgents.relationships);
  for (const tool of scanMcpTools(paths.repoRoot)) addEntity(entities, tool);
  for (const file of scanWorkspaceFiles(paths.workspaceDir)) addEntity(entities, file);
  for (const doc of scanArchitectureDocs(paths.workspaceDir)) {
    addEntity(entities, doc);
    relationships.push(
      { from: "agent:AGT-architect-agent", to: doc.id, kind: "reads" },
      { from: "agent:AGT-senior-dev-agent", to: doc.id, kind: "reads" },
      { from: "agent:AGT-runtime-librarian-agent", to: doc.id, kind: "reads" },
    );
  }
  for (const channel of [ChannelTasks, ChannelEvents, ChannelControl]) {
    addEntity(entities, { id: `redis_channel:${channel}`, type: "redis_channel", name: channel, source: "sage-a2a" });
  }
  const config = scanSageConfig(paths.configFile);
  for (const entity of config.entities) addEntity(entities, entity);
  relationships.push(...config.relationships);
  if (config.soulFinding) addEntity(entities, config.soulFinding);
  relationships.push(...baseRelationships(entities));

  const findings = entities.filter((entity) => entity.type === "finding");
  return {
    generatedAt: new Date().toISOString(),
    repoRoot: paths.repoRoot,
    configFile: paths.configFile,
    entities,
    relationships,
    findings,
  };
}

function saveSnapshot(snapshot: RuntimeInventorySnapshot): void {
  const { inventoryFile } = runtimePaths();
  fs.mkdirSync(path.dirname(inventoryFile), { recursive: true });
  fs.writeFileSync(inventoryFile, `${JSON.stringify(snapshot, null, 2)}\n`, "utf-8");
}

function loadSnapshot(): RuntimeInventorySnapshot | undefined {
  const { inventoryFile } = runtimePaths();
  return readJson(inventoryFile) as unknown as RuntimeInventorySnapshot | undefined;
}

function getSnapshot(refresh?: boolean): RuntimeInventorySnapshot {
  if (!refresh) {
    const existing = loadSnapshot();
    if (existing) return existing;
  }
  const snapshot = buildRuntimeInventorySnapshot();
  saveSnapshot(snapshot);
  return snapshot;
}

function searchSnapshot(snapshot: RuntimeInventorySnapshot, query: string, entityType?: string, limit = 25) {
  const q = query.toLowerCase();
  return snapshot.entities
    .filter((entity) => !entityType || entity.type === entityType)
    .map((entity) => ({ entity, haystack: JSON.stringify(entity).toLowerCase() }))
    .filter(({ haystack }) => haystack.includes(q))
    .slice(0, limit)
    .map(({ entity }) => entity);
}

function recordEvent(evt: RuntimeEvent): void {
  eventBuffer.push(evt);
  if (eventBuffer.length > maxEvents) {
    eventBuffer.splice(0, eventBuffer.length - maxEvents);
  }
}

function parseRedisEvent(channel: string, payload: string): RuntimeEvent {
  const observedAt = new Date().toISOString();
  try {
    const parsed = JSON.parse(payload) as Record<string, unknown>;
    const metadata = parsed["metadata"] as Record<string, unknown> | undefined;
    const status = parsed["status"] as Record<string, unknown> | undefined;
    return {
      channel,
      observedAt,
      taskId: typeof parsed["taskId"] === "string" ? parsed["taskId"] : undefined,
      contextId: typeof parsed["contextId"] === "string" ? parsed["contextId"] : undefined,
      kind: typeof parsed["kind"] === "string" ? parsed["kind"] : undefined,
      state: typeof status?.["state"] === "string" ? status["state"] : undefined,
      activity: typeof metadata?.["activity"] === "string" ? metadata["activity"] : undefined,
      agent: typeof metadata?.["agent"] === "string" ? metadata["agent"] : undefined,
      tool: typeof metadata?.["tool"] === "string" ? metadata["tool"] : undefined,
      decision: typeof parsed["decision"] === "string" ? parsed["decision"] : undefined,
      summary: summarizePayload(parsed),
    };
  } catch {
    return { channel, observedAt, summary: payload.slice(0, 240) };
  }
}

function summarizePayload(parsed: Record<string, unknown>): string {
  const parts = (parsed["parts"] as Array<{ text?: string }> | undefined)
    ?? ((parsed["status"] as { message?: { parts?: Array<{ text?: string }> } } | undefined)?.message?.parts);
  const text = parts?.map((part) => part.text).filter(Boolean).join(" ");
  if (text) return text.slice(0, 240);
  const artifact = parsed["artifact"] as { parts?: Array<{ text?: string }> } | undefined;
  const artifactText = artifact?.parts?.map((part) => part.text).filter(Boolean).join(" ");
  if (artifactText) return artifactText.slice(0, 240);
  return "";
}

function startRuntimeEventObserver(): void {
  if (observerStarted || process.env["RUNTIME_EVENT_OBSERVER_ENABLED"] !== "true") return;
  const addr = process.env["REDIS_ADDR"];
  if (!addr) return;
  observerStarted = true;
  const [host, portRaw] = addr.split(":");
  const redis = new RedisClient(host ?? "redis", parseInt(portRaw ?? "6379", 10), process.env["REDIS_PASSWORD"] || undefined);
  redis.connect()
    .then(async () => {
      for (const channel of [ChannelTasks, ChannelEvents, ChannelControl]) {
        await redis.subscribe(channel, (_channel, payload) => {
          recordEvent(parseRedisEvent(_channel, payload));
        });
      }
    })
    .catch((err) => {
      observerStarted = false;
      process.stderr.write(`[runtime-inventory] Redis observer failed: ${err instanceof Error ? err.message : String(err)}\n`);
    });
}

export function registerRuntimeInventoryTools(server: McpServer): void {
  startRuntimeEventObserver();

  server.registerTool(
    "runtime_inventory_scan",
    {
      title: "Scan Local Runtime Inventory",
      description: "Build a sanitized local runtime inventory of Sage services, routes, agents, prompts, MCP tools, mounts, workspace files, architecture docs, Redis channels, and prompt-source conflicts.",
      inputSchema: z.object({
        scope: z.string().optional().describe("Optional label for the scan scope. v1 scans local-runtime regardless of value."),
        refresh: z.boolean().optional().describe("Force a fresh scan instead of returning the latest snapshot."),
      }).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async ({ refresh }) => {
      const snapshot = getSnapshot(refresh ?? true);
      return {
        content: [{ type: "text", text: JSON.stringify(snapshot, null, 2) }],
        structuredContent: snapshot as unknown as Record<string, unknown>,
      };
    },
  );

  server.registerTool(
    "runtime_inventory_search",
    {
      title: "Search Runtime Inventory",
      description: "Search the latest runtime inventory by entity name, route, service, prompt source, env var, tool, architecture doc, or relationship metadata.",
      inputSchema: z.object({
        query: z.string().min(1),
        entity_type: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional(),
      }).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async ({ query, entity_type, limit }) => {
      const snapshot = getSnapshot(false);
      const results = searchSnapshot(snapshot, query, entity_type, limit ?? 25);
      const output = { query, count: results.length, results };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "runtime_inventory_events",
    {
      title: "Read Runtime Inventory Events",
      description: "Return recent Redis-observed Sage task/activity annotations. These annotate runtime flow; run runtime_inventory_scan for source-of-truth file state.",
      inputSchema: z.object({
        context_id: z.string().optional(),
        task_id: z.string().optional(),
        limit: z.number().int().min(1).max(200).optional(),
      }).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async ({ context_id, task_id, limit }) => {
      const filtered = eventBuffer
        .filter((evt) => !context_id || evt.contextId === context_id)
        .filter((evt) => !task_id || evt.taskId === task_id)
        .slice(-(limit ?? 50));
      const output = { count: filtered.length, events: filtered };
      return {
        content: [{ type: "text", text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );
}
