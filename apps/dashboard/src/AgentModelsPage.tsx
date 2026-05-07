import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  getAgentModelCatalog,
  updateAgentModel,
  type AgentModelCatalog,
  type AgentModelConfig,
} from "./api.js";

const AUTO_REFRESH_MS = 30_000;

function sortByName(items: AgentModelConfig[]): AgentModelConfig[] {
  return [...items].sort((a, b) => {
    if (a.agentId === "AGT-sage") return -1;
    if (b.agentId === "AGT-sage") return 1;
    return a.displayName.localeCompare(b.displayName);
  });
}

function SourceBadge({ source }: { source: string }) {
  const cls =
    source === "override"
      ? "models-source-badge models-source-override"
      : source === "registry"
        ? "models-source-badge models-source-registry"
        : "models-source-badge models-source-default";
  return <span className={cls}>{source}</span>;
}

export default function AgentModelsPage() {
  const [catalog, setCatalog] = useState<AgentModelCatalog | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState<Record<string, boolean>>({});
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  // agentId → timestamp until which "✓ Live" confirmation badge is shown
  const [confirmed, setConfirmed] = useState<Record<string, number>>({});
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadCatalog = useCallback(async (silent = false) => {
    if (!silent) setBusy(true);
    try {
      const next = await getAgentModelCatalog();
      setCatalog(next);
      setErr(null);
      setDrafts((prev) => {
        const merged: Record<string, string> = { ...prev };
        for (const agent of next.agents) {
          if (!(agent.agentId in merged)) {
            merged[agent.agentId] = agent.currentModel;
          }
        }
        return merged;
      });
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      if (!silent) setBusy(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    void loadCatalog();
  }, [loadCatalog]);

  // Auto-refresh ticker
  useEffect(() => {
    timerRef.current = setInterval(() => void loadCatalog(true), AUTO_REFRESH_MS);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [loadCatalog]);

  const agents = useMemo(() => sortByName(catalog?.agents ?? []), [catalog]);

  const saveAgentModel = useCallback(
    async (agentId: string) => {
      const nextModel = (drafts[agentId] ?? "").trim();
      setSaving((prev) => ({ ...prev, [agentId]: true }));
      try {
        const updated = await updateAgentModel(agentId, nextModel);
        // Re-fetch the full catalog so currentModel reflects live ActiveModel()
        const fresh = await getAgentModelCatalog();
        setCatalog(fresh);
        setDrafts((prev) => {
          const merged = { ...prev };
          for (const agent of fresh.agents) {
            if (!(agent.agentId in merged)) {
              merged[agent.agentId] = agent.currentModel;
            }
          }
          return merged;
        });
        // Find live model from fresh catalog for the just-updated agent
        const liveAgent = fresh.agents.find((a) => a.agentId === updated.agentId);
        const liveModel = liveAgent?.currentModel ?? updated.currentModel;
        const appliedModel = nextModel || updated.configuredModel || "";
        // Mark confirmed if the live model matches what was applied
        if (!appliedModel || liveModel === appliedModel) {
          const expiresAt = Date.now() + 4000;
          setConfirmed((prev) => ({ ...prev, [agentId]: expiresAt }));
          setTimeout(() => {
            setConfirmed((prev) => {
              const next = { ...prev };
              if ((next[agentId] ?? 0) <= Date.now()) delete next[agentId];
              return next;
            });
          }, 4200);
        }
        setErr(null);
      } catch (e) {
        setErr(e instanceof Error ? e.message : String(e));
      } finally {
        setSaving((prev) => ({ ...prev, [agentId]: false }));
      }
    },
    [drafts],
  );

  const now = Date.now();

  return (
    <main className="models-page">
      <section className="models-card">
        <div className="models-head">
          <div>
            <h1>Agent Models</h1>
            <p>
              Live model routing per agent. <strong>Live Model</strong> reflects{" "}
              <code>ActiveModel()</code> from the running process — it confirms the change is
              in effect. Auto-refreshes every {AUTO_REFRESH_MS / 1000}s.
            </p>
          </div>
          <button className="models-refresh-btn" onClick={() => void loadCatalog()} disabled={busy}>
            {busy ? "Refreshing..." : "↺ Refresh"}
          </button>
        </div>

        {err && <div className="models-error">{err}</div>}

        <div className="models-table-wrap">
          <table className="models-table">
            <thead>
              <tr>
                <th>Agent</th>
                <th title="ActiveModel() from the running process — updates after Apply">Live Model</th>
                <th title="Model set in agents.json / registry">Registry</th>
                <th title="override = your change is live · registry = from config · default = fallback">Source</th>
                <th>Set Model</th>
              </tr>
            </thead>
            <tbody>
              {agents.map((agent) => {
                const isSaving = Boolean(saving[agent.agentId]);
                const isConfirmed = (confirmed[agent.agentId] ?? 0) > now;
                const selectedValue = drafts[agent.agentId] ?? "";
                const options = [...(catalog?.modelOptions ?? [])];
                if (selectedValue && !options.includes(selectedValue)) {
                  options.unshift(selectedValue);
                }
                return (
                  <tr key={agent.agentId} className={isConfirmed ? "models-row-confirmed" : undefined}>
                    <td>
                      <div className="models-agent-name">{agent.displayName}</div>
                      <div className="models-agent-id">{agent.agentId}</div>
                    </td>
                    <td>
                      <span className="models-mono models-live-model">
                        {agent.currentModel || "(default)"}
                      </span>
                      {isConfirmed && (
                        <span className="models-confirmed-badge" title="Change confirmed live">✓ Live</span>
                      )}
                    </td>
                    <td className="models-mono">{agent.configuredModel || "(none)"}</td>
                    <td><SourceBadge source={agent.source} /></td>
                    <td>
                      <div className="models-editor">
                        <select
                          value={selectedValue}
                          onChange={(e) =>
                            setDrafts((prev) => ({
                              ...prev,
                              [agent.agentId]: e.target.value,
                            }))
                          }
                        >
                          <option value="">(Use registry/default)</option>
                          {options.map((modelName) => (
                            <option key={modelName} value={modelName}>{modelName}</option>
                          ))}
                        </select>
                        <button onClick={() => void saveAgentModel(agent.agentId)} disabled={isSaving}>
                          {isSaving ? "Saving..." : "Apply"}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </main>
  );
}

