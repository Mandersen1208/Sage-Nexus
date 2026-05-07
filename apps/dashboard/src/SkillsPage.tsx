import { useCallback, useEffect, useMemo, useState } from "react";
import {
  composeSkill,
  deleteSkill,
  getDiscoveredSkillSources,
  getDiscoveredSkills,
  getSkillCatalog,
  releaseDiscoveredSkillSource,
  syncDiscoveredSkills,
  syncDiscoveredSkillSource,
  updateDiscoveredSkill,
  updateDiscoveredSkillSource,
  createDiscoveredSkillSource,
  updateSkill,
  type DiscoveredSkillItem,
  type DiscoveredSkillSource,
  type SkillCatalogItem,
} from "./api.js";

type LocalSkillDraft = {
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
};

type SourceDraft = {
  id: string;
  displayName: string;
  endpoint: string;
  trust: "trusted" | "untrusted";
  enabled: boolean;
};

const defaultLocalSkillDraft: LocalSkillDraft = {
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
};

const defaultSourceDraft: SourceDraft = {
  id: "",
  displayName: "",
  endpoint: "",
  trust: "untrusted",
  enabled: true,
};

function bySource(
  skills: DiscoveredSkillItem[],
): Map<string, DiscoveredSkillItem[]> {
  const grouped = new Map<string, DiscoveredSkillItem[]>();
  for (const skill of skills) {
    const list = grouped.get(skill.sourceId) ?? [];
    list.push(skill);
    grouped.set(skill.sourceId, list);
  }
  for (const list of grouped.values()) {
    list.sort((a, b) => a.canonicalName.localeCompare(b.canonicalName));
  }
  return grouped;
}

export default function SkillsPage() {
  const [localSkills, setLocalSkills] = useState<SkillCatalogItem[]>([]);
  const [sources, setSources] = useState<DiscoveredSkillSource[]>([]);
  const [discoveredSkills, setDiscoveredSkills] = useState<DiscoveredSkillItem[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const [localModalOpen, setLocalModalOpen] = useState(false);
  const [localDraft, setLocalDraft] = useState<LocalSkillDraft>(defaultLocalSkillDraft);

  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [sourceDraft, setSourceDraft] = useState<SourceDraft>(defaultSourceDraft);
  const [workspaceLocalExpanded, setWorkspaceLocalExpanded] = useState(true);
  const [indexedLocalExpanded, setIndexedLocalExpanded] = useState(true);
  const [expandedSourceIds, setExpandedSourceIds] = useState<Record<string, boolean>>({});

  const loadLocalSkills = useCallback(async () => {
    const catalog = await getSkillCatalog();
    setLocalSkills(catalog.skills);
    return catalog.skills;
  }, []);

  const loadDiscovered = useCallback(async () => {
    const [nextSources, nextSkills] = await Promise.all([
      getDiscoveredSkillSources(),
      getDiscoveredSkills(),
    ]);
    setSources(nextSources);
    setDiscoveredSkills(nextSkills);
  }, []);

  const reloadAll = useCallback(async () => {
    await Promise.all([loadLocalSkills(), loadDiscovered()]);
  }, [loadDiscovered, loadLocalSkills]);

  useEffect(() => {
    void reloadAll().catch((e) => {
      setError(e instanceof Error ? e.message : String(e));
    });
  }, [reloadAll]);

  const grouped = useMemo(() => bySource(discoveredSkills), [discoveredSkills]);
  const localSource = useMemo(
    () => sources.find((source) => source.sourceType === "local") ?? null,
    [sources],
  );
  const serverSources = useMemo(
    () => sources.filter((source) => source.sourceType !== "local"),
    [sources],
  );
  const localDiscoveredSkills = useMemo(
    () => (localSource ? grouped.get(localSource.id) ?? [] : []),
    [grouped, localSource],
  );

  useEffect(() => {
    setExpandedSourceIds((prev) => {
      let changed = false;
      const next: Record<string, boolean> = { ...prev };
      for (const source of serverSources) {
        if (typeof next[source.id] === "boolean") continue;
        next[source.id] = true;
        changed = true;
      }
      return changed ? next : prev;
    });
  }, [serverSources]);

  const onCreateLocalSkill = useCallback(() => {
    setLocalDraft(defaultLocalSkillDraft);
    setLocalModalOpen(true);
    setError(null);
  }, []);

  const onSaveLocalSkill = useCallback(() => {
    const id = localDraft.id.trim().toLowerCase();
    if (!id || !/^[a-z0-9][a-z0-9-_]*$/.test(id)) {
      setError("Skill id must be lowercase alphanumeric and may include - or _.");
      return;
    }
    const tags = localDraft.tagsText
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);
    const assigned = localDraft.assignedAgentIdsText
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);

    const run = async () => {
      setBusy(true);
      setError(null);
      await composeSkill({
        id,
        name: localDraft.name.trim(),
        description: localDraft.description.trim(),
        tags,
        enabled: localDraft.enabled,
        trigger: localDraft.trigger.trim(),
        assignedAgentIds: assigned,
        inputs: localDraft.inputs.trim(),
        outputs: localDraft.outputs.trim(),
        notes: localDraft.notes.trim(),
      });
      setSuccess(`Created local skill ${id}.`);
      setLocalModalOpen(false);
      await loadLocalSkills();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadLocalSkills, localDraft]);

  const onToggleLocalSkill = useCallback((skill: SkillCatalogItem) => {
    const run = async () => {
      setBusy(true);
      setError(null);
      await updateSkill(skill.id, { enabled: !skill.enabled });
      setSuccess(`${skill.enabled ? "Disabled" : "Enabled"} local skill ${skill.id}.`);
      await loadLocalSkills();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadLocalSkills]);

  const onDeleteLocalSkill = useCallback((id: string) => {
    if (!window.confirm(`Delete local skill ${id}?`)) return;
    const run = async () => {
      setBusy(true);
      setError(null);
      await deleteSkill(id);
      setSuccess(`Deleted local skill ${id}.`);
      await loadLocalSkills();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadLocalSkills]);

  const onCreateSource = useCallback(() => {
    setSourceDraft(defaultSourceDraft);
    setSourceModalOpen(true);
    setError(null);
  }, []);

  const onSaveSource = useCallback(() => {
    const id = sourceDraft.id.trim().toLowerCase();
    if (!id || !/^[a-z0-9][a-z0-9._:-]*$/.test(id)) {
      setError("Server id must be lowercase and may include . _ : -");
      return;
    }
    if (!sourceDraft.endpoint.trim()) {
      setError("Server endpoint is required.");
      return;
    }
    const run = async () => {
      setBusy(true);
      setError(null);
      await createDiscoveredSkillSource({
        id,
        displayName: sourceDraft.displayName.trim() || id,
        endpoint: sourceDraft.endpoint.trim(),
        trust: sourceDraft.trust,
        enabled: sourceDraft.enabled,
      });
      setSourceModalOpen(false);
      setSuccess(`Added source ${id}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered, sourceDraft]);

  const onToggleSource = useCallback((source: DiscoveredSkillSource) => {
    const run = async () => {
      setBusy(true);
      setError(null);
      await updateDiscoveredSkillSource(source.id, { enabled: !source.enabled });
      setSuccess(`${source.enabled ? "Disabled" : "Enabled"} source ${source.id}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const onReleaseSource = useCallback((source: DiscoveredSkillSource) => {
    const run = async () => {
      setBusy(true);
      setError(null);
      const result = await releaseDiscoveredSkillSource(source.id);
      setSuccess(`Released ${result.released} quarantined skills for ${source.id}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const onSyncSource = useCallback((source: DiscoveredSkillSource) => {
    const run = async () => {
      setBusy(true);
      setError(null);
      await syncDiscoveredSkillSource(source.id);
      setSuccess(`Synced source ${source.id}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const onSyncAll = useCallback(() => {
    const run = async () => {
      setBusy(true);
      setError(null);
      await syncDiscoveredSkills();
      setSuccess("Synced all enabled discovery sources.");
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const onReleaseSkill = useCallback((skill: DiscoveredSkillItem) => {
    const run = async () => {
      setBusy(true);
      setError(null);
      await updateDiscoveredSkill(skill.id, { state: "released" });
      setSuccess(`Released skill ${skill.canonicalName}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const onDisableSkill = useCallback((skill: DiscoveredSkillItem) => {
    const next = skill.skillState === "disabled" ? "quarantined" : "disabled";
    const run = async () => {
      setBusy(true);
      setError(null);
      await updateDiscoveredSkill(skill.id, { state: next });
      setSuccess(`${next === "disabled" ? "Disabled" : "Re-enabled"} skill ${skill.canonicalName}.`);
      await loadDiscovered();
    };
    void run()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [loadDiscovered]);

  const toggleSourceExpanded = useCallback((sourceId: string) => {
    setExpandedSourceIds((prev) => ({
      ...prev,
      [sourceId]: !(prev[sourceId] ?? true),
    }));
  }, []);

  const renderDiscoveredSkillTable = useCallback((skills: DiscoveredSkillItem[]) => {
    if (skills.length === 0) {
      return <div className="settings-empty-tools">No discovered skills yet for this source.</div>;
    }
    return (
      <table className="settings-tools-table">
        <thead>
          <tr>
            <th>Skill</th>
            <th>State</th>
            <th>Risk</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {skills.map((skill) => (
            <tr key={skill.id}>
              <td>
                <div className="settings-tool-name">{skill.canonicalName}</div>
                <div className="settings-tool-id">{skill.originalToolName}</div>
                <div className="settings-tool-command">{skill.description}</div>
              </td>
              <td>{skill.skillState}</td>
              <td>{skill.riskLevel}</td>
              <td className="settings-tool-actions">
                <button
                  className="settings-btn"
                  onClick={() => onReleaseSkill(skill)}
                  disabled={busy || skill.skillState === "released"}
                >
                  Release
                </button>
                <button className="settings-btn" onClick={() => onDisableSkill(skill)} disabled={busy}>
                  {skill.skillState === "disabled" ? "Enable" : "Disable"}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }, [busy, onDisableSkill, onReleaseSkill]);

  return (
    <main className="settings-page">
      <section className="settings-card settings-tools-card">
        <header className="settings-head">
          <div>
            <h1>Skills</h1>
            <p>Manage local skills and govern discovered MCP server capabilities.</p>
          </div>
        </header>

        <div className="settings-actions">
          <button className="settings-btn primary" onClick={onCreateLocalSkill} disabled={busy}>Create Local Skill</button>
          <button className="settings-btn" onClick={onCreateSource} disabled={busy}>Add MCP Source</button>
          <button className="settings-btn" onClick={onSyncAll} disabled={busy}>Sync All Sources</button>
        </div>

        <div className="settings-subsection">
          <div className="settings-subhead">
            <h2>Local Skills</h2>
            <p>Create local skills and inspect indexed local capabilities.</p>
          </div>
          <div className="skills-source-list">
            <div className="skills-source-card">
              <div className="skills-source-head">
                <div>
                  <div className="settings-tool-name">Workspace Local Skills</div>
                  <div className="settings-tool-command">Create, enable/disable, and delete local workspace skills.</div>
                </div>
                <div className="settings-tool-actions">
                  <button className="settings-btn" onClick={() => setWorkspaceLocalExpanded((prev) => !prev)} disabled={busy}>
                    {workspaceLocalExpanded ? "Collapse" : "Expand"}
                  </button>
                </div>
              </div>
              {workspaceLocalExpanded && (
                <div className="settings-tools-table-wrap">
                  {localSkills.length === 0 ? (
                    <div className="settings-empty-tools">No local skills found yet.</div>
                  ) : (
                    <table className="settings-tools-table">
                      <thead>
                        <tr>
                          <th>Skill</th>
                          <th>Source</th>
                          <th>State</th>
                          <th>Updated</th>
                          <th>Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {localSkills.map((skill) => (
                          <tr key={skill.id}>
                            <td>
                              <div className="settings-tool-name">{skill.name}</div>
                              <div className="settings-tool-id">{skill.id}</div>
                            </td>
                            <td>{skill.source}</td>
                            <td>{skill.enabled ? "enabled" : "disabled"}</td>
                            <td className="settings-tool-command">{new Date(skill.updatedAt).toLocaleString()}</td>
                            <td className="settings-tool-actions">
                              <button className="settings-btn" onClick={() => onToggleLocalSkill(skill)} disabled={busy}>
                                {skill.enabled ? "Disable" : "Enable"}
                              </button>
                              <button className="settings-btn danger" onClick={() => onDeleteLocalSkill(skill.id)} disabled={busy}>
                                Delete
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              )}
            </div>

            {localSource && (
              <div className="skills-source-card">
                <div className="skills-source-head">
                  <div>
                    <div className="settings-tool-name">Indexed Local Skills</div>
                    <div className="settings-tool-id">{localSource.id} | {localSource.endpoint}</div>
                    <div className="settings-tool-command">
                      trust: {localSource.trust} | {localSource.enabled ? "enabled" : "disabled"}
                      {localSource.lastSyncStatus ? ` | sync: ${localSource.lastSyncStatus}` : ""}
                    </div>
                  </div>
                  <div className="settings-tool-actions">
                    <button className="settings-btn" onClick={() => onSyncSource(localSource)} disabled={busy}>Sync</button>
                    <button className="settings-btn" onClick={() => setIndexedLocalExpanded((prev) => !prev)} disabled={busy}>
                      {indexedLocalExpanded ? "Collapse" : "Expand"}
                    </button>
                  </div>
                </div>
                {indexedLocalExpanded && renderDiscoveredSkillTable(localDiscoveredSkills)}
              </div>
            )}
          </div>
        </div>

        <div className="settings-subsection">
          <div className="settings-subhead">
            <h2>Discovered Server Skills</h2>
            <p>Release quarantined skills by server or one-by-one. Disable servers and individual skills.</p>
          </div>
          {serverSources.length === 0 ? (
            <div className="settings-empty-tools">No discovery sources configured.</div>
          ) : (
            <div className="skills-source-list">
              {serverSources.map((source) => {
                const sourceSkills = grouped.get(source.id) ?? [];
                const quarantined = sourceSkills.filter((skill) => skill.skillState === "quarantined").length;
                const expanded = expandedSourceIds[source.id] ?? true;
                return (
                  <div className="skills-source-card" key={source.id}>
                    <div className="skills-source-head">
                      <div>
                        <div className="settings-tool-name">{source.displayName}</div>
                        <div className="settings-tool-id">{source.id} | {source.endpoint}</div>
                        <div className="settings-tool-command">
                          trust: {source.trust} | {source.enabled ? "enabled" : "disabled"}
                          {source.lastSyncStatus ? ` | sync: ${source.lastSyncStatus}` : ""}
                        </div>
                      </div>
                      <div className="settings-tool-actions">
                        <button className="settings-btn" onClick={() => onSyncSource(source)} disabled={busy}>Sync</button>
                        <button className="settings-btn" onClick={() => onReleaseSource(source)} disabled={busy || quarantined === 0}>
                          Release Server ({quarantined})
                        </button>
                        <button className="settings-btn" onClick={() => onToggleSource(source)} disabled={busy}>
                          {source.enabled ? "Disable Server" : "Enable Server"}
                        </button>
                        <button className="settings-btn" onClick={() => toggleSourceExpanded(source.id)} disabled={busy}>
                          {expanded ? "Collapse" : "Expand"}
                        </button>
                      </div>
                    </div>
                    {expanded && renderDiscoveredSkillTable(sourceSkills)}
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {error && <div className="settings-message error">{error}</div>}
        {success && <div className="settings-message success">{success}</div>}
      </section>

      {localModalOpen && (
        <div className="settings-modal-backdrop" role="presentation">
          <div className="settings-modal settings-skill-modal" role="dialog" aria-modal="true" aria-label="Create local skill">
            <h2>Create Local Skill</h2>
            <p>Compose a local skill without manual markdown editing.</p>
            <div className="settings-grid">
              <div className="settings-field">
                <label>Skill id</label>
                <input
                  className="settings-input"
                  value={localDraft.id}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, id: e.target.value }))}
                  placeholder="architecture-review"
                />
              </div>
              <div className="settings-field">
                <label>Name</label>
                <input
                  className="settings-input"
                  value={localDraft.name}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, name: e.target.value }))}
                  placeholder="Architecture Review"
                />
              </div>
              <div className="settings-field settings-wide-field">
                <label>Description</label>
                <input
                  className="settings-input"
                  value={localDraft.description}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, description: e.target.value }))}
                  placeholder="What this skill handles"
                />
              </div>
              <div className="settings-field">
                <label>Tags</label>
                <input
                  className="settings-input"
                  value={localDraft.tagsText}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, tagsText: e.target.value }))}
                  placeholder="planning, architecture"
                />
              </div>
              <div className="settings-field">
                <label>Intended agents</label>
                <input
                  className="settings-input"
                  value={localDraft.assignedAgentIdsText}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, assignedAgentIdsText: e.target.value }))}
                  placeholder="AGT-architect-agent, AGT-senior-dev-agent"
                />
              </div>
              <div className="settings-field settings-checkbox-field">
                <label>
                  <input
                    type="checkbox"
                    checked={localDraft.enabled}
                    onChange={(e) => setLocalDraft((prev) => ({ ...prev, enabled: e.target.checked }))}
                  />
                  Enabled
                </label>
              </div>
              <div className="settings-field settings-wide-field">
                <label>When to use</label>
                <textarea
                  className="settings-input settings-small-textarea"
                  value={localDraft.trigger}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, trigger: e.target.value }))}
                />
              </div>
              <div className="settings-field">
                <label>Inputs</label>
                <textarea
                  className="settings-input settings-small-textarea"
                  value={localDraft.inputs}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, inputs: e.target.value }))}
                />
              </div>
              <div className="settings-field">
                <label>Expected output</label>
                <textarea
                  className="settings-input settings-small-textarea"
                  value={localDraft.outputs}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, outputs: e.target.value }))}
                />
              </div>
              <div className="settings-field settings-wide-field">
                <label>Execution notes</label>
                <textarea
                  className="settings-input settings-small-textarea"
                  value={localDraft.notes}
                  onChange={(e) => setLocalDraft((prev) => ({ ...prev, notes: e.target.value }))}
                />
              </div>
            </div>
            <div className="settings-modal-actions">
              <button className="settings-btn primary" onClick={onSaveLocalSkill} disabled={busy}>Create Skill</button>
              <button className="settings-btn" onClick={() => setLocalModalOpen(false)} disabled={busy}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {sourceModalOpen && (
        <div className="settings-modal-backdrop" role="presentation">
          <div className="settings-modal" role="dialog" aria-modal="true" aria-label="Add MCP source">
            <h2>Add MCP Source</h2>
            <p>Register an external MCP server for canonical discovery sync.</p>
            <div className="settings-grid">
              <div className="settings-field">
                <label>Server id</label>
                <input
                  className="settings-input"
                  value={sourceDraft.id}
                  onChange={(e) => setSourceDraft((prev) => ({ ...prev, id: e.target.value }))}
                  placeholder="github-mcp"
                />
              </div>
              <div className="settings-field">
                <label>Display name</label>
                <input
                  className="settings-input"
                  value={sourceDraft.displayName}
                  onChange={(e) => setSourceDraft((prev) => ({ ...prev, displayName: e.target.value }))}
                  placeholder="GitHub MCP"
                />
              </div>
              <div className="settings-field settings-wide-field">
                <label>Endpoint</label>
                <input
                  className="settings-input"
                  value={sourceDraft.endpoint}
                  onChange={(e) => setSourceDraft((prev) => ({ ...prev, endpoint: e.target.value }))}
                  placeholder="http://host:3031"
                />
              </div>
              <div className="settings-field">
                <label>Trust</label>
                <select
                  className="settings-input"
                  value={sourceDraft.trust}
                  onChange={(e) => setSourceDraft((prev) => ({ ...prev, trust: e.target.value as "trusted" | "untrusted" }))}
                >
                  <option value="untrusted">untrusted</option>
                  <option value="trusted">trusted</option>
                </select>
              </div>
              <div className="settings-field settings-checkbox-field">
                <label>
                  <input
                    type="checkbox"
                    checked={sourceDraft.enabled}
                    onChange={(e) => setSourceDraft((prev) => ({ ...prev, enabled: e.target.checked }))}
                  />
                  Enabled
                </label>
              </div>
            </div>
            <div className="settings-modal-actions">
              <button className="settings-btn primary" onClick={onSaveSource} disabled={busy}>Add Source</button>
              <button className="settings-btn" onClick={() => setSourceModalOpen(false)} disabled={busy}>Cancel</button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}
