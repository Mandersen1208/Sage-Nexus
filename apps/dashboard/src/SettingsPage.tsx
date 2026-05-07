import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	composeSkill,
	createToolCatalogItem,
	deleteSkill,
	deleteToolCatalogItem,
  dispatchManager,
  completeCopilotLogin,
  getCopilotProviderStatus,
	getSkillCatalog,
	getSkillContent,
  getToolCatalog,
  logoutCopilotProvider,
  refreshCopilotProviderToken,
  startCopilotLogin,
	updateSkill,
  updateToolCatalogItem,
  type CopilotProviderStatus,
  type GitHubDeviceLoginStart,
	type SkillCatalogItem,
	type SkillContentItem,
  type ToolCatalogItem,
} from "./api.js";

type DeviceFlowState = GitHubDeviceLoginStart & {
  startedAt: number;
  nextPollMs: number;
  status: "waiting" | "completing" | "failed";
  error?: string;
};

type LocalMCPSetup = {
  workspaceRoot: string;
  serverCommand: string;
  serverArgs: string;
  autoStart: boolean;
};

type LocalMCPTool = {
  id: string;
  name: string;
  description: string;
  command: string;
  args: string;
  enabled: boolean;
  assignedAgentIds?: string[];
  area?: string;
  createdAt: number;
};

type ToolDraft = {
  id: string;
  name: string;
  description: string;
  command: string;
  args: string;
  enabled: boolean;
  area: string;
  assignedAgentIdsText: string;
};

type SkillDraft = {
  id: string;
  name: string;
  description: string;
  tagsText: string;
  enabled: boolean;
  trigger: string;
  assignedAgentIdsText: string;
  inputs: string;
  outputs: string;
  notes: string;
  content: string;
};

type ManagerChatMessage = {
  role: "user" | "manager";
  text: string;
  ts: number;
};

const localSetupKey = "sage_local_mcp_setup";
const localToolsKey = "sage_local_mcp_tools";
const localToolsMigrationKey = "sage_local_mcp_tools_migrated_v1";

const defaultLocalSetup: LocalMCPSetup = {
  workspaceRoot: "",
  serverCommand: "node",
  serverArgs: "services/sage-mcp/dist/index.js",
  autoStart: false,
};

const defaultToolDraft: ToolDraft = {
  id: "",
  name: "",
  description: "",
  command: "",
  args: "",
  enabled: true,
  area: "",
  assignedAgentIdsText: "",
};

const defaultSkillDraft: SkillDraft = {
  id: "",
  name: "",
  description: "",
  tagsText: "",
  enabled: true,
  trigger: "",
  assignedAgentIdsText: "",
  inputs: "",
  outputs: "",
  notes: "",
  content: "",
};

function formatExpiry(ts: number | undefined): string {
  if (!ts) return "n/a";
  return new Date(ts).toLocaleString();
}

function isPendingAuthError(message: string): boolean {
  const value = message.toLowerCase();
  return value.includes("authorization_pending") || value.includes("slow_down");
}

function loadLocalSetup(): LocalMCPSetup {
  try {
    const raw = localStorage.getItem(localSetupKey);
    if (!raw) return defaultLocalSetup;
    const parsed = JSON.parse(raw) as Partial<LocalMCPSetup>;
    return {
      workspaceRoot: parsed.workspaceRoot ?? "",
      serverCommand: parsed.serverCommand ?? defaultLocalSetup.serverCommand,
      serverArgs: parsed.serverArgs ?? defaultLocalSetup.serverArgs,
      autoStart: !!parsed.autoStart,
    };
  } catch {
    return defaultLocalSetup;
  }
}

function loadLocalTools(): LocalMCPTool[] {
  try {
    const raw = localStorage.getItem(localToolsKey);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as LocalMCPTool[];
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item) => typeof item.id === "string" && item.id.trim() !== "");
  } catch {
    return [];
  }
}

function extractJsonObject(text: string): Record<string, unknown> | null {
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start < 0 || end <= start) return null;
  const raw = text.slice(start, end + 1);
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // ignore parse errors
  }
  return null;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => asString(item)).filter((item) => item.length > 0);
}

function buildManagerOnlyDraftingHeader(kind: "skill" | "tool"): string {
  const target = kind === "skill" ? "skill capability" : "tool capability";
  return [
    `You are in Manager-Only Create-and-Design Mode for ${target}.`,
    "Do not delegate to worker agents.",
    "Do not edit files or execute code changes.",
    "Return a plan-first response with concise rationale and implementation-ready fields.",
    "Primary job: design and create a high-quality draft for this capability.",
    "Core planning skills and tooling constraints:",
    "- File/Directory Reading (Read/LS): inspect structure without making changes.",
    "- Search (Grep/Glob): search patterns and file types across project.",
    "- Ask User: request clarification only when needed.",
    "- Repository Structure Analysis: reason about dependencies and workflow.",
    "- Task/Research Agents: research complex issues before execution.",
    "- Git History/Diffs: use prior changes as context.",
    "- Todo Lists: track steps and completion checkpoints.",
  ].join("\n");
}

function parseLabeledLine(text: string, labels: string[]): string {
  const escaped = labels.map((label) => label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const re = new RegExp(`(?:^|\\n)\\s*(?:${escaped.join("|")})\\s*:\\s*([^\\n]+)`, "i");
  const match = text.match(re);
  return match?.[1]?.trim() ?? "";
}

function parseLabeledList(text: string, labels: string[]): string[] {
  const raw = parseLabeledLine(text, labels);
  if (!raw) return [];
  return raw
    .split(/[,\|]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

export default function SettingsPage() {
  const [status, setStatus] = useState<CopilotProviderStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [deviceFlow, setDeviceFlow] = useState<DeviceFlowState | null>(null);
  const [localSetup, setLocalSetup] = useState<LocalMCPSetup>(() => loadLocalSetup());
  const [tools, setTools] = useState<ToolCatalogItem[]>([]);
  const [toolModalOpen, setToolModalOpen] = useState(false);
  const [toolDraft, setToolDraft] = useState<ToolDraft>(defaultToolDraft);
  const [setupDraft, setSetupDraft] = useState<LocalMCPSetup>(defaultLocalSetup);
  const [editingToolId, setEditingToolId] = useState<string | null>(null);
  const [toolError, setToolError] = useState<string | null>(null);
  const [toolAiPrompt, setToolAiPrompt] = useState("");
  const [toolAiMessages, setToolAiMessages] = useState<ManagerChatMessage[]>([]);
  const [toolAiBusy, setToolAiBusy] = useState(false);
  const [skills, setSkills] = useState<SkillCatalogItem[]>([]);
  const [skillModalOpen, setSkillModalOpen] = useState(false);
  const [skillDraft, setSkillDraft] = useState<SkillDraft>(defaultSkillDraft);
  const [editingSkillId, setEditingSkillId] = useState<string | null>(null);
  const [skillAdvancedOpen, setSkillAdvancedOpen] = useState(false);
  const [skillError, setSkillError] = useState<string | null>(null);
  const [skillBusy, setSkillBusy] = useState(false);
  const [skillAiPrompt, setSkillAiPrompt] = useState("");
  const [skillAiMessages, setSkillAiMessages] = useState<ManagerChatMessage[]>([]);
  const [skillAiBusy, setSkillAiBusy] = useState(false);
  const timerRef = useRef<number | null>(null);

  const clearTimer = useCallback(() => {
    if (timerRef.current != null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const loadStatus = useCallback(async () => {
    const next = await getCopilotProviderStatus();
    setStatus(next);
    return next;
  }, []);

  useEffect(() => {
    let cancelled = false;
    const run = async () => {
      try {
        const next = await getCopilotProviderStatus();
        if (!cancelled) {
          setStatus(next);
          setErr(null);
        }
      } catch (e) {
        if (!cancelled) {
          setErr(e instanceof Error ? e.message : String(e));
        }
      }
    };
    void run();
    const id = window.setInterval(() => void run(), 30_000);
    return () => {
      cancelled = true;
      clearTimer();
      window.clearInterval(id);
    };
  }, [clearTimer]);

  useEffect(() => {
    localStorage.setItem(localSetupKey, JSON.stringify(localSetup));
  }, [localSetup]);

  const loadTools = useCallback(async () => {
    const catalog = await getToolCatalog();
    setTools(catalog.tools);
    return catalog.tools;
  }, []);

  const loadSkills = useCallback(async () => {
    const catalog = await getSkillCatalog();
    setSkills(catalog.skills);
    return catalog.skills;
  }, []);

  useEffect(() => {
    void loadTools().catch((e) => {
      setToolError(e instanceof Error ? e.message : String(e));
    });
  }, [loadTools]);

  useEffect(() => {
    void loadSkills().catch((e) => {
      setSkillError(e instanceof Error ? e.message : String(e));
    });
  }, [loadSkills]);

  const onNewSkill = useCallback(() => {
    setEditingSkillId(null);
    setSkillDraft(defaultSkillDraft);
    setSkillAdvancedOpen(false);
    setSkillAiPrompt("");
    setSkillAiMessages([]);
    setSkillError(null);
    setSkillModalOpen(true);
  }, []);

  const onEditSkill = useCallback((id: string) => {
    const run = async () => {
      setSkillBusy(true);
      setSkillError(null);
      const skill: SkillContentItem = await getSkillContent(id);
      setEditingSkillId(skill.id);
      setSkillDraft({
        id: skill.id,
        name: skill.name,
        description: skill.description ?? "",
        tagsText: (skill.tags ?? []).join(", "),
        enabled: skill.enabled,
        trigger: "",
        assignedAgentIdsText: "",
        inputs: "",
        outputs: "",
        notes: "",
        content: skill.content,
      });
      setSkillAdvancedOpen(false);
      setSkillAiPrompt("");
      setSkillAiMessages([]);
      setSkillModalOpen(true);
    };
    void run()
      .catch((e) => setSkillError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSkillBusy(false));
  }, []);

  const onSaveSkill = useCallback(() => {
    const id = skillDraft.id.trim().toLowerCase();
    if (!id) {
      setSkillError("Skill id is required.");
      return;
    }
    if (!/^[a-z0-9][a-z0-9-_]*$/.test(id)) {
      setSkillError("Skill id must be lowercase alphanumeric and may include - or _.");
      return;
    }
    const tags = skillDraft.tagsText
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);
    const assignedAgentIds = skillDraft.assignedAgentIdsText
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);
    const run = async () => {
      setSkillBusy(true);
      setSkillError(null);
      if (editingSkillId) {
        await updateSkill(editingSkillId, {
          name: skillDraft.name.trim(),
          description: skillDraft.description.trim(),
          tags,
          enabled: skillDraft.enabled,
          content: skillDraft.content.trim() || undefined,
        });
        setSkillModalOpen(false);
        setSuccess(`Updated skill ${editingSkillId}.`);
      } else {
        await composeSkill({
          id,
          name: skillDraft.name.trim(),
          description: skillDraft.description.trim(),
          tags,
          enabled: skillDraft.enabled,
          trigger: skillDraft.trigger.trim(),
          assignedAgentIds,
          inputs: skillDraft.inputs.trim(),
          outputs: skillDraft.outputs.trim(),
          notes: skillDraft.notes.trim(),
        });
        setEditingSkillId(id);
        setSkillModalOpen(false);
        setSuccess(`Created skill ${id}.`);
      }
      await loadSkills();
    };
    void run()
      .catch((e) => setSkillError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSkillBusy(false));
  }, [editingSkillId, loadSkills, skillDraft]);

  const onToggleSkill = useCallback((skill: SkillCatalogItem) => {
    const run = async () => {
      setSkillBusy(true);
      setSkillError(null);
      await updateSkill(skill.id, { enabled: !skill.enabled });
      setSuccess(`${skill.enabled ? "Disabled" : "Enabled"} skill ${skill.id}.`);
      if (editingSkillId === skill.id) {
        setSkillDraft((prev) => ({ ...prev, enabled: !skill.enabled }));
      }
      await loadSkills();
    };
    void run()
      .catch((e) => setSkillError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSkillBusy(false));
  }, [editingSkillId, loadSkills]);

  const onDeleteSkill = useCallback((id: string) => {
    if (!window.confirm(`Delete skill ${id}?`)) return;
    const run = async () => {
      setSkillBusy(true);
      setSkillError(null);
      await deleteSkill(id);
      if (editingSkillId === id) {
        setEditingSkillId(null);
        setSkillDraft(defaultSkillDraft);
      }
      setSuccess(`Deleted skill ${id}.`);
      await loadSkills();
    };
    void run()
      .catch((e) => setSkillError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSkillBusy(false));
  }, [editingSkillId, loadSkills]);

  useEffect(() => {
    const migrated = localStorage.getItem(localToolsMigrationKey) === "true";
    if (migrated) return;
    const legacy = loadLocalTools();
    if (legacy.length === 0) {
      localStorage.setItem(localToolsMigrationKey, "true");
      return;
    }
    const run = async () => {
      for (const item of legacy) {
        try {
          await createToolCatalogItem({
            id: item.id,
            name: item.name,
            description: item.description,
            command: item.command || "local-tool",
            args: item.args,
            enabled: item.enabled,
            area: item.area,
            assignedAgentIds: item.assignedAgentIds,
          });
        } catch {
          // Ignore duplicate/create errors during one-time migration.
        }
      }
      localStorage.setItem(localToolsMigrationKey, "true");
      localStorage.removeItem(localToolsKey);
      await loadTools();
    };
    void run();
  }, [loadTools]);

  const onStartDeviceLogin = useCallback(async () => {
    setBusy(true);
    setErr(null);
    setSuccess(null);
    clearTimer();
    try {
      const start = await startCopilotLogin();
      const flow: DeviceFlowState = {
        ...start,
        startedAt: Date.now(),
        nextPollMs: Math.max(2000, start.interval * 1000),
        status: "waiting",
      };
      setDeviceFlow(flow);
      window.open(start.verificationUri, "_blank", "noopener,noreferrer");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [clearTimer]);

  const onOpenDevicePage = useCallback(() => {
    if (!deviceFlow) return;
    window.open(deviceFlow.verificationUri, "_blank", "noopener,noreferrer");
  }, [deviceFlow]);

  const onCancelDeviceLogin = useCallback(() => {
    clearTimer();
    setDeviceFlow(null);
  }, [clearTimer]);

  useEffect(() => {
    if (!deviceFlow || deviceFlow.status === "failed") return;

    const expiresAt = deviceFlow.startedAt + deviceFlow.expiresIn * 1000;
    const delay = deviceFlow.nextPollMs;

    clearTimer();
    timerRef.current = window.setTimeout(() => {
      const complete = async () => {
        if (Date.now() > expiresAt) {
          setDeviceFlow((prev) =>
            prev
              ? {
                  ...prev,
                  status: "failed",
                  error: "Device code expired. Start a new login.",
                }
              : prev,
          );
          return;
        }

        try {
          setDeviceFlow((prev) => (prev ? { ...prev, status: "completing", error: undefined } : prev));
          const next = await completeCopilotLogin({ deviceCode: deviceFlow.deviceCode });
          setStatus(next);
          setDeviceFlow(null);
          setSuccess("Copilot connected and token saved.");
          setErr(null);
        } catch (e) {
          const message = e instanceof Error ? e.message : String(e);
          if (isPendingAuthError(message)) {
            const slowDown = message.toLowerCase().includes("slow_down");
            setDeviceFlow((prev) =>
              prev
                ? {
                    ...prev,
                    status: "waiting",
                    nextPollMs: slowDown ? Math.min(prev.nextPollMs + 2000, 15_000) : prev.nextPollMs,
                  }
                : prev,
            );
            return;
          }
          setDeviceFlow((prev) =>
            prev
              ? {
                  ...prev,
                  status: "failed",
                  error: message,
                }
              : prev,
          );
        }
      };
      void complete();
    }, delay);

    return () => clearTimer();
  }, [clearTimer, deviceFlow]);

  const onRefresh = useCallback(async () => {
    setBusy(true);
    setErr(null);
    setSuccess(null);
    try {
      const next = await refreshCopilotProviderToken();
      setStatus(next);
      setSuccess("Copilot token refresh completed.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
      try {
        await loadStatus();
      } catch {
        // keep original error text visible
      }
    } finally {
      setBusy(false);
    }
  }, [loadStatus]);

  const onLogout = useCallback(async () => {
    setBusy(true);
    setErr(null);
    setSuccess(null);
    try {
      const next = await logoutCopilotProvider();
      setStatus(next);
      setDeviceFlow(null);
      setSuccess("Copilot auth cleared.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  const onSaveLocalSetup = useCallback(() => {
    setSuccess("Local MCP setup saved on this machine.");
    setErr(null);
  }, []);

  const onOpenToolModal = useCallback(() => {
    setToolDraft(defaultToolDraft);
    setSetupDraft(localSetup);
    setEditingToolId(null);
    setToolAiPrompt("");
    setToolAiMessages([]);
    setToolError(null);
    setToolModalOpen(true);
  }, [localSetup]);

  const onEditToolModal = useCallback((tool: ToolCatalogItem) => {
    setToolDraft({
      id: tool.id,
      name: tool.name,
      description: tool.description,
      command: tool.command ?? "",
      args: tool.args ?? "",
      enabled: tool.enabled,
      area: tool.area ?? "",
      assignedAgentIdsText: (tool.assignedAgentIds ?? []).join(", "),
    });
    setSetupDraft(localSetup);
    setEditingToolId(tool.id);
    setToolAiPrompt("");
    setToolAiMessages([]);
    setToolError(null);
    setToolModalOpen(true);
  }, [localSetup]);

  const onAskManagerForSkill = useCallback(() => {
    const prompt = skillAiPrompt.trim();
    if (!prompt) {
      setSkillError("Enter a request for the manager first.");
      return;
    }
    const run = async () => {
      setSkillAiBusy(true);
      setSkillError(null);
      setSkillAiMessages((prev) => [...prev, { role: "user", text: prompt, ts: Date.now() }]);
      setSkillAiPrompt("");
      const response = await dispatchManager(
        [
          buildManagerOnlyDraftingHeader("skill"),
          "You are helping create a new skill capability for the manager settings modal.",
          "User will provide plain language only. Do not ask for JSON.",
          "Reply in natural language with clear labeled lines when possible.",
          "If possible include a JSON object too (optional) with shape:",
          '{"id":"","name":"","description":"","tags":[],"trigger":"","assignedAgentIds":[],"inputs":"","outputs":"","notes":""}',
          "Use lowercase hyphenated id and concise executable wording.",
          `User request: ${prompt}`,
        ].join("\n"),
      );
      setSkillAiMessages((prev) => [...prev, { role: "manager", text: response.content, ts: Date.now() }]);
      const parsed = extractJsonObject(response.content);
      if (parsed) {
        setSkillDraft((prev) => ({
          ...prev,
          id: asString(parsed.id) || prev.id,
          name: asString(parsed.name) || prev.name,
          description: asString(parsed.description) || prev.description,
          tagsText: asStringArray(parsed.tags).join(", ") || prev.tagsText,
          trigger: asString(parsed.trigger) || prev.trigger,
          assignedAgentIdsText: asStringArray(parsed.assignedAgentIds).join(", ") || prev.assignedAgentIdsText,
          inputs: asString(parsed.inputs) || prev.inputs,
          outputs: asString(parsed.outputs) || prev.outputs,
          notes: asString(parsed.notes) || prev.notes,
        }));
        return;
      }
      setSkillDraft((prev) => ({
        ...prev,
        id: parseLabeledLine(response.content, ["id", "skill id"]) || prev.id,
        name: parseLabeledLine(response.content, ["name", "skill name"]) || prev.name,
        description: parseLabeledLine(response.content, ["description", "purpose"]) || prev.description,
        tagsText: parseLabeledList(response.content, ["tags"]).join(", ") || prev.tagsText,
        trigger: parseLabeledLine(response.content, ["when to use", "trigger"]) || prev.trigger,
        assignedAgentIdsText: parseLabeledList(response.content, ["assigned agents", "agents", "intended agents"]).join(", ") || prev.assignedAgentIdsText,
        inputs: parseLabeledLine(response.content, ["inputs"]) || prev.inputs,
        outputs: parseLabeledLine(response.content, ["outputs", "expected output"]) || prev.outputs,
        notes: parseLabeledLine(response.content, ["notes", "execution notes"]) || prev.notes,
      }));
    };
    void run()
      .catch((e) => setSkillError(e instanceof Error ? e.message : String(e)))
      .finally(() => setSkillAiBusy(false));
  }, [skillAiPrompt]);

  const onAskManagerForTool = useCallback(() => {
    const prompt = toolAiPrompt.trim();
    if (!prompt) {
      setToolError("Enter a request for the manager first.");
      return;
    }
    const run = async () => {
      setToolAiBusy(true);
      setToolError(null);
      setToolAiMessages((prev) => [...prev, { role: "user", text: prompt, ts: Date.now() }]);
      setToolAiPrompt("");
      const response = await dispatchManager(
        [
          buildManagerOnlyDraftingHeader("tool"),
          "You are helping create a new tool capability for the manager settings modal.",
          "User will provide plain language only. Do not ask for JSON.",
          "Reply in natural language with clear labeled lines when possible.",
          "If possible include a JSON object too (optional) with shape:",
          '{"id":"","name":"","description":"","command":"","args":"","area":"","assignedAgentIds":[]}',
          "Use practical executable defaults.",
          `User request: ${prompt}`,
        ].join("\n"),
      );
      setToolAiMessages((prev) => [...prev, { role: "manager", text: response.content, ts: Date.now() }]);
      const parsed = extractJsonObject(response.content);
      if (parsed) {
        setToolDraft((prev) => ({
          ...prev,
          id: asString(parsed.id) || prev.id,
          name: asString(parsed.name) || prev.name,
          description: asString(parsed.description) || prev.description,
          command: asString(parsed.command) || prev.command,
          args: asString(parsed.args) || prev.args,
          area: asString(parsed.area) || prev.area,
          assignedAgentIdsText: asStringArray(parsed.assignedAgentIds).join(", ") || prev.assignedAgentIdsText,
        }));
        return;
      }
      setToolDraft((prev) => ({
        ...prev,
        id: parseLabeledLine(response.content, ["id", "tool id"]) || prev.id,
        name: parseLabeledLine(response.content, ["name", "tool name"]) || prev.name,
        description: parseLabeledLine(response.content, ["description", "purpose"]) || prev.description,
        command: parseLabeledLine(response.content, ["command"]) || prev.command,
        args: parseLabeledLine(response.content, ["args", "arguments"]) || prev.args,
        area: parseLabeledLine(response.content, ["area", "domain"]) || prev.area,
        assignedAgentIdsText: parseLabeledList(response.content, ["assigned agents", "agents"]).join(", ") || prev.assignedAgentIdsText,
      }));
    };
    void run()
      .catch((e) => setToolError(e instanceof Error ? e.message : String(e)))
      .finally(() => setToolAiBusy(false));
  }, [toolAiPrompt]);

  const onAddLocalTool = useCallback(() => {
    const id = toolDraft.id.trim();
    const command = toolDraft.command.trim();
    const assignedAgentIds = toolDraft.assignedAgentIdsText
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);
    if (!id) {
      setToolError("Tool id is required.");
      return;
    }
    if (!/^[a-z0-9][a-z0-9-_:.]*$/i.test(id)) {
      setToolError("Tool id must be alphanumeric and may include - _ : .");
      return;
    }
    if (!editingToolId && tools.some((item: ToolCatalogItem) => item.id.toLowerCase() === id.toLowerCase())) {
      setToolError(`Tool ${id} already exists.`);
      return;
    }
    if (!command) {
      setToolError("Tool command is required.");
      return;
    }
    if (!setupDraft.serverCommand.trim()) {
      setToolError("MCP launch command is required.");
      return;
    }
    const save = async () => {
      if (editingToolId) {
        await updateToolCatalogItem(editingToolId, {
          name: toolDraft.name.trim() || id,
          description: toolDraft.description.trim(),
          enabled: toolDraft.enabled,
          area: toolDraft.area.trim(),
          assignedAgentIds,
          command,
          args: toolDraft.args.trim(),
        });
      } else {
        await createToolCatalogItem({
          id,
          name: toolDraft.name.trim() || id,
          description: toolDraft.description.trim(),
          enabled: toolDraft.enabled,
          area: toolDraft.area.trim(),
          assignedAgentIds,
          command,
          args: toolDraft.args.trim(),
        });
      }
      setLocalSetup({
        workspaceRoot: setupDraft.workspaceRoot.trim(),
        serverCommand: setupDraft.serverCommand.trim(),
        serverArgs: setupDraft.serverArgs.trim(),
        autoStart: setupDraft.autoStart,
      });
      setToolModalOpen(false);
      setSuccess(`${editingToolId ? "Updated" : "Added"} tool ${id}.`);
      setToolError(null);
      await loadTools();
    };
    void save().catch((e) => setToolError(e instanceof Error ? e.message : String(e)));
  }, [editingToolId, loadTools, setupDraft, toolDraft, tools]);

  const onToggleTool = useCallback((tool: ToolCatalogItem) => {
    const save = async () => {
      await updateToolCatalogItem(tool.id, { enabled: !tool.enabled });
      await loadTools();
    };
    void save().catch((e) => setToolError(e instanceof Error ? e.message : String(e)));
  }, [loadTools]);

  const onDeleteTool = useCallback((tool: ToolCatalogItem) => {
    if (!window.confirm(`Delete tool ${tool.id}?`)) return;
    const run = async () => {
      await deleteToolCatalogItem(tool.id);
      setSuccess(`Deleted tool ${tool.id}.`);
      await loadTools();
    };
    void run().catch((e) => setToolError(e instanceof Error ? e.message : String(e)));
  }, [loadTools]);

  // Skill authoring moved to the dedicated Skills page.
  void skills;
  void skillModalOpen;
  void skillAdvancedOpen;
  void skillError;
  void skillBusy;
  void skillAiMessages;
  void skillAiBusy;
  void onNewSkill;
  void onEditSkill;
  void onSaveSkill;
  void onToggleSkill;
  void onDeleteSkill;
  void onAskManagerForSkill;

  const connected = !!status?.connected;
  const statusText = useMemo(() => {
    if (!status) return "Loading provider status...";
    if (status.connected) return `Connected (${status.tokenSource ?? "unknown"})`;
    return "Not connected";
  }, [status]);

  return (
    <main className="settings-page">
      <section className="settings-card">
        <header className="settings-head">
          <div>
            <h1>Settings</h1>
            <p>Manage GitHub Copilot authentication used by the manager.</p>
          </div>
          <span className={`settings-status ${connected ? "connected" : "missing"}`}>{statusText}</span>
        </header>

        <div className="settings-grid">
          <div className="settings-field">
            <label>Token source</label>
            <span>{status?.tokenSource ?? "none"}</span>
          </div>
          <div className="settings-field">
            <label>Cached token valid</label>
            <span>{status?.cachedTokenValid ? "yes" : "no"}</span>
          </div>
          <div className="settings-field">
            <label>Cached token expires</label>
            <span>{formatExpiry(status?.cachedTokenExpires)}</span>
          </div>
          <div className="settings-field">
            <label>OAuth stored</label>
            <span>{status?.oauthStored ? "yes" : "no"}</span>
          </div>
          <div className="settings-field">
            <label>Env token available</label>
            <span>{status?.envTokenAvailable ? "yes" : "no"}</span>
          </div>
        </div>

        <div className="settings-actions">
          <button className="settings-btn primary" disabled={busy} onClick={() => void onStartDeviceLogin()}>
            Connect Copilot
          </button>
          <button className="settings-btn" disabled={busy} onClick={() => void onRefresh()}>
            Refresh Auth
          </button>
          <button className="settings-btn danger" disabled={busy} onClick={() => void onLogout()}>
            Logout
          </button>
        </div>

        {err && <div className="settings-message error">{err}</div>}
        {success && <div className="settings-message success">{success}</div>}
      </section>

      <section className="settings-card settings-tools-card">
        <header className="settings-head">
          <div>
            <h1>Capabilities</h1>
            <p>Manage tool capabilities: callable MCP or custom actions with add, edit, disable, remove, and agent assignment.</p>
          </div>
        </header>

        <div className="settings-grid">
          <div className="settings-field">
            <label>Workspace root</label>
            <input
              className="settings-input"
              value={localSetup.workspaceRoot}
              onChange={(e) => setLocalSetup((prev) => ({ ...prev, workspaceRoot: e.target.value }))}
              placeholder="C:\\Users\\matta\\code\\sage-nexus"
            />
          </div>
          <div className="settings-field">
            <label>MCP launch command</label>
            <input
              className="settings-input"
              value={localSetup.serverCommand}
              onChange={(e) => setLocalSetup((prev) => ({ ...prev, serverCommand: e.target.value }))}
              placeholder="node"
            />
          </div>
          <div className="settings-field">
            <label>MCP launch args</label>
            <input
              className="settings-input"
              value={localSetup.serverArgs}
              onChange={(e) => setLocalSetup((prev) => ({ ...prev, serverArgs: e.target.value }))}
              placeholder="services/sage-mcp/dist/index.js"
            />
          </div>
          <div className="settings-field settings-checkbox-field">
            <label>
              <input
                type="checkbox"
                checked={localSetup.autoStart}
                onChange={(e) => setLocalSetup((prev) => ({ ...prev, autoStart: e.target.checked }))}
              />
              Auto-start MCP server on app load
            </label>
          </div>
        </div>

        <div className="settings-actions">
          <button className="settings-btn" onClick={onSaveLocalSetup}>Save Local Setup</button>
          <button className="settings-btn primary" onClick={onOpenToolModal}>Add Tool</button>
        </div>

        <div className="settings-tools-table-wrap">
          {tools.length === 0 ? (
            <div className="settings-empty-tools">No tools found.</div>
          ) : (
            <table className="settings-tools-table">
              <thead>
                <tr>
                  <th>Tool</th>
                  <th>Source</th>
                  <th>Command</th>
                  <th>State</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {tools.map((tool) => (
                  <tr key={tool.id}>
                    <td>
                      <div className="settings-tool-name">{tool.name}</div>
                      <div className="settings-tool-id">{tool.id}</div>
                    </td>
                    <td>{tool.source}</td>
                    <td className="settings-tool-command">{`${tool.command} ${tool.args}`.trim()}</td>
                    <td>{tool.enabled ? "enabled" : "disabled"}</td>
                    <td className="settings-tool-actions">
                      <button className="settings-btn" onClick={() => onEditToolModal(tool)}>
                        Edit
                      </button>
                      <button className="settings-btn" onClick={() => onToggleTool(tool)}>
                        {tool.enabled ? "Disable" : "Enable"}
                      </button>
                      <button className="settings-btn danger" onClick={() => onDeleteTool(tool)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>

      <section className="settings-card settings-tools-card">
        <header className="settings-head">
          <div>
            <h1>Skill Governance</h1>
            <p>Skill creation and release controls now live on the dedicated Skills page.</p>
          </div>
        </header>
      </section>

      {deviceFlow && (
        <div className="settings-modal-backdrop" role="presentation">
          <div className="settings-modal" role="dialog" aria-modal="true" aria-label="GitHub device login">
            <h2>Connect GitHub Copilot</h2>
            <p>
              Continue in GitHub with code <strong>{deviceFlow.userCode}</strong>. We will auto-complete and save auth once you approve.
            </p>
            <div className="settings-modal-code">{deviceFlow.userCode}</div>
            <div className="settings-modal-actions">
              <button className="settings-btn primary" onClick={onOpenDevicePage}>
                Open Login Page
              </button>
              <button className="settings-btn" onClick={onCancelDeviceLogin}>
                Cancel
              </button>
            </div>
            <div className="settings-modal-foot">
              {deviceFlow.status === "completing" ? "Checking approval..." : "Waiting for GitHub approval..."}
            </div>
            {deviceFlow.error && <div className="settings-message error">{deviceFlow.error}</div>}
          </div>
        </div>
      )}

      {toolModalOpen && (
        <div className="settings-modal-backdrop" role="presentation">
          <div className="settings-modal" role="dialog" aria-modal="true" aria-label="MCP tool editor">
            <h2>{editingToolId ? "Edit Tool" : "Add Tool"}</h2>
            <p>Tool metadata is saved in manager and shared across dashboard sessions.</p>
            {!editingToolId && (
              <div className="settings-ai-block">
                <label className="settings-ai-label">Manager Chat</label>
                <div className="settings-ai-chat">
                  {toolAiMessages.length === 0 ? (
                    <div className="settings-ai-empty">Send instructions to the manager. Replies appear here and fields update automatically.</div>
                  ) : (
                    toolAiMessages.map((msg, idx) => (
                      <div key={`${msg.ts}-${idx}`} className={`settings-ai-msg ${msg.role === "user" ? "user" : "manager"}`}>
                        <div className="settings-ai-msg-role">{msg.role === "user" ? "You" : "Manager"}</div>
                        <pre className="settings-ai-msg-text">{msg.text}</pre>
                      </div>
                    ))
                  )}
                </div>
                <textarea
                  className="settings-input settings-ai-textarea"
                  value={toolAiPrompt}
                  onChange={(e) => setToolAiPrompt(e.target.value)}
                  placeholder="Example: add a tool for schema checks in postgres repos, scoped to backend agents."
                />
                <div className="settings-modal-actions">
                  <button className="settings-btn" onClick={onAskManagerForTool} disabled={toolAiBusy}>
                    {toolAiBusy ? "Asking Manager..." : "Ask Manager"}
                  </button>
                </div>
              </div>
            )}
            <details className="settings-detail-expand" open>
              <summary>Tool Details</summary>
              <div className="settings-grid">
              <div className="settings-field">
                <label>Tool id</label>
                <input
                  className="settings-input"
                  value={toolDraft.id}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, id: e.target.value }))}
                  placeholder="file.create"
                  disabled={editingToolId !== null}
                />
              </div>
              <div className="settings-field">
                <label>Display name</label>
                <input
                  className="settings-input"
                  value={toolDraft.name}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, name: e.target.value }))}
                  placeholder="Create File"
                />
              </div>
              <div className="settings-field">
                <label>Command</label>
                <input
                  className="settings-input"
                  value={toolDraft.command}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, command: e.target.value }))}
                  placeholder="node"
                />
              </div>
              <div className="settings-field">
                <label>Args</label>
                <input
                  className="settings-input"
                  value={toolDraft.args}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, args: e.target.value }))}
                  placeholder="tools/create-file.js"
                />
              </div>
              <div className="settings-field">
                <label>Description</label>
                <input
                  className="settings-input"
                  value={toolDraft.description}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, description: e.target.value }))}
                  placeholder="Creates files inside workspace"
                />
              </div>
              <div className="settings-field">
                <label>Area</label>
                <input
                  className="settings-input"
                  value={toolDraft.area}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, area: e.target.value }))}
                  placeholder="filesystem"
                />
              </div>
              <div className="settings-field">
                <label>Assigned agents (comma separated)</label>
                <input
                  className="settings-input"
                  value={toolDraft.assignedAgentIdsText}
                  onChange={(e) => setToolDraft((prev) => ({ ...prev, assignedAgentIdsText: e.target.value }))}
                  placeholder="AGT-backend-dev-agent, AGT-frontend-dev-agent"
                />
              </div>
              <div className="settings-field settings-checkbox-field">
                <label>
                  <input
                    type="checkbox"
                    checked={toolDraft.enabled}
                    onChange={(e) => setToolDraft((prev) => ({ ...prev, enabled: e.target.checked }))}
                  />
                  Enabled on create
                </label>
              </div>

              <div className="settings-field">
                <label>Workspace root (included in add)</label>
                <input
                  className="settings-input"
                  value={setupDraft.workspaceRoot}
                  onChange={(e) => setSetupDraft((prev) => ({ ...prev, workspaceRoot: e.target.value }))}
                  placeholder="C:\\Users\\matta\\code\\sage-nexus"
                />
              </div>
              <div className="settings-field">
                <label>MCP launch command (included in add)</label>
                <input
                  className="settings-input"
                  value={setupDraft.serverCommand}
                  onChange={(e) => setSetupDraft((prev) => ({ ...prev, serverCommand: e.target.value }))}
                  placeholder="node"
                />
              </div>
              <div className="settings-field">
                <label>MCP launch args (included in add)</label>
                <input
                  className="settings-input"
                  value={setupDraft.serverArgs}
                  onChange={(e) => setSetupDraft((prev) => ({ ...prev, serverArgs: e.target.value }))}
                  placeholder="services/sage-mcp/dist/index.js"
                />
              </div>
              <div className="settings-field settings-checkbox-field">
                <label>
                  <input
                    type="checkbox"
                    checked={setupDraft.autoStart}
                    onChange={(e) => setSetupDraft((prev) => ({ ...prev, autoStart: e.target.checked }))}
                  />
                  Auto-start MCP server
                </label>
              </div>
              </div>
            </details>
            <div className="settings-modal-actions">
              <button className="settings-btn primary" onClick={onAddLocalTool}>
                {editingToolId ? "Save Changes" : "Add Tool"}
              </button>
              <button className="settings-btn" onClick={() => setToolModalOpen(false)}>Cancel</button>
            </div>
            {toolError && <div className="settings-message error">{toolError}</div>}
          </div>
        </div>
      )}
    </main>
  );
}

