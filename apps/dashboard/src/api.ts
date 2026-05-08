import { MANAGER_URL } from "./config.js";

const CONTEXT_KEY = "sage_chat_context_id";

// Local storage only tracks this browser's fallback context. Durable shared
// sessions live in the manager and are exposed through /chat/sessions.
export function createNewContextId(): string {
  return `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function setContextId(id: string): void {
  localStorage.setItem(CONTEXT_KEY, id);
}

export function getContextId(): string {
  let id = localStorage.getItem(CONTEXT_KEY);
  if (!id) {
    id = createNewContextId();
    setContextId(id);
  }
  return id;
}

export interface ChatResponse {
  taskId: string;
  contextId: string;
}

export interface DispatchResponse {
  request_id: string;
  content: string;
  agent: string;
  status: string;
  error?: string;
  timestamp: number;
}

export type AgentMode = "auto" | "solo" | "launch";

export interface ChatModeSelection {
  agentMode: AgentMode;
  targetAgentId?: string;
  label: string;
  description?: string;
}

export interface AgentCatalogMode {
  id: AgentMode;
  label: string;
  description?: string;
  enabled: boolean;
  disabledReason?: string;
}

export interface AgentCatalogAgent {
  id: string;
  displayName: string;
  description?: string;
  authority?: string;
  targetable: boolean;
  modes: AgentCatalogMode[];
}

export interface AgentCatalog {
  defaultMode: ChatModeSelection;
  agents: AgentCatalogAgent[];
}

export interface AgentModelConfig {
  agentId: string;
  displayName: string;
  currentModel: string;
  configuredModel: string;
  source: string;
}

export interface AgentModelCatalog {
  agents: AgentModelConfig[];
  modelOptions: string[];
}

export interface CopilotProviderStatus {
  connected: boolean;
  tokenSource?: string;
  cachedTokenValid: boolean;
  cachedTokenExpires?: number;
  oauthStored: boolean;
  envTokenAvailable: boolean;
  error?: string;
}

export interface GitHubDeviceLoginStart {
  deviceCode: string;
  userCode: string;
  verificationUri: string;
  expiresIn: number;
  interval: number;
}

export interface ToolCatalogItem {
  id: string;
  name: string;
  description: string;
  source: "mcp" | "custom";
  enabled: boolean;
  assignedAgentIds?: string[];
  area?: string;
  command?: string;
  args?: string;
  createdAt?: number;
  updatedAt?: number;
}

export interface ToolCatalog {
  tools: ToolCatalogItem[];
  syncedAt: number;
}

export interface SkillCatalogItem {
  id: string;
  name: string;
  description: string;
  tags?: string[];
  enabled: boolean;
  source: string;
  updatedAt: number;
}

export interface SkillCatalog {
  skills: SkillCatalogItem[];
  syncedAt: number;
}

export interface SkillContentItem {
  id: string;
  name: string;
  description: string;
  tags?: string[];
  enabled: boolean;
  content: string;
  updatedAt: number;
}

export type DiscoveredSkillState = "quarantined" | "released" | "disabled";
export type SkillSourceTrust = "trusted" | "untrusted";

export interface DiscoveredSkillSource {
  id: string;
  displayName: string;
  endpoint: string;
  trust: SkillSourceTrust;
  enabled: boolean;
  sourceType: "remote" | "local";
  lastSyncAt?: string;
  lastSyncStatus?: string;
  lastSyncError?: string;
}

export interface DiscoveredSkillItem {
  id: string;
  sourceId: string;
  sourceName: string;
  originalToolName: string;
  canonicalName: string;
  description: string;
  tags?: string[];
  riskLevel: "low" | "medium" | "high";
  executionType: "stateless" | "stateful";
  requiresSession: boolean;
  skillState: DiscoveredSkillState;
  allowedAgents?: string[];
  updatedAt?: string;
}

export type ChatTranscriptRole = "user" | "assistant";
export type ChatTranscriptStatus = "pending" | "done" | "error";

export interface ChatTranscriptMessage {
  id: string;
  role: ChatTranscriptRole;
  text: string;
  createdAt: number;
  status?: ChatTranscriptStatus;
  taskId?: string;
}

export interface ChatSession {
  id: string;
  title: string;
  createdAt: number;
  updatedAt: number;
  messageCount?: number;
  agentMode?: AgentMode;
  targetAgentId?: string;
  modeLabel?: string;
}

export interface ChatSessionDetail {
  session: ChatSession;
  messages: ChatTranscriptMessage[];
}

async function responseText(res: Response): Promise<string> {
  const text = await res.text();
  return text.trim() || res.statusText;
}

export async function listChatSessions(): Promise<ChatSession[]> {
  const res = await fetch(`${MANAGER_URL}/chat/sessions`);
  if (!res.ok) {
    throw new Error(`/chat/sessions ${res.status}: ${await responseText(res)}`);
  }
  const body = (await res.json()) as { sessions?: ChatSession[] };
  return Array.isArray(body.sessions) ? body.sessions : [];
}

export async function createChatSession(
  contextId?: string,
  title = "New chat",
): Promise<ChatSession> {
  const body: { contextId?: string; title: string } = { title };
  if (contextId) body.contextId = contextId;
  const res = await fetch(`${MANAGER_URL}/chat/sessions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`/chat/sessions ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ChatSession;
}

export async function getChatSession(contextId: string): Promise<ChatSessionDetail> {
  const res = await fetch(`${MANAGER_URL}/chat/sessions/${encodeURIComponent(contextId)}`);
  if (!res.ok) {
    throw new Error(`/chat/sessions/${contextId} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ChatSessionDetail;
}

export async function deleteChatSession(contextId: string): Promise<void> {
  const res = await fetch(`${MANAGER_URL}/chat/sessions/${encodeURIComponent(contextId)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(`/chat/sessions/${contextId} ${res.status}: ${await responseText(res)}`);
  }
}

export async function updateChatSessionMode(
  contextId: string,
  selection: ChatModeSelection,
): Promise<ChatSession> {
  const res = await fetch(`${MANAGER_URL}/chat/sessions/${encodeURIComponent(contextId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      agentMode: selection.agentMode,
      targetAgentId: selection.targetAgentId ?? "",
    }),
  });
  if (!res.ok) {
    throw new Error(`/chat/sessions/${contextId} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ChatSession;
}

export async function getAgentCatalog(): Promise<AgentCatalog> {
  const res = await fetch(`${MANAGER_URL}/agents/catalog`);
  if (!res.ok) {
    throw new Error(`/agents/catalog ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as AgentCatalog;
}

export async function getAgentModelCatalog(): Promise<AgentModelCatalog> {
  const res = await fetch(`${MANAGER_URL}/agents/models`);
  if (!res.ok) {
    throw new Error(`/agents/models ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as AgentModelCatalog;
}

export async function updateAgentModel(agentId: string, model: string): Promise<AgentModelConfig> {
  const res = await fetch(`${MANAGER_URL}/agents/models`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      agentId,
      model,
    }),
  });
  if (!res.ok) {
    throw new Error(`/agents/models ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as AgentModelConfig;
}

export async function getCopilotProviderStatus(): Promise<CopilotProviderStatus> {
  const res = await fetch(`${MANAGER_URL}/providers/copilot/status`);
  if (!res.ok) {
    throw new Error(`/providers/copilot/status ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as CopilotProviderStatus;
}

export async function startCopilotLogin(clientId?: string): Promise<GitHubDeviceLoginStart> {
  const res = await fetch(`${MANAGER_URL}/providers/copilot/login/start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ clientId: clientId ?? "" }),
  });
  if (!res.ok) {
    throw new Error(`/providers/copilot/login/start ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as GitHubDeviceLoginStart;
}

export async function completeCopilotLogin(input: {
  clientId?: string;
  deviceCode?: string;
  token?: string;
}): Promise<CopilotProviderStatus> {
  const res = await fetch(`${MANAGER_URL}/providers/copilot/login/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      clientId: input.clientId ?? "",
      deviceCode: input.deviceCode ?? "",
      token: input.token ?? "",
    }),
  });
  if (!res.ok) {
    throw new Error(`/providers/copilot/login/complete ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as CopilotProviderStatus;
}

export async function refreshCopilotProviderToken(): Promise<CopilotProviderStatus> {
  const res = await fetch(`${MANAGER_URL}/providers/copilot/refresh`, {
    method: "POST",
  });
  if (!res.ok) {
    throw new Error(`/providers/copilot/refresh ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as CopilotProviderStatus;
}

export async function logoutCopilotProvider(): Promise<CopilotProviderStatus> {
  const res = await fetch(`${MANAGER_URL}/providers/copilot/logout`, {
    method: "POST",
  });
  if (!res.ok) {
    throw new Error(`/providers/copilot/logout ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as CopilotProviderStatus;
}

export async function getToolCatalog(): Promise<ToolCatalog> {
  const res = await fetch(`${MANAGER_URL}/tools/catalog`);
  if (!res.ok) {
    throw new Error(`/tools/catalog ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ToolCatalog;
}

export async function createToolCatalogItem(input: {
  id: string;
  name?: string;
  description?: string;
  enabled?: boolean;
  assignedAgentIds?: string[];
  area?: string;
  command: string;
  args?: string;
}): Promise<ToolCatalogItem> {
  const res = await fetch(`${MANAGER_URL}/tools/catalog`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(`/tools/catalog ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ToolCatalogItem;
}

export async function updateToolCatalogItem(
  id: string,
  patch: {
    name?: string;
    description?: string;
    enabled?: boolean;
    assignedAgentIds?: string[];
    area?: string;
    command?: string;
    args?: string;
  },
): Promise<ToolCatalogItem> {
  const res = await fetch(`${MANAGER_URL}/tools/catalog/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) {
    throw new Error(`/tools/catalog/${id} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as ToolCatalogItem;
}

export async function deleteToolCatalogItem(id: string): Promise<void> {
  const res = await fetch(`${MANAGER_URL}/tools/catalog/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(`/tools/catalog/${id} ${res.status}: ${await responseText(res)}`);
  }
}

export async function getSkillCatalog(): Promise<SkillCatalog> {
  const res = await fetch(`${MANAGER_URL}/skills/catalog`);
  if (!res.ok) {
    throw new Error(`/skills/catalog ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as SkillCatalog;
}

export async function getSkillContent(id: string): Promise<SkillContentItem> {
  const res = await fetch(`${MANAGER_URL}/skills/catalog/${encodeURIComponent(id)}`);
  if (!res.ok) {
    throw new Error(`/skills/catalog/${id} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as SkillContentItem;
}

export async function createSkill(input: {
  id: string;
  name?: string;
  description?: string;
  tags?: string[];
  enabled?: boolean;
  content?: string;
}): Promise<SkillContentItem> {
  const res = await fetch(`${MANAGER_URL}/skills/catalog`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(`/skills/catalog ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as SkillContentItem;
}

export async function composeSkill(input: {
  id: string;
  name?: string;
  description?: string;
  tags?: string[];
  enabled?: boolean;
  trigger?: string;
  assignedAgentIds?: string[];
  inputs?: string;
  outputs?: string;
  notes?: string;
}): Promise<SkillContentItem> {
  const res = await fetch(`${MANAGER_URL}/skills/compose`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(`/skills/compose ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as SkillContentItem;
}

export async function updateSkill(
  id: string,
  patch: {
    name?: string;
    description?: string;
    tags?: string[];
    enabled?: boolean;
    content?: string;
  },
): Promise<SkillContentItem> {
  const res = await fetch(`${MANAGER_URL}/skills/catalog/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) {
    throw new Error(`/skills/catalog/${id} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as SkillContentItem;
}

export async function deleteSkill(id: string): Promise<void> {
  const res = await fetch(`${MANAGER_URL}/skills/catalog/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(`/skills/catalog/${id} ${res.status}: ${await responseText(res)}`);
  }
}

export async function getDiscoveredSkillSources(): Promise<DiscoveredSkillSource[]> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/servers`);
  if (!res.ok) {
    throw new Error(`/skills/discovered/servers ${res.status}: ${await responseText(res)}`);
  }
  const payload = (await res.json()) as { sources?: DiscoveredSkillSource[] };
  return Array.isArray(payload.sources) ? payload.sources : [];
}

export async function createDiscoveredSkillSource(input: {
  id: string;
  displayName: string;
  endpoint: string;
  trust?: SkillSourceTrust;
  enabled?: boolean;
}): Promise<DiscoveredSkillSource> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/servers`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/servers ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as DiscoveredSkillSource;
}

export async function updateDiscoveredSkillSource(
  id: string,
  patch: {
    displayName?: string;
    endpoint?: string;
    trust?: SkillSourceTrust;
    enabled?: boolean;
  },
): Promise<DiscoveredSkillSource> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/servers/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/servers/${id} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as DiscoveredSkillSource;
}

export async function releaseDiscoveredSkillSource(id: string): Promise<{ sourceId: string; released: number }> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/servers/${encodeURIComponent(id)}/release`, {
    method: "POST",
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/servers/${id}/release ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as { sourceId: string; released: number };
}

export async function syncDiscoveredSkillSource(id: string): Promise<Record<string, unknown>> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/servers/${encodeURIComponent(id)}/sync`, {
    method: "POST",
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/servers/${id}/sync ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as Record<string, unknown>;
}

export async function syncDiscoveredSkills(): Promise<Record<string, unknown>> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/sync`, {
    method: "POST",
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/sync ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as Record<string, unknown>;
}

export async function getDiscoveredSkills(filters?: {
  sourceId?: string;
  state?: DiscoveredSkillState;
}): Promise<DiscoveredSkillItem[]> {
  const params = new URLSearchParams();
  if (filters?.sourceId) params.set("sourceId", filters.sourceId);
  if (filters?.state) params.set("state", filters.state);
  const suffix = params.toString();
  const res = await fetch(`${MANAGER_URL}/skills/discovered/skills${suffix ? `?${suffix}` : ""}`);
  if (!res.ok) {
    throw new Error(`/skills/discovered/skills ${res.status}: ${await responseText(res)}`);
  }
  const payload = (await res.json()) as { skills?: DiscoveredSkillItem[] };
  return Array.isArray(payload.skills) ? payload.skills : [];
}

export async function updateDiscoveredSkill(
  id: string,
  patch: {
    state?: DiscoveredSkillState;
    allowedAgents?: string[];
  },
): Promise<DiscoveredSkillItem> {
  const res = await fetch(`${MANAGER_URL}/skills/discovered/skills/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) {
    throw new Error(`/skills/discovered/skills/${id} ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as DiscoveredSkillItem;
}

export async function sendChat(
  content: string,
  contextId = getContextId(),
  selection?: ChatModeSelection,
): Promise<ChatResponse> {
  const res = await fetch(`${MANAGER_URL}/chat`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      contextId,
      content,
      agentMode: selection?.agentMode,
      targetAgentId: selection?.targetAgentId ?? "",
    }),
  });
  if (!res.ok) {
    const text = await res.text();
    if (res.status === 404) {
      throw new Error(
        `Manager at ${MANAGER_URL} does not expose /chat. Rebuild/restart manager and try again.`,
      );
    }
    throw new Error(`/chat ${res.status}: ${text}`);
  }
  return (await res.json()) as ChatResponse;
}

export async function dispatchManager(
  content: string,
  capability?: string,
  sessionId?: string,
): Promise<DispatchResponse> {
  const res = await fetch(`${MANAGER_URL}/dispatch`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      content,
      capability: capability ?? "acp:cap:skill.agent-delegate",
      resource: "sage://workspace/*",
      session_id: sessionId ?? "",
    }),
  });
  if (!res.ok) {
    throw new Error(`/dispatch ${res.status}: ${await responseText(res)}`);
  }
  return (await res.json()) as DispatchResponse;
}

export async function sendContinue(
  taskId: string,
  decision: "continue" | "stop",
  note?: string,
): Promise<void> {
  const res = await fetch(`${MANAGER_URL}/chat/continue`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      taskId,
      decision,
      note: note ?? "",
    }),
  });
  if (!res.ok) {
    const text = await res.text();
    if (res.status === 404) {
      throw new Error(
        `Manager at ${MANAGER_URL} does not expose /chat/continue. Rebuild/restart manager and try again.`,
      );
    }
    throw new Error(`/chat/continue ${res.status}: ${text}`);
  }
}

export async function stopChat(taskId: string): Promise<void> {
  const res = await fetch(`${MANAGER_URL}/chat/stop`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ taskId }),
  });
  if (!res.ok) {
    const text = await res.text();
    if (res.status === 404) {
      throw new Error(`Task is no longer active: ${taskId}`);
    }
    throw new Error(`/chat/stop ${res.status}: ${text}`);
  }
}

// File browser APIs
export interface WorkspaceFile {
	name: string;
	path: string;
	isDir: boolean;
	size: number;
	modTime: number;
}

export interface WorkspaceFileListResponse {
	files: WorkspaceFile[];
	path: string;
}

export async function listWorkspaceFiles(dir: string = ""): Promise<WorkspaceFileListResponse> {
	const params = new URLSearchParams();
	if (dir) {
		params.set("dir", dir);
	}
	const res = await fetch(`${MANAGER_URL}/workspace/files/list?${params}`);
	if (!res.ok) {
		throw new Error(`/workspace/files/list ${res.status}: ${await responseText(res)}`);
	}
	return (await res.json()) as WorkspaceFileListResponse;
}

export async function downloadFile(path: string): Promise<void> {
	const params = new URLSearchParams();
	params.set("path", path);
	const res = await fetch(`${MANAGER_URL}/workspace/files/download?${params}`);
	if (!res.ok) {
		throw new Error(`/workspace/files/download ${res.status}: ${await responseText(res)}`);
	}

	// Get filename from content-disposition if available
	const contentDisposition = res.headers.get("content-disposition");
	let filename = path.split("/").pop() || "download";
	if (contentDisposition) {
		const matches = contentDisposition.match(/filename[^;=\n]*=(?:(["\'])(.+?)\1|([^;\n]*))/) || [];
		if (matches[2] || matches[3]) {
			filename = matches[2] || matches[3];
		}
	}

	const blob = await res.blob();
	const url = window.URL.createObjectURL(blob);
	const a = document.createElement("a");
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	window.URL.revokeObjectURL(url);
	document.body.removeChild(a);
}
