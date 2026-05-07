// A2A protocol event vocabulary, transported over Redis pub/sub.
// Mirrors services/manager/a2a/types.go. Spec:
// https://a2a-protocol.org/latest/specification/

export const ChannelTasks = "sage:tasks";
export const ChannelEvents = "sage:events";
export const ChannelControl = "sage:control";

export interface ControlMessage {
  taskId: string;
  decision: "continue" | "stop";
  note?: string;
}

export type TaskState =
  | "submitted"
  | "working"
  | "completed"
  | "failed"
  | "canceled"
  | "rejected"
  | "input-required"
  | "auth-required";

export interface Part {
  kind: "text";
  text?: string;
}

export interface A2AMessage {
  kind: "message";
  messageId: string;
  taskId: string;
  contextId?: string;
  role: "user" | "agent";
  parts: Part[];
  metadata?: Record<string, unknown>;
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

export interface Artifact {
  artifactId: string;
  parts: Part[];
}

export interface TaskArtifactUpdateEvent {
  kind: "artifact-update";
  taskId: string;
  contextId?: string;
  artifact: Artifact;
  lastChunk?: boolean;
}

export type A2AEvent = TaskStatusUpdateEvent | TaskArtifactUpdateEvent;

// Construct a Message envelope to publish on sage:tasks.
export function buildMessage(
  taskId: string,
  contextId: string,
  content: string,
  metadata?: Record<string, unknown>,
): A2AMessage {
  return {
    kind: "message",
    messageId: cryptoRandomId(),
    taskId,
    contextId,
    role: "user",
    parts: [{ kind: "text", text: content }],
    metadata,
  };
}

function cryptoRandomId(): string {
  // node 18+ has globalThis.crypto.randomUUID
  const c = (globalThis as { crypto?: { randomUUID?: () => string } }).crypto;
  if (c?.randomUUID) return c.randomUUID();
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

// Pull human-readable text from a TaskStatusUpdateEvent for MCP progress
// notifications. Falls back to the activity name if no text is present.
export function summarizeStatus(evt: TaskStatusUpdateEvent): string {
  if (evt.status.message) {
    const text = evt.status.message.parts
      .filter((p) => p.kind === "text" && p.text)
      .map((p) => p.text)
      .join(" ");
    if (text) return text;
  }
  const activity = evt.metadata?.["activity"];
  if (typeof activity === "string") return activity;
  return evt.status.state;
}

// Extract artifact text from a TaskArtifactUpdateEvent.
export function artifactText(evt: TaskArtifactUpdateEvent): string {
  return evt.artifact.parts
    .filter((p) => p.kind === "text" && p.text)
    .map((p) => p.text ?? "")
    .join("");
}

// Terminal task states close the stream — Sage stops waiting on these.
export function isTerminal(state: TaskState): boolean {
  return state === "completed" || state === "failed" || state === "canceled" || state === "rejected";
}

// Paused states are non-terminal but Sage's tool call should still resolve
// so she can ask the user. The task can be resumed via a control message.
export function isPaused(state: TaskState): boolean {
  return state === "input-required" || state === "auth-required";
}
