import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  createChatSession,
  createNewContextId,
  deleteChatSession,
  getAgentCatalog,
  getChatSession,
  getCopilotProviderStatus,
  getContextId,
  listChatSessions,
  sendChat,
  setContextId as persistContextId,
  stopChat,
  updateChatSessionMode,
} from "./api.js";
import type {
  AgentCatalog,
  AgentCatalogAgent,
  AgentCatalogMode,
  ChatModeSelection,
  CopilotProviderStatus,
  ChatSession as RemoteChatSession,
  ChatTranscriptMessage,
  ChatTranscriptStatus,
} from "./api.js";
import type { StreamState } from "./hooks/useStream.js";
import type { TaskTimeline } from "./types.js";
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';

type ChatRole = "user" | "assistant";
type ChatStatus = "pending" | "done" | "error";

interface ChatAttachment {
  id: string;
  dataUrl: string;
  name: string;
}

interface ChatMessage {
  id: string;
  role: ChatRole;
  text: string;
  createdAt: number;
  status?: ChatStatus;
  taskId?: string;
  attachments?: ChatAttachment[];
}

type ChatSession = RemoteChatSession;

interface ChatPageProps {
  stream: StreamState;
  onOpenTask: (taskId: string) => void;
}

const transcriptPrefix = "sage_chat_transcript:";
const sessionsKey = "sage_chat_sessions";
const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
const MAX_SESSIONS = 100;
const SAGE_PROFILE_IMAGE = "/sage-profile.jpg";
const FALLBACK_MODE: ChatModeSelection = {
  agentMode: "auto",
  label: "Sage Auto",
  description: "Sage Auto sends the turn through the manager/orchestrator.",
};

function transcriptKey(contextId: string): string {
  return `${transcriptPrefix}${contextId}`;
}

function messageId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function attachmentId(): string {
  return `att-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
}

function loadTranscript(contextId: string): ChatMessage[] {
  try {
    const raw = localStorage.getItem(transcriptKey(contextId));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as ChatMessage[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function saveTranscript(contextId: string, messages: ChatMessage[]) {
  try {
    localStorage.setItem(transcriptKey(contextId), JSON.stringify(messages.slice(-80)));
  } catch {
    // localStorage full — trim attachments and retry
    const slim = messages.slice(-80).map((m) => ({ ...m, attachments: undefined }));
    try { localStorage.setItem(transcriptKey(contextId), JSON.stringify(slim)); } catch { /* ignore */ }
  }
}

function sessionTitleFromText(text: string): string {
  const clean = text.replace(/\s+/g, " ").trim();
  if (!clean) return "New chat";
  return clean.length > 46 ? `${clean.slice(0, 43)}...` : clean;
}

function sessionInitial(title: string): string {
  const t = title.trim();
  if (!t || t === "New chat") return "N";
  return t[0]!.toUpperCase();
}

function normalizeSession(raw: unknown): ChatSession | null {
  if (!raw || typeof raw !== "object") return null;
  const item = raw as Partial<ChatSession>;
  if (typeof item.id !== "string" || !item.id) return null;
  return {
    id: item.id,
    title: typeof item.title === "string" && item.title ? item.title : "New chat",
    createdAt: typeof item.createdAt === "number" ? item.createdAt : Date.now(),
    updatedAt: typeof item.updatedAt === "number" ? item.updatedAt : Date.now(),
    messageCount: typeof item.messageCount === "number" ? item.messageCount : undefined,
    agentMode:
      item.agentMode === "auto" || item.agentMode === "solo" || item.agentMode === "launch"
        ? item.agentMode
        : undefined,
    targetAgentId: typeof item.targetAgentId === "string" ? item.targetAgentId : undefined,
    modeLabel: typeof item.modeLabel === "string" ? item.modeLabel : undefined,
  };
}

function normalizeSessions(sessions: ChatSession[]): ChatSession[] {
  const seen = new Set<string>();
  return sessions
    .filter((session) => {
      if (!session.id || seen.has(session.id)) return false;
      seen.add(session.id);
      return true;
    })
    .sort((a, b) => b.updatedAt - a.updatedAt)
    .slice(0, MAX_SESSIONS);
}

function mergeSession(existing: ChatSession | undefined, incoming: ChatSession): ChatSession {
  if (!existing) return incoming;
  const incomingTitle = incoming.title?.trim();
  const existingTitle = existing.title?.trim();
  const title =
    incomingTitle && incomingTitle !== "New chat"
      ? incoming.title
      : existingTitle
        ? existing.title
        : incoming.title || "New chat";
  return {
    id: incoming.id,
    title,
    createdAt: Math.min(existing.createdAt, incoming.createdAt),
    updatedAt: Math.max(existing.updatedAt, incoming.updatedAt),
    messageCount: incoming.messageCount ?? existing.messageCount,
    agentMode: incoming.agentMode ?? existing.agentMode,
    targetAgentId: incoming.targetAgentId ?? existing.targetAgentId,
    modeLabel: incoming.modeLabel ?? existing.modeLabel,
  };
}

function mergeSessions(existing: ChatSession[], incoming: ChatSession[]): ChatSession[] {
  const byId = new Map<string, ChatSession>();
  for (const session of existing) {
    if (session.id) byId.set(session.id, session);
  }
  for (const session of incoming) {
    if (!session.id) continue;
    byId.set(session.id, mergeSession(byId.get(session.id), session));
  }
  return normalizeSessions(Array.from(byId.values()));
}

function transcriptStatus(status: ChatTranscriptStatus | undefined): ChatStatus | undefined {
  if (status === "pending" || status === "done" || status === "error") return status;
  return undefined;
}

function fromRemoteMessage(msg: ChatTranscriptMessage): ChatMessage {
  return {
    id: msg.id || messageId(),
    role: msg.role === "assistant" ? "assistant" : "user",
    text: msg.text ?? "",
    createdAt: typeof msg.createdAt === "number" ? msg.createdAt : Date.now(),
    status: transcriptStatus(msg.status),
    taskId: msg.taskId,
  };
}

function selectionForCatalogMode(agent: AgentCatalogAgent, mode: AgentCatalogMode): ChatModeSelection {
  return {
    agentMode: mode.id,
    targetAgentId: mode.id === "auto" ? undefined : agent.id,
    label: mode.label,
    description: mode.description,
  };
}

function selectionKey(selection: ChatModeSelection): string {
  return `${selection.agentMode}:${selection.targetAgentId ?? ""}`;
}

function selectionFromSession(
  session: ChatSession | undefined,
  catalog: AgentCatalog | null,
): ChatModeSelection {
  if (!catalog) return FALLBACK_MODE;
  if (!session?.agentMode) return catalog.defaultMode;
  if (session.agentMode === "auto") return catalog.defaultMode;
  for (const agent of catalog.agents) {
    if (agent.id !== session.targetAgentId) continue;
    const mode = agent.modes.find((item) => item.id === session.agentMode && item.enabled);
    if (mode) return selectionForCatalogMode(agent, mode);
  }
  if (session.modeLabel) {
    return {
      agentMode: session.agentMode,
      targetAgentId: session.targetAgentId,
      label: session.modeLabel,
    };
  }
  return catalog.defaultMode;
}

function selectionFromCatalog(
  catalog: AgentCatalog | null,
  selection: ChatModeSelection | null,
): ChatModeSelection | null {
  if (!catalog || !selection) return selection;
  if (selection.agentMode === "auto") return catalog.defaultMode;
  for (const agent of catalog.agents) {
    if (agent.id !== selection.targetAgentId) continue;
    const mode = agent.modes.find((item) => item.id === selection.agentMode && item.enabled);
    if (mode) return selectionForCatalogMode(agent, mode);
  }
  return selection;
}

function loadSessions(currentContextId: string): ChatSession[] {
  let sessions: ChatSession[] = [];
  try {
    const raw = localStorage.getItem(sessionsKey);
    const parsed = raw ? JSON.parse(raw) as unknown[] : [];
    if (Array.isArray(parsed)) {
      sessions = parsed
        .map(normalizeSession)
        .filter((item): item is ChatSession => item !== null);
    }
  } catch {
    sessions = [];
  }

  if (!sessions.some((s) => s.id === currentContextId)) {
    const messages = loadTranscript(currentContextId);
    const firstUser = messages.find((m) => m.role === "user")?.text;
    sessions.unshift({
      id: currentContextId,
      title: firstUser ? sessionTitleFromText(firstUser) : "New chat",
      createdAt: Date.now(),
      updatedAt: Date.now(),
    });
  }

  return normalizeSessions(sessions);
}

function saveSessions(sessions: ChatSession[]) {
  localStorage.setItem(sessionsKey, JSON.stringify(sessions.slice(0, MAX_SESSIONS)));
}

function taskText(task: TaskTimeline): string {
  return task.finalText || task.artifact || "";
}

function taskError(task: TaskTimeline): string {
  return task.errorText ?? "Task failed";
}

function isLiveTask(task: TaskTimeline): boolean {
  return (
    task.state !== "completed" &&
    task.state !== "failed" &&
    task.state !== "canceled" &&
    task.state !== "rejected"
  );
}

function liveStreamAgent(task: TaskTimeline): string {
  for (let index = task.events.length - 1; index >= 0; index -= 1) {
    const evt = task.events[index];
    if (evt?.metadata?.activity !== "model_delta") continue;
    const agent = evt.metadata.agent;
    if (typeof agent === "string" && agent) return agent;
  }
  return "agent";
}

function relativeTime(ms: number): string {
  const diff = Math.max(0, Date.now() - ms);
  if (diff < 60_000) return "now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
  return `${Math.floor(diff / 86_400_000)}d`;
}

function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(new Error("Failed to read file"));
    reader.readAsDataURL(file);
  });
}

export default function ChatPage({ stream, onOpenTask }: ChatPageProps) {
  const initialContextId = useMemo(() => getContextId(), []);
  const [contextId, setContextId] = useState(initialContextId);
  const [sessions, setSessions] = useState<ChatSession[]>(() => loadSessions(initialContextId));
  const [messages, setMessages] = useState<ChatMessage[]>(() => loadTranscript(initialContextId));
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [busy, setBusy] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [catalog, setCatalog] = useState<AgentCatalog | null>(null);
  const [catalogErr, setCatalogErr] = useState<string | null>(null);
  const [providerStatus, setProviderStatus] = useState<CopilotProviderStatus | null>(null);
  const [modeMenuOpen, setModeMenuOpen] = useState(false);
  const [oneShotMode, setOneShotMode] = useState<ChatModeSelection | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const loadTokenRef = useRef(0);

  const contextTasks = useMemo(
    () => Object.values(stream.tasks).filter((task) => task.contextId === contextId),
    [contextId, stream.tasks],
  );
  const logsByTaskId = useMemo(
    () => new Map(stream.logs.map((log) => [log.request_id, log])),
    [stream.logs],
  );
  const activeTaskIds = useMemo(
    () =>
      contextTasks
        .filter(isLiveTask)
        .map((task) => task.taskId),
    [contextTasks],
  );
  const activeCount = activeTaskIds.length;
  const liveStreamTask = useMemo(
    () =>
      contextTasks
        .filter((task) => isLiveTask(task) && Boolean(task.liveText))
        .sort((a, b) => b.lastEventAt - a.lastEventAt)[0],
    [contextTasks],
  );

  const pullProviderStatus = useCallback(async () => {
    try {
      const status = await getCopilotProviderStatus();
      setProviderStatus(status);
    } catch {
      setProviderStatus(null);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function loadCatalog() {
      try {
        const next = await getAgentCatalog();
        if (cancelled) return;
        setCatalog(next);
        setCatalogErr(null);
      } catch (e) {
        if (cancelled) return;
        setCatalog(null);
        setCatalogErr(e instanceof Error ? e.message : String(e));
      }
    }

    void loadCatalog();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function loadProviderStatus() {
      await pullProviderStatus();
      if (cancelled) return;
    }

    void loadProviderStatus();
    const id = window.setInterval(() => void loadProviderStatus(), 30000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [pullProviderStatus]);

  useEffect(() => {
    let cancelled = false;

    async function syncCentralSessions() {
      try {
        let remote = normalizeSessions(await listChatSessions());
        let selected = remote.find((session) => session.id === initialContextId) ?? remote[0];

        if (!selected) {
          selected = await createChatSession(initialContextId, "New chat");
          remote = [selected];
        }

        const detail = await getChatSession(selected.id).catch(() => ({
          session: selected,
          messages: [] as ChatTranscriptMessage[],
        }));
        if (cancelled) return;

        const syncedMessages = detail.messages.map(fromRemoteMessage);
        persistContextId(detail.session.id);
        setContextId(detail.session.id);
        setSessions((prev) =>
          mergeSessions(prev, [detail.session, ...remote.filter((s) => s.id !== detail.session.id)]),
        );
        setMessages(syncedMessages);
        saveTranscript(detail.session.id, syncedMessages);
        setErr(null);
      } catch (e) {
        if (cancelled) return;
        setErr(`Session sync unavailable: ${e instanceof Error ? e.message : String(e)}`);
      }
    }

    void syncCentralSessions();
    return () => {
      cancelled = true;
    };
  }, [initialContextId]);

  useEffect(() => { saveTranscript(contextId, messages); }, [contextId, messages]);
  useEffect(() => { saveSessions(sessions); }, [sessions]);
  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        const remote = normalizeSessions(await listChatSessions());
        if (!cancelled && remote.length > 0) {
          setSessions((prev) => mergeSessions(prev, remote));
        }
      } catch {
        // The local cache remains available if the manager is rebuilding.
      }
    };
    const timer = window.setInterval(() => void refresh(), 15_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages]);

  useEffect(() => {
    if (!sessionsOpen) return;
    const close = (e: KeyboardEvent) => { if (e.key === "Escape") setSessionsOpen(false); };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [sessionsOpen]);

  useEffect(() => {
    if (!modeMenuOpen) return;
    const close = (e: KeyboardEvent) => { if (e.key === "Escape") setModeMenuOpen(false); };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [modeMenuOpen]);

  useEffect(() => {
    setMessages((prev) => {
      let changed = false;
      const next = prev.map((msg) => {
        if (msg.role !== "assistant" || !msg.taskId) return msg;
        const task = stream.tasks[msg.taskId];
        const log = logsByTaskId.get(msg.taskId);
        if (!task && log) {
          const text = log.error ? log.error : formatLogOutput(log.output);
          const status: ChatStatus = log.error ? "error" : "done";
          if (msg.text === text && msg.status === status) return msg;
          changed = true;
          return { ...msg, text, status };
        }
        if (!task) return msg;
        if (task.state === "failed" || task.state === "canceled" || task.state === "rejected") {
          const text = taskError(task);
          if (msg.text === text && msg.status === "error") return msg;
          changed = true;
          return { ...msg, text, status: "error" as ChatStatus };
        }
        const text = taskText(task);
        if (task.state === "completed" && text) {
          if (msg.text === text && msg.status === "done") return msg;
          changed = true;
          return { ...msg, text, status: "done" as ChatStatus };
        }
        if (text && msg.text !== text) {
          changed = true;
          return { ...msg, text, status: "pending" as ChatStatus };
        }
        return msg;
      });
      return changed ? next : prev;
    });
  }, [logsByTaskId, stream.tasks]);

  const touchSession = useCallback((id: string, title?: string) => {
    setSessions((prev) => {
      const now = Date.now();
      const next = prev.map((s) =>
        s.id === id
          ? { ...s, title: title && s.title === "New chat" ? title : s.title, updatedAt: now }
          : s,
      );
      if (!next.some((s) => s.id === id)) {
        next.push({ id, title: title ?? "New chat", createdAt: now, updatedAt: now });
      }
      return normalizeSessions(next);
    });
  }, []);

  const switchSession = useCallback(
    async (id: string) => {
      const loadToken = ++loadTokenRef.current;
      persistContextId(id);
      setContextId(id);
      setMessages(loadTranscript(id));
      setText("");
      setAttachments([]);
      setErr(null);
      setSessionsOpen(false);
      touchSession(id);
      try {
        const detail = await getChatSession(id);
        if (loadTokenRef.current !== loadToken) return;
        const syncedMessages = detail.messages.map(fromRemoteMessage);
        setSessions((prev) =>
          mergeSessions(prev, [detail.session]),
        );
        setMessages(syncedMessages);
        saveTranscript(id, syncedMessages);
      } catch (e) {
        if (loadTokenRef.current === loadToken) {
          setErr(`Session load failed: ${e instanceof Error ? e.message : String(e)}`);
        }
      }
    },
    [touchSession],
  );

  const onNewChat = useCallback(async () => {
    let nextSession: ChatSession;
    try {
      nextSession = await createChatSession(undefined, "New chat");
      setErr(null);
    } catch (e) {
      nextSession = {
        id: createNewContextId(),
        title: "New chat",
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };
      setErr(`Session create failed: ${e instanceof Error ? e.message : String(e)}`);
    }
    const nextId = nextSession.id;
    persistContextId(nextId);
    setSessions((prev) => mergeSessions(prev, [nextSession]));
    try {
      const remote = normalizeSessions(await listChatSessions());
      setSessions((prev) => mergeSessions(prev, remote));
    } catch {
      // The new local selection is already present; periodic sync will retry.
    }
    setContextId(nextId);
    setMessages([]);
    setText("");
    setAttachments([]);
    setSessionsOpen(false);
  }, []);

  const onClearChat = useCallback(async () => {
    localStorage.removeItem(transcriptKey(contextId));
    setMessages([]);
    setErr(null);
    try {
      await deleteChatSession(contextId);
      const session = await createChatSession(contextId, "New chat");
      setSessions((prev) =>
        mergeSessions(prev.filter((s) => s.id !== contextId), [session]),
      );
    } catch (e) {
      setErr(`Clear failed: ${e instanceof Error ? e.message : String(e)}`);
      setSessions((prev) =>
        prev.map((s) =>
          s.id === contextId ? { ...s, title: "New chat", updatedAt: Date.now() } : s,
        ),
      );
    }
  }, [contextId]);

  const onDeleteSession = useCallback(
    async (id: string) => {
      const session = sessions.find((s) => s.id === id);
      const label = session?.title && session.title !== "New chat" ? session.title : "this chat";
      if (!window.confirm(`Delete ${label}?`)) return;

      setErr(null);
      try {
        await deleteChatSession(id);
      } catch (e) {
        setErr(`Delete failed: ${e instanceof Error ? e.message : String(e)}`);
        return;
      }

      localStorage.removeItem(transcriptKey(id));
      const remaining = sessions.filter((s) => s.id !== id);
      if (id === contextId) {
        let next = remaining[0];
        if (!next) {
          try {
            next = await createChatSession(undefined, "New chat");
          } catch {
            next = {
              id: createNewContextId(),
              title: "New chat",
              createdAt: Date.now(),
              updatedAt: Date.now(),
            };
          }
        }
        persistContextId(next.id);
        setContextId(next.id);
        setMessages(loadTranscript(next.id));
        setText("");
        setAttachments([]);
        setErr(null);
        setSessionsOpen(false);
        setSessions((prev) =>
          normalizeSessions([
            ...(remaining.length > 0 ? remaining : [next]),
            ...prev.filter((s) => s.id !== id),
          ]),
        );
        try {
          const detail = await getChatSession(next.id);
          const syncedMessages = detail.messages.map(fromRemoteMessage);
          setMessages(syncedMessages);
          saveTranscript(next.id, syncedMessages);
          setSessions((prev) =>
            mergeSessions(prev, [detail.session]),
          );
        } catch {
          // Local fallback is already selected.
        }
        return;
      }
      setSessions(normalizeSessions(remaining));
    },
    [contextId, sessions],
  );

  const handleFiles = useCallback(async (files: File[]) => {
    const imageFiles = files.filter(
      (f) => f.type.startsWith("image/") && f.size <= MAX_ATTACHMENT_BYTES,
    );
    if (imageFiles.length === 0) return;
    const newAtts: ChatAttachment[] = await Promise.all(
      imageFiles.map(async (f) => ({
        id: attachmentId(),
        dataUrl: await fileToDataUrl(f),
        name: f.name,
      })),
    );
    setAttachments((prev) => [...prev, ...newAtts].slice(0, 6));
  }, []);

  const onFileSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? []);
      void handleFiles(files);
      e.target.value = "";
    },
    [handleFiles],
  );

  const removeAttachment = useCallback((id: string) => {
    setAttachments((prev) => prev.filter((a) => a.id !== id));
  }, []);

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    transcriptRef.current?.classList.add("drag-over");
  }, []);

  const onDragLeave = useCallback((e: React.DragEvent) => {
    if (!transcriptRef.current?.contains(e.relatedTarget as Node)) {
      transcriptRef.current?.classList.remove("drag-over");
    }
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      transcriptRef.current?.classList.remove("drag-over");
      const files = Array.from(e.dataTransfer.files);
      void handleFiles(files);
    },
    [handleFiles],
  );

  const currentSession = sessions.find((s) => s.id === contextId);
  const stickyMode = useMemo(
    () => selectionFromSession(currentSession, catalog),
    [catalog, currentSession],
  );
  const activeMode = useMemo(
    () => selectionFromCatalog(catalog, oneShotMode) ?? stickyMode,
    [catalog, oneShotMode, stickyMode],
  );

  const onSend = useCallback(async () => {
    const content = text.trim();
    const hasAtts = attachments.length > 0;
    if ((!content && !hasAtts) || busy) return;

    const messageText = content || (hasAtts ? "[Image shared]" : "");

    const userMessage: ChatMessage = {
      id: messageId(),
      role: "user",
      text: messageText,
      createdAt: Date.now(),
      attachments: hasAtts ? [...attachments] : undefined,
    };

    setMessages((prev) => [...prev, userMessage]);
    setText("");
    setAttachments([]);
    setBusy(true);
    setErr(null);
    touchSession(contextId, sessionTitleFromText(messageText));

    try {
      const res = await sendChat(messageText, contextId, activeMode);
      const assistantMessage: ChatMessage = {
        id: messageId(),
        role: "assistant",
        text: "",
        createdAt: Date.now(),
        status: "pending",
        taskId: res.taskId,
      };
      setMessages((prev) => [...prev, assistantMessage]);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setErr(message);
      setMessages((prev) => [
        ...prev,
        { id: messageId(), role: "assistant", text: message, createdAt: Date.now(), status: "error" },
      ]);
    } finally {
      setOneShotMode(null);
      setBusy(false);
    }
  }, [activeMode, busy, contextId, text, attachments, touchSession]);

  const onStop = useCallback(async () => {
    if (activeTaskIds.length === 0 || stopping) return;
    setStopping(true);
    setErr(null);
    try {
      await Promise.all(activeTaskIds.map((taskId) => stopChat(taskId)));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setStopping(false);
    }
  }, [activeTaskIds, stopping]);

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        void onSend();
      }
    },
    [onSend],
  );

  const setSessionMode = useCallback(
    async (selection: ChatModeSelection) => {
      setModeMenuOpen(false);
      setErr(null);
      try {
        const updated = await updateChatSessionMode(contextId, selection);
        setSessions((prev) => mergeSessions(prev, [updated]));
        setOneShotMode(null);
      } catch (e) {
        setErr(`Mode update failed: ${e instanceof Error ? e.message : String(e)}`);
      }
    },
    [contextId],
  );

  const useModeOnce = useCallback((selection: ChatModeSelection) => {
    setOneShotMode(selection);
    setModeMenuOpen(false);
  }, []);

  return (
    <main className="chat-page">
      <div className="chat-workspace">
        <button
          className={`session-backdrop ${sessionsOpen ? "open" : ""}`}
          aria-label="Close chat sessions"
          tabIndex={sessionsOpen ? 0 : -1}
          onClick={() => setSessionsOpen(false)}
        />

        <aside className={`session-rail ${sessionsOpen ? "open" : ""}`} aria-label="Chat sessions">
          <div className="session-rail-head">
            <div>
              <div className="session-kicker">Study Threads</div>
              <div className="session-count">{sessions.length} saved notebooks</div>
            </div>
            <div className="session-rail-actions">
              <button className="session-new-btn" onClick={() => void onNewChat()}>
                New Study
              </button>
              <button
                className="session-close-btn mobile-only"
                onClick={() => setSessionsOpen(false)}
              >
                Close
              </button>
            </div>
          </div>

          <div className="session-list">
            {sessions.map((session) => (
              <div
                key={session.id}
                className={`session-item ${session.id === contextId ? "active" : ""}`}
              >
                <button className="session-main" onClick={() => void switchSession(session.id)}>
                  <div className="session-avatar">{sessionInitial(session.title)}</div>
                  <div className="session-info">
                    <span className="session-title">{session.title}</span>
                    <span className="session-meta">
                      {relativeTime(session.updatedAt)}
                      {typeof session.messageCount === "number" ? ` - ${session.messageCount} msgs` : ""}
                    </span>
                  </div>
                </button>
                <button
                  className="session-delete-btn"
                  aria-label={`Delete ${session.title}`}
                  onClick={() => void onDeleteSession(session.id)}
                >
                  Delete
                </button>
              </div>
            ))}
          </div>
        </aside>

        <section className="chat-shell">
          <div className="chat-head">
            <div className="sage-presence">
              <div className="sage-presence-portrait" aria-hidden="true" />
              <div className="chat-heading">
                <div className="chat-kicker">Sage Nexus Study</div>
                <div className="chat-title">Sage</div>
                <div className="chat-subtitle">
                  <span>{currentSession?.title ?? "New chat"}</span>
                  {activeCount > 0 && <span>{activeCount} active</span>}
                  <span>{stream.connected ? "live" : "offline"}</span>
                  <span className={providerStatus?.connected ? "provider-pill connected" : "provider-pill missing"}>
                    Copilot {providerStatus?.connected ? providerStatus.tokenSource ?? "connected" : "missing auth"}
                  </span>
                </div>
              </div>
            </div>
            <div className="chat-head-actions">
              <button
                className="chat-secondary-btn chat-sessions-btn mobile-only"
                aria-expanded={sessionsOpen}
                onClick={() => setSessionsOpen(true)}
              >
                Sessions
              </button>
              <button className="chat-secondary-btn" onClick={() => void onClearChat()}>
                Clear
              </button>
              <button
                className="chat-secondary-btn danger"
                onClick={() => void onDeleteSession(contextId)}
              >
                Delete
              </button>
            </div>
          </div>

          <div
            ref={transcriptRef}
            className="chat-transcript"
            onDragOver={onDragOver}
            onDragLeave={onDragLeave}
            onDrop={onDrop}
          >
            {messages.length === 0 && (
              <div className="chat-empty">
                <div className="chat-empty-art" aria-hidden="true">
                  <div className="study-card study-card-primary">
                    <span>Focus</span>
                    <strong>Research Sprint</strong>
                  </div>
                  <div className="study-card study-card-secondary">
                    <span>Sources</span>
                    <strong>Notes + Images</strong>
                  </div>
                </div>
                <div className="chat-empty-copy">
                  <div>Open a study session with Sage.</div>
                  <div>Ask for summaries, compare notes, drop images, or turn a rough question into a structured study plan.</div>
                  <div className="study-prompts" aria-label="Suggested study prompts">
                    <span>Summarize</span>
                    <span>Explain</span>
                    <span>Quiz me</span>
                  </div>
                </div>
              </div>
            )}
            {messages.map((msg) => (
              <ChatBubble
                key={msg.id}
                message={msg}
                onOpenTask={msg.taskId ? () => onOpenTask(msg.taskId ?? "") : undefined}
              />
            ))}
            <div ref={bottomRef} />
          </div>

          {liveStreamTask?.liveText && (
            <div className="chat-stream-box" role="status" aria-live="polite">
              <div className="chat-stream-box-head">
                <span>Live output</span>
                <small>{liveStreamAgent(liveStreamTask)}</small>
              </div>
              <div className="chat-stream-box-body">
                <MarkdownMessage text={liveStreamTask.liveText} />
              </div>
            </div>
          )}

          <div className="chat-composer">
            <div className="compose-mode-row">
              <ModeSelector
                catalog={catalog}
                activeMode={activeMode}
                stickyMode={stickyMode}
                oneShotMode={oneShotMode}
                catalogError={catalogErr}
                open={modeMenuOpen}
                placement="composer"
                onToggle={() => setModeMenuOpen((value) => !value)}
                onSetSession={(selection) => void setSessionMode(selection)}
                onUseOnce={useModeOnce}
              />
            </div>

            {attachments.length > 0 && (
              <div className="compose-attachments">
                {attachments.map((att) => (
                  <div key={att.id} className="compose-attachment">
                    <img src={att.dataUrl} alt={att.name} />
                    <button
                      className="compose-attachment-remove"
                      onClick={() => removeAttachment(att.id)}
                      aria-label={`Remove ${att.name}`}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}

            <textarea
              className="chat-compose-input"
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder="Ask Sage what to study next..."
              rows={3}
              disabled={busy}
            />

            <div className="compose-toolbar">
              <div className="compose-media">
                <button
                  className="media-btn"
                  title="Attach image or GIF (max 5 MB)"
                  onClick={() => fileInputRef.current?.click()}
                >
                  📎
                </button>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/*"
                  multiple
                  hidden
                  onChange={onFileSelect}
                />
              </div>
              <div className="compose-actions">
                <button
                  className="chat-stop-btn"
                  onClick={onStop}
                  disabled={stopping || activeTaskIds.length === 0}
                >
                  {stopping ? "Stopping…" : "Stop"}
                </button>
                <button
                  className="chat-send-btn"
                  onClick={onSend}
                  disabled={busy || (!text.trim() && attachments.length === 0)}
                >
                  {busy ? "Sending…" : "Send"}
                </button>
              </div>
            </div>
          </div>

          {err && <div className="chat-page-error">{err}</div>}
        </section>
      </div>
    </main>
  );
}


function ModeSelector({
  catalog,
  activeMode,
  stickyMode,
  oneShotMode,
  catalogError,
  open,
  placement = "composer",
  onToggle,
  onSetSession,
  onUseOnce,
}: {
  catalog: AgentCatalog | null;
  activeMode: ChatModeSelection;
  stickyMode: ChatModeSelection;
  oneShotMode: ChatModeSelection | null;
  catalogError: string | null;
  open: boolean;
  placement?: "header" | "composer";
  onToggle: () => void;
  onSetSession: (selection: ChatModeSelection) => void;
  onUseOnce: (selection: ChatModeSelection) => void;
}) {
  const activeKey = selectionKey(activeMode);
  const stickyKey = selectionKey(stickyMode);
  const oneShotKey = oneShotMode ? selectionKey(oneShotMode) : "";

  return (
    <div className={`mode-selector ${placement === "header" ? "header-mode" : "composer-mode"}`}>
      <button
        className={`mode-chip ${oneShotMode ? "oneshot" : ""}`}
        onClick={onToggle}
        aria-expanded={open}
        aria-label="Change agent mode"
        title={activeMode.description || activeMode.label}
      >
        <span className="mode-chip-copy">
          <small>Agent mode</small>
          <span>{activeMode.label}</span>
        </span>
        {oneShotMode && <em>once</em>}
        <b aria-hidden="true">v</b>
      </button>

      {open && (
        <div className={`mode-menu ${placement === "header" ? "header-menu" : ""}`} role="menu">
          {catalog ? (
            catalog.agents.map((agent) => (
              <div key={agent.id} className="mode-agent-group">
                <div className="mode-agent-head">
                  <span>{agent.displayName}</span>
                  {agent.authority && <small>{agent.authority}</small>}
                </div>
                <div className="mode-agent-options">
                  {agent.modes.map((mode) => {
                    const selection = selectionForCatalogMode(agent, mode);
                    const key = selectionKey(selection);
                    return (
                      <div
                        key={`${agent.id}-${mode.id}`}
                        className={`mode-option ${mode.enabled ? "" : "disabled"} ${
                          key === activeKey ? "active" : ""
                        }`}
                      >
                        <div className="mode-option-copy">
                          <span>{mode.label}</span>
                          <small>{mode.enabled ? mode.description : mode.disabledReason}</small>
                        </div>
                        <div className="mode-option-actions">
                          <button
                            disabled={!mode.enabled}
                            className={key === stickyKey ? "selected" : ""}
                            onClick={() => onSetSession(selection)}
                          >
                            Set
                          </button>
                          <button
                            disabled={!mode.enabled}
                            className={key === oneShotKey ? "selected" : ""}
                            onClick={() => onUseOnce(selection)}
                          >
                            Once
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))
          ) : (
            <div className="mode-catalog-fallback">
              <span>{FALLBACK_MODE.label}</span>
              <small>{catalogError ? `Catalog unavailable: ${catalogError}` : "Catalog unavailable"}</small>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function SageAvatar() {
  return (
    <div
      className="bubble-avatar sage-avatar-img"
      aria-label="Sage"
      role="img"
      style={{ backgroundImage: `url(${SAGE_PROFILE_IMAGE})` }}
    >
      <span>S</span>
    </div>
  );
}

async function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  document.body.appendChild(textarea);
  textarea.select();
  try {
    const copied = document.execCommand("copy");
    if (!copied) throw new Error("Copy command failed");
  } finally {
    document.body.removeChild(textarea);
  }
}

function CodeBlock({ language, code }: { language: string; code: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void copyToClipboard(code)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      })
      .catch(() => setCopied(false));
  };
  return (
    <div className="code-block-wrapper">
      <div className="code-block-header">
        {language && <span className="code-block-lang">{language}</span>}
        <button className="code-block-copy" onClick={copy} title="Copy code">
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <SyntaxHighlighter
        style={oneDark}
        language={language || 'text'}
        PreTag="div"
        customStyle={{ margin: 0, borderRadius: '0 0 8px 8px', background: 'transparent' }}
      >
        {code}
      </SyntaxHighlighter>
    </div>
  );
}

function MarkdownMessage({ text }: { text: string }) {
  return (
    <div className="prose">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code(props) {
            const { node: _node, className, children, ...rest } = props as typeof props & { node?: unknown };
            const match = /language-(\w+)/.exec(className ?? '');
            const codeStr = String(children).replace(/\n$/, '');
            if (match || codeStr.includes('\n')) {
              return <CodeBlock language={match?.[1] ?? ''} code={codeStr} />;
            }
            return <code className="inline-code" {...rest}>{children}</code>;
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function formatLogOutput(output: unknown): string {
  if (typeof output === "string") return output;
  if (output == null) return "";
  return JSON.stringify(output, null, 2);
}

function ChatBubble({
  message,
  onOpenTask,
}: {
  message: ChatMessage;
  onOpenTask?: () => void;
}) {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const isSage = message.role === "assistant";
  const label = isSage ? "Sage" : "You";
  const showTyping = isSage && message.status === "pending" && !message.text;
  const text = message.text || (showTyping ? "" : "");
  const canCopy = message.text.trim().length > 0;

  const copyMsg = () => {
    if (!canCopy) return;
    void copyToClipboard(message.text)
      .then(() => {
        setCopyState("copied");
        setTimeout(() => setCopyState("idle"), 1800);
      })
      .catch(() => {
        setCopyState("failed");
        setTimeout(() => setCopyState("idle"), 2200);
      });
  };

  return (
    <div className={`chat-bubble-row ${message.role}`}>
      {isSage && <SageAvatar />}
      {!isSage && <div className="bubble-avatar bubble-avatar-user">M</div>}

      <div className={`chat-bubble ${message.role} ${message.status ?? ""}`}>
        <div className="chat-bubble-meta">
          <span>{label}</span>
          <span>
            {new Date(message.createdAt).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </span>
          {message.status === "error" && <span>error</span>}
          {message.taskId && (
            <button className="chat-task-link" onClick={onOpenTask}>
              task ↗
            </button>
          )}
          {canCopy && (
            <button
              className={`msg-copy-btn ${copyState !== "idle" ? "active" : ""}`}
              onClick={copyMsg}
              title="Copy message"
              aria-label={`Copy ${label} message`}
            >
              {copyState === "copied" ? "Copied" : copyState === "failed" ? "Failed" : "Copy"}
            </button>
          )}
        </div>

        {showTyping ? (
          <div className="typing-indicator">
            <span />
            <span />
            <span />
          </div>
        ) : (
          <div className="chat-bubble-text">
            {isSage ? <MarkdownMessage text={text} /> : text}
          </div>
        )}

        {message.attachments?.map((att) => (
          <img
            key={att.id}
            className="bubble-image"
            src={att.dataUrl}
            alt={att.name}
            onClick={() => window.open(att.dataUrl, "_blank")}
          />
        ))}
      </div>
    </div>
  );
}
