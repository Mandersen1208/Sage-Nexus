export interface AgentHandoff {
  agent_id: string;
  role: string;
  model?: string;
  tool_calls?: string[];
  reply?: string;
  timestamp: number;
}

export interface ToolCallLog {
  agent: string;
  tool: string;
  duration_ms: number;
  error: string;
}

export interface AgentResponseLog {
  request_id: string;
  timestamp: number;
  agents: string[];
  model?: string;
  input: unknown;
  output: unknown;
  error?: string;
  tool_calls?: string[];
  tool_errors?: ToolCallLog[];
  handoffs?: AgentHandoff[];
  orchestration_path?: string[];
}

export interface ErrorEvent {
  kind: string;
  agent?: string;
  tool?: string;
  error: string;
  duration_ms?: number;
  request_id?: string;
  timestamp?: number;
}

// ── A2A protocol event vocabulary (Redis bus) ──────────────────────────────

export type TaskState =
  | "submitted"
  | "working"
  | "completed"
  | "failed"
  | "canceled"
  | "rejected"
  | "input-required"
  | "auth-required";

export interface A2APart {
  kind: "text";
  text?: string;
}

export interface A2AMessage {
  kind: "message";
  messageId?: string;
  taskId?: string;
  contextId?: string;
  role: "user" | "agent";
  parts: A2APart[];
}

export interface TaskStatus {
  state: TaskState;
  message?: A2AMessage;
  timestamp: string;
}

export interface TaskStatusUpdateEvent {
  kind: "status-update";
  taskId: string;
  contextId?: string;
  status: TaskStatus;
  metadata?: Record<string, unknown>;
}

export interface TaskArtifactUpdateEvent {
  kind: "artifact-update";
  taskId: string;
  contextId?: string;
  artifact: { artifactId: string; parts: A2APart[] };
  lastChunk?: boolean;
}

export type A2AEvent = TaskStatusUpdateEvent | TaskArtifactUpdateEvent;

// In-memory per-task timeline assembled by the dashboard from sage:events.
export interface TaskTimeline {
  taskId: string;
  contextId?: string;
  state: TaskState;
  events: TaskStatusUpdateEvent[];
  artifact: string;
  liveText?: string;       // volatile streamed model text before final artifact
  startedAt: number;       // unix ms — first event arrival
  lastEventAt: number;     // unix ms — most recent event for liveness pulse
  finalText?: string;      // alias for artifact for downstream consumers
  errorText?: string;      // populated on failed/canceled
}

// Legacy types still used by /agent-response-logs polling path.
export interface RequestEntry {
  request_id: string;
  content?: string;
  status: "pending" | "done" | "error" | "paused";
  timestamp?: number;
  response_timestamp?: number;
  output?: string;
  error?: string;
  agent?: string;
}

export interface MergedRequest extends RequestEntry {
  log?: AgentResponseLog;
  timeline?: TaskTimeline;
}
