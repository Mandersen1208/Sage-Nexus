import fs from "fs";
import path from "path";
import { getSkillsRoot, loadSkillRegistry } from "../constants.js";

export interface SkillSummary {
  id: string;
  name: string;
  description: string;
  tags: string[];
}

export interface SkillContent {
  id: string;
  name: string;
  content: string;
  truncated: boolean;
}

export interface ReferenceFile {
  name: string;
  path: string;
}

/**
 * List all registered skills with metadata.
 */
export function listSkills(): SkillSummary[] {
  return Object.values(loadSkillRegistry()).map(({ id, name, description, tags }) => ({
    id, name, description, tags,
  }));
}

/**
 * Read the main SKILL.md for a given skill id.
 */
export function getSkillContent(id: string, limit?: number): SkillContent | null {
  const skill = loadSkillRegistry()[id];
  if (!skill) return null;

  const filePath = path.join(skill.skillsRoot || getSkillsRoot(), skill.skillFile);
  if (!fs.existsSync(filePath)) return null;

  let content = fs.readFileSync(filePath, "utf-8");
  let truncated = false;

  if (limit && content.length > limit) {
    content = content.slice(0, limit) + "\n\n[... content truncated — use skill_get_reference to read specific reference files]";
    truncated = true;
  }

  return { id, name: skill.name, content, truncated };
}

/**
 * List reference files for a skill (if it has a references/ dir).
 */
export function listSkillReferences(id: string): ReferenceFile[] {
  const skill = loadSkillRegistry()[id];
  if (!skill?.referencesDir) return [];

  const refDir = path.join(skill.skillsRoot || getSkillsRoot(), skill.referencesDir);
  if (!fs.existsSync(refDir)) return [];

  return fs.readdirSync(refDir)
    .filter(f => f.endsWith(".md") && !f.startsWith("_"))
    .sort()
    .map(f => ({ name: f.replace(".md", ""), path: path.join(refDir, f) }));
}

/**
 * Read a specific reference file for a skill.
 */
export function getSkillReference(id: string, refName: string, limit?: number): SkillContent | null {
  const skill = loadSkillRegistry()[id];
  if (!skill?.referencesDir) return null;

  const refPath = path.join(skill.skillsRoot || getSkillsRoot(), skill.referencesDir, `${refName}.md`);
  if (!fs.existsSync(refPath)) return null;

  let content = fs.readFileSync(refPath, "utf-8");
  let truncated = false;

  if (limit && content.length > limit) {
    content = content.slice(0, limit) + "\n\n[... truncated]";
    truncated = true;
  }

  return { id: `${id}/${refName}`, name: `${skill.name} — ${refName}`, content, truncated };
}

/**
 * Search skills by keyword across name, description, and tags.
 */
export function searchSkills(query: string): SkillSummary[] {
  const q = query.toLowerCase();
  return listSkills().filter(s =>
    s.name.toLowerCase().includes(q) ||
    s.description.toLowerCase().includes(q) ||
    s.tags.some(t => t.includes(q))
  );
}
