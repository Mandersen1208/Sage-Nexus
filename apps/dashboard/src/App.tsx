import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { sendContinue } from "./api.js";
import { useStream, type StreamState } from "./hooks/useStream.js";
import type {
  AgentHandoff,
  AgentResponseLog,
  ErrorEvent,
  MergedRequest,
  RequestEntry,
  TaskState,
  TaskStatusUpdateEvent,
} from "./types.js";
import "./App.css";

type Route = "chat" | "dashboard" | "models" | "settings" | "files" | "skills";

const ChatPage = lazy(() => import("./Chat.js"));
const AgentModelsPage = lazy(() => import("./AgentModelsPage.js"));
const SettingsPage = lazy(() => import("./SettingsPage.js"));
const FilesPage = lazy(() => import("./FilesPage.js"));
const SkillsPage = lazy(() => import("./SkillsPage.js"));

function currentRoute(): Route {
  if (window.location.pathname.startsWith("/settings")) return "settings";
  if (window.location.pathname.startsWith("/models")) return "models";
  if (window.location.pathname.startsWith("/files")) return "files";
  if (window.location.pathname.startsWith("/skills")) return "skills";
  return window.location.pathname.startsWith("/dashboard") ? "dashboard" : "chat";
}

function formatAge(ts: number): string {
  const diff = Math.floor(Date.now() / 1000 - ts);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return `${Math.floor(diff / 3600)}h ago`;
}

function shortId(id: string): string {
  const parts = id.split("-");
  return (parts[parts.length - 1] ?? id).slice(0, 8);
}

function humanAgent(id: string): string {
  return id
    .replace(/^AGT-/, "")
    .replace(/-agent$/, "")
    .replace(/-/g, " ");
}

function str(v: unknown): string {
  if (typeof v === "string") return v;
  if (v == null) return "";
  return JSON.stringify(v, null, 2);
}

function statusToLegacy(state: TaskState): "pending" | "done" | "error" | "paused" {
  if (state === "completed") return "done";
  if (state === "failed" || state === "canceled" || state === "rejected") return "error";
  if (state === "input-required" || state === "auth-required") return "paused";
  return "pending";
}

function metaStr(meta: Record<string, unknown> | undefined, key: string): string {
  const v = meta?.[key];
  return typeof v === "string" ? v : "";
}

function metaNum(meta: Record<string, unknown> | undefined, key: string): number {
  const v = meta?.[key];
  return typeof v === "number" ? v : 0;
}

function activityIcon(activity: string): string {
  switch (activity) {
    case "start": return "start";
    case "route": return "route";
    case "worker": return "agent";
    case "tool": return "tool";
    case "heartbeat": return "beat";
    case "error": return "error";
    case "input-required": return "wait";
    default: return "event";
  }
}

function pausedSummary(timeline: { events: TaskStatusUpdateEvent[] }): {
  prompt: string;
  reason: string;
  rounds: number;
  lastAgent: string;
} | null {
  const lastPaused = [...timeline.events]
    .reverse()
    .find((e) => e.status.state === "input-required" || e.status.state === "auth-required");
  if (!lastPaused) return null;
  return {
    prompt:
      (lastPaused.metadata?.["prompt"] as string | undefined) ??
      lastPaused.status.message?.parts?.[0]?.text ??
      "Manager paused",
    reason: (lastPaused.metadata?.["reason"] as string | undefined) ?? "paused",
    rounds: (lastPaused.metadata?.["rounds_completed"] as number | undefined) ?? 0,
    lastAgent: (lastPaused.metadata?.["last_agent"] as string | undefined) ?? "",
  };
}

function failureSummary(timeline: { events: TaskStatusUpdateEvent[] }): {
  reason: string;
  rawError: string;
  rounds: number;
  lastAgent: string;
} | null {
  const lastFailed = [...timeline.events]
    .reverse()
    .find((e) => e.status.state === "failed" || e.status.state === "canceled" || e.status.state === "rejected");
  if (!lastFailed) return null;
  return {
    reason: (lastFailed.metadata?.["reason"] as string | undefined) ?? "unknown",
    rawError: (lastFailed.metadata?.["raw_error"] as string | undefined) ?? "",
    rounds: (lastFailed.metadata?.["rounds_completed"] as number | undefined) ?? 0,
    lastAgent: (lastFailed.metadata?.["last_agent"] as string | undefined) ?? "",
  };
}

function useRoute(): [Route, (next: Route) => void] {
  const [route, setRoute] = useState<Route>(currentRoute);

  useEffect(() => {
    if (window.location.pathname === "/") {
      window.history.replaceState(null, "", "/chat");
      setRoute("chat");
    }
    const onPopState = () => setRoute(currentRoute());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const navigate = useCallback((next: Route) => {
    const path =
      next === "dashboard"
        ? "/dashboard"
        : next === "models"
          ? "/models"
          : next === "settings"
            ? "/settings"
            : next === "files"
              ? "/files"
              : next === "skills"
                ? "/skills"
              : "/chat";
    if (window.location.pathname !== path) {
      window.history.pushState(null, "", path);
    }
    setRoute(next);
  }, []);

  return [route, navigate];
}

export default function App() {
  const stream = useStream();
  const [route, navigate] = useRoute();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const enriched = useMemo(() => buildMergedRequests(stream), [stream]);
  const inFlight = enriched.filter((r) => r.status === "pending").length;

  const openTask = useCallback((taskId: string) => {
    setSelectedId(taskId);
    navigate("dashboard");
  }, [navigate]);

  return (
    <div className="app">
      <Topbar
        connected={stream.connected}
        route={route}
        onNavigate={navigate}
        taskCount={enriched.length}
        inFlight={inFlight}
        errorCount={stream.errors.length}
      />
      {route === "dashboard" ? (
        <DashboardPage
          enriched={enriched}
          errors={stream.errors}
          selectedId={selectedId}
          onSelect={setSelectedId}
        />
      ) : route === "models" ? (
        <Suspense fallback={<PageLoading />}>
          <AgentModelsPage />
        </Suspense>
      ) : route === "settings" ? (
        <Suspense fallback={<PageLoading />}>
          <SettingsPage />
        </Suspense>
      ) : route === "files" ? (
        <Suspense fallback={<PageLoading />}>
          <FilesPage />
        </Suspense>
      ) : route === "skills" ? (
        <Suspense fallback={<PageLoading />}>
          <SkillsPage />
        </Suspense>
      ) : (
        <Suspense fallback={<PageLoading />}>
          <ChatPage stream={stream} onOpenTask={openTask} />
        </Suspense>
      )}
    </div>
  );
}

function PageLoading() {
  return <main className="page-loading">Loading...</main>;
}

function buildMergedRequests({ tasks, logs }: StreamState): MergedRequest[] {
  const logMap = new Map<string, AgentResponseLog>(
    logs.map((l) => [l.request_id, l]),
  );
  const seen = new Set<string>();
  const result: MergedRequest[] = [];

  for (const [id, t] of Object.entries(tasks)) {
    seen.add(id);
    const initial = t.events[0];
    const inputText =
      initial?.status.message?.parts?.find((p) => p.kind === "text")?.text ?? "";
    result.push({
      request_id: id,
      content: inputText || str(logMap.get(id)?.input),
      status: statusToLegacy(t.state),
      timestamp: Math.floor(t.startedAt / 1000),
      output: t.finalText || t.artifact,
      error: t.errorText,
      log: logMap.get(id),
      timeline: t,
    });
  }

  for (const log of logs) {
    if (!seen.has(log.request_id)) {
      const entry: RequestEntry = {
        request_id: log.request_id,
        content: str(log.input),
        status: log.error ? "error" : "done",
        timestamp: log.timestamp,
        output: str(log.output),
        error: log.error,
      };
      result.push({ ...entry, log });
    }
  }

  return result.sort((a, b) => (b.timestamp ?? 0) - (a.timestamp ?? 0));
}

function Topbar({
  connected,
  route,
  onNavigate,
  taskCount,
  inFlight,
  errorCount,
}: {
  connected: boolean;
  route: Route;
  onNavigate: (route: Route) => void;
  taskCount: number;
  inFlight: number;
  errorCount: number;
}) {
  return (
    <header className="topbar">
      <div className="topbar-left">
        <div className="brand">
          <span className="brand-name">SAGE NEXUS</span>
          <span className={`conn-dot ${connected ? "on" : "off"}`} />
          <span className="conn-label">{connected ? "Live" : "Offline"}</span>
        </div>
        <nav className="topnav">
          <button
            className={`nav-btn ${route === "chat" ? "active" : ""}`}
            onClick={() => onNavigate("chat")}
          >
            Chat
          </button>
          <button
            className={`nav-btn ${route === "dashboard" ? "active" : ""}`}
            onClick={() => onNavigate("dashboard")}
          >
            Dashboard
          </button>
          <button
            className={`nav-btn ${route === "models" ? "active" : ""}`}
            onClick={() => onNavigate("models")}
          >
            Models
          </button>
          <button
            className={`nav-btn ${route === "files" ? "active" : ""}`}
            onClick={() => onNavigate("files")}
          >
            Files
          </button>
          <button
            className={`nav-btn ${route === "skills" ? "active" : ""}`}
            onClick={() => onNavigate("skills")}
          >
            Skills
          </button>
          <button
            className={`nav-btn ${route === "settings" ? "active" : ""}`}
            onClick={() => onNavigate("settings")}
          >
            Settings
          </button>
        </nav>
      </div>
      <div className="stats">
        <Stat label="Tasks" value={taskCount} />
        <Stat label="In-flight" value={inFlight} glow={inFlight > 0} />
        <Stat label="Errors" value={errorCount} warn={errorCount > 0} />
      </div>
    </header>
  );
}

function DashboardPage({
  enriched,
  errors,
  selectedId,
  onSelect,
}: {
  enriched: MergedRequest[];
  errors: ErrorEvent[];
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}) {
  const [, setTick] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, []);

  const selected = enriched.find((r) => r.request_id === selectedId) ?? null;

  return (
    <div className="dashboard-shell">
      <div className={`body ${selected ? "has-selected" : ""}`}>
        <aside className="sidebar">
          <div className="sidebar-title">Tasks</div>
          <div className="req-list">
            {enriched.length === 0 && (
              <div className="empty">No tasks yet.</div>
            )}
            {enriched.map((req) => (
              <ReqItem
                key={req.request_id}
                req={req}
                selected={selectedId === req.request_id}
                onClick={() => onSelect(req.request_id)}
              />
            ))}
          </div>
        </aside>

        <main className="detail">
          {selected ? (
            <>
              <button className="detail-back" onClick={() => onSelect(null)}>
                Back to tasks
              </button>
              <ReqDetail req={selected} />
            </>
          ) : (
            <div className="no-sel">
              <p>Select a task to see the live activity timeline</p>
            </div>
          )}
        </main>
      </div>

      <footer className="errfeed">
        <div className="errfeed-title">Error Bus</div>
        <div className="errfeed-list">
          {errors.length === 0 ? (
            <span className="no-errors">All clear</span>
          ) : (
            errors.map((e, i) => <ErrItem key={i} err={e} />)
          )}
        </div>
      </footer>
    </div>
  );
}

function Stat({
  label,
  value,
  glow,
  warn,
}: {
  label: string;
  value: number;
  glow?: boolean;
  warn?: boolean;
}) {
  const cls = warn ? "warn" : glow ? "glow" : "";
  return (
    <div className="stat">
      <span className="stat-label">{label}</span>
      <span className={`stat-value ${cls}`}>{value}</span>
    </div>
  );
}

function ReqItem({
  req,
  selected,
  onClick,
}: {
  req: MergedRequest;
  selected: boolean;
  onClick: () => void;
}) {
  const content = req.content ?? str(req.log?.input);
  const live =
    req.timeline && Date.now() - req.timeline.lastEventAt < 7_000 && req.status === "pending";
  return (
    <div
      className={`req-item ${selected ? "sel" : ""} st-${req.status}`}
      onClick={onClick}
    >
      <div className="req-row">
        <span className="req-id">
          {live && <span className="pulse-dot" />}
          {shortId(req.request_id)}
        </span>
        <span className={`badge st-${req.status}`}>{req.status}</span>
      </div>
      <div className="req-preview">{content.slice(0, 90)}{content.length > 90 ? "..." : ""}</div>
      <div className="req-meta">
        {req.timestamp ? formatAge(req.timestamp) : ""}
        {req.timeline && req.timeline.events.length > 0 && (
          <span className="req-chain"> · {req.timeline.events.length} events</span>
        )}
      </div>
    </div>
  );
}

function ReqDetail({ req }: { req: MergedRequest }) {
  const content = req.content ?? str(req.log?.input);
  const output = req.output ?? str(req.log?.output);
  const handoffs = req.log?.handoffs ?? [];
  const timeline = req.timeline;
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const onDecision = useCallback(
    async (decision: "continue" | "stop") => {
      setBusy(true);
      setErr(null);
      try {
        await sendContinue(req.request_id, decision);
      } catch (e) {
        setErr(e instanceof Error ? e.message : String(e));
      } finally {
        setBusy(false);
      }
    },
    [req.request_id],
  );

  return (
    <div className="detail-inner">
      <div className="detail-hdr">
        <span className="detail-id">Task {shortId(req.request_id)}</span>
        <span className={`badge st-${req.status}`}>
          {timeline ? timeline.state : req.status}
        </span>
        {req.log?.model && <span className="detail-model">{req.log.model}</span>}
      </div>

      <Section title="Input">
        <pre className="code-block">{content}</pre>
      </Section>

      {timeline && (timeline.state === "input-required" || timeline.state === "auth-required") && (() => {
        const p = pausedSummary(timeline);
        if (!p) return null;
        return (
          <Section title="Paused - Awaiting Confirmation">
            <div className="paused-card">
              <div className="paused-prompt">{p.prompt}</div>
              <div className="paused-meta">
                <span className="paused-reason">{p.reason}</span>
                {p.rounds > 0 && <span className="paused-rounds">{p.rounds} rounds done</span>}
                {p.lastAgent && <span className="paused-agent">last: {humanAgent(p.lastAgent)}</span>}
              </div>
              <div className="paused-actions">
                <button
                  className="paused-btn continue"
                  onClick={() => onDecision("continue")}
                  disabled={busy}
                >
                  Continue
                </button>
                <button
                  className="paused-btn stop"
                  onClick={() => onDecision("stop")}
                  disabled={busy}
                >
                  Stop
                </button>
              </div>
              {err && <div className="paused-err">{err}</div>}
            </div>
          </Section>
        );
      })()}

      {timeline && (timeline.state === "failed" || timeline.state === "canceled" || timeline.state === "rejected") && (() => {
        const f = failureSummary(timeline);
        if (!f) return null;
        return (
          <Section title="Failure Reason">
            <div className="fail-card">
              <div className="fail-row">
                <span className="fail-reason">{f.reason}</span>
                {f.rounds > 0 && <span className="fail-rounds">{f.rounds} rounds</span>}
                {f.lastAgent && <span className="fail-agent">last: {humanAgent(f.lastAgent)}</span>}
              </div>
              {f.rawError && <pre className="code-block fail-raw">{f.rawError}</pre>}
            </div>
          </Section>
        );
      })()}

      {timeline && timeline.events.length > 0 && (
        <Section title={`Activity (${timeline.events.length})`}>
          <div className="timeline">
            {timeline.events.map((evt, i) => (
              <TimelineRow key={i} evt={evt} />
            ))}
          </div>
        </Section>
      )}

      {handoffs.length > 0 && (
        <Section title={`Agent Chain (${handoffs.length})`}>
          <div className="chain">
            {handoffs.map((h, i) => (
              <AgentCard key={i} handoff={h} index={i} total={handoffs.length} />
            ))}
          </div>
        </Section>
      )}

      {output && (
        <Section title="Final Output">
          <pre className="code-block output">{output}</pre>
        </Section>
      )}

      {req.error && (
        <Section title="Error">
          <pre className="code-block err">{req.error}</pre>
        </Section>
      )}

      {req.log?.tool_errors && req.log.tool_errors.length > 0 && (
        <Section title="Tool Errors">
          {req.log.tool_errors.map((te, i) => (
            <div key={i} className="tool-err-row">
              <span className="tool-err-agent">{humanAgent(te.agent)}</span>
              <span className="tool-err-name">{te.tool}</span>
              <span className="tool-err-msg">{te.error}</span>
            </div>
          ))}
        </Section>
      )}
    </div>
  );
}

function TimelineRow({ evt }: { evt: TaskStatusUpdateEvent }) {
  const activity = metaStr(evt.metadata, "activity") || evt.status.state;
  const phase = metaStr(evt.metadata, "phase");
  const agent = metaStr(evt.metadata, "agent");
  const tool = metaStr(evt.metadata, "tool");
  const dur = metaNum(evt.metadata, "duration_ms");
  const elapsed = metaNum(evt.metadata, "elapsed_ms");
  const errMsg = metaStr(evt.metadata, "error");
  const message =
    evt.status.message?.parts?.find((p) => p.kind === "text")?.text ?? "";
  const ts = evt.status.timestamp
    ? new Date(evt.status.timestamp).toLocaleTimeString()
    : "";

  return (
    <div className={`tl-row tl-${activity}`}>
      <span className="tl-time">{ts}</span>
      <span className="tl-icon">{activityIcon(activity)}</span>
      <span className="tl-activity">{activity}{phase ? `:${phase}` : ""}</span>
      {agent && <span className="tl-agent">{humanAgent(agent)}</span>}
      {tool && <span className="tl-tool">{tool}</span>}
      {dur > 0 && <span className="tl-dur">{dur}ms</span>}
      {elapsed > 0 && activity === "heartbeat" && (
        <span className="tl-elapsed">{Math.round(elapsed / 1000)}s</span>
      )}
      {message && <span className="tl-msg">{message}</span>}
      {errMsg && <span className="tl-err">{errMsg}</span>}
    </div>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="section">
      <div className="section-title">{title}</div>
      {children}
    </section>
  );
}

function AgentCard({
  handoff,
  index,
  total,
}: {
  handoff: AgentHandoff;
  index: number;
  total: number;
}) {
  const [open, setOpen] = useState(true);

  return (
    <div className="agent-card">
      <div className="agent-hdr" onClick={() => setOpen((o) => !o)}>
        <div className="agent-step">
          <span className="step-num">{index + 1}/{total}</span>
          <span className="agent-name">{humanAgent(handoff.agent_id)}</span>
          <span className="agent-role">{handoff.role}</span>
          {handoff.model && <span className="agent-model">{handoff.model}</span>}
        </div>
        <div className="agent-tools">
          {handoff.tool_calls?.map((t, i) => (
            <span key={i} className="tool-chip">{t}</span>
          ))}
        </div>
        <span className="expand">{open ? "up" : "down"}</span>
      </div>
      {open && (
        <div className="agent-body">
          {handoff.reply ? (
            <pre className="agent-reply">{handoff.reply}</pre>
          ) : (
            <span className="no-reply">No reply captured for this agent yet</span>
          )}
        </div>
      )}
    </div>
  );
}

function ErrItem({ err }: { err: ErrorEvent }) {
  const ts = err.timestamp
    ? new Date(err.timestamp * 1000).toLocaleTimeString()
    : "";
  return (
    <div className="err-item">
      {ts && <span className="err-ts">{ts}</span>}
      <span className="err-kind">{err.kind}</span>
      {err.agent && (
        <span className="err-agent">{humanAgent(err.agent)}</span>
      )}
      {err.tool && <span className="err-tool">{err.tool}</span>}
      <span className="err-msg">{err.error}</span>
    </div>
  );
}
