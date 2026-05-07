import { useEffect, useState, useCallback } from "react";
import { MANAGER_URL } from "../config.js";
import type {
  AgentResponseLog,
  ErrorEvent,
  TaskTimeline,
  TaskStatusUpdateEvent,
  TaskArtifactUpdateEvent,
  A2AEvent,
} from "../types.js";

export interface StreamState {
  connected: boolean;
  tasks: Record<string, TaskTimeline>;
  logs: AgentResponseLog[];
  errors: ErrorEvent[];
}

function isStatusUpdate(evt: A2AEvent): evt is TaskStatusUpdateEvent {
  return evt.kind === "status-update";
}

function isArtifactUpdate(evt: A2AEvent): evt is TaskArtifactUpdateEvent {
  return evt.kind === "artifact-update";
}

function artifactText(evt: TaskArtifactUpdateEvent): string {
  return evt.artifact.parts
    .filter((p) => p.kind === "text" && typeof p.text === "string")
    .map((p) => p.text ?? "")
    .join("");
}

function statusMessageText(evt: TaskStatusUpdateEvent): string {
  return (
    evt.status.message?.parts
      ?.filter((p) => p.kind === "text" && typeof p.text === "string")
      ?.map((p) => p.text ?? "")
      ?.join(" ") ?? ""
  );
}

function isTerminalState(state: TaskTimeline["state"]): boolean {
  return (
    state === "completed" ||
    state === "failed" ||
    state === "canceled" ||
    state === "rejected"
  );
}

function nextTimelineState(
  previous: TaskTimeline["state"],
  incoming: TaskTimeline["state"],
): TaskTimeline["state"] {
  if (isTerminalState(previous) && !isTerminalState(incoming)) {
    return previous;
  }
  return incoming;
}

export function useStream(): StreamState {
  const [state, setState] = useState<StreamState>({
    connected: false,
    tasks: {},
    logs: [],
    errors: [],
  });

  const fetchLogs = useCallback(async () => {
    try {
      const res = await fetch(`${MANAGER_URL}/agent-response-logs`);
      if (!res.ok) return;
      const raw: unknown = await res.json();
      const logs = Array.isArray(raw) ? (raw as AgentResponseLog[]) : [];
      setState((prev) => ({ ...prev, logs }));
    } catch {
      // manager might not be reachable yet
    }
  }, []);

  useEffect(() => {
    fetchLogs();
    const poll = setInterval(fetchLogs, 5_000);
    return () => clearInterval(poll);
  }, [fetchLogs]);

  useEffect(() => {
    const es = new EventSource(`${MANAGER_URL}/stream`);

    es.onopen = () => setState((prev) => ({ ...prev, connected: true }));
    es.onerror = () => setState((prev) => ({ ...prev, connected: false }));

    es.onmessage = (event: MessageEvent<string>) => {
      try {
        const envelope = JSON.parse(event.data) as {
          channel: string;
          payload: string;
        };
        const data = JSON.parse(envelope.payload) as unknown;

        setState((prev) => {
          // Cross-cutting error feed.
          if (envelope.channel === "sage:errors") {
            return {
              ...prev,
              errors: [data as ErrorEvent, ...prev.errors].slice(0, 200),
            };
          }

          // A2A bus — sage:events carries status-update or artifact-update.
          if (envelope.channel === "sage:events") {
            const evt = data as A2AEvent;
            const taskId = (evt as { taskId?: string }).taskId;
            if (!taskId) return prev;

            const now = Date.now();
            const existing: TaskTimeline = prev.tasks[taskId] ?? {
              taskId,
              contextId: (evt as { contextId?: string }).contextId,
              state: "submitted",
              events: [],
              artifact: "",
              startedAt: now,
              lastEventAt: now,
            };

            if (isStatusUpdate(evt)) {
              const nextState = nextTimelineState(existing.state, evt.status.state);
              const next: TaskTimeline = {
                ...existing,
                state: nextState,
                events: [...existing.events, evt],
                lastEventAt: now,
              };
              if (evt.status.state === "failed" || evt.status.state === "canceled" || evt.status.state === "rejected") {
                next.errorText = statusMessageText(evt) || `task ${evt.status.state}`;
              }
              if (nextState === "completed" && next.artifact) {
                next.finalText = next.artifact;
              }
              return { ...prev, tasks: { ...prev.tasks, [taskId]: next } };
            }

            if (isArtifactUpdate(evt)) {
              const newArtifact = existing.artifact + artifactText(evt);
              return {
                ...prev,
                tasks: {
                  ...prev.tasks,
                  [taskId]: {
                    ...existing,
                    artifact: newArtifact,
                    finalText: newArtifact,
                    lastEventAt: now,
                  },
                },
              };
            }
          }

          return prev;
        });
      } catch {
        // ignore malformed events
      }
    };

    return () => es.close();
  }, []);

  return state;
}
