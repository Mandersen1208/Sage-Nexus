import fs from "fs";
import path from "path";

// Shared constants - no character limit enforced.
export const CHARACTER_LIMIT = Number.MAX_SAFE_INTEGER;

export const DEFAULT_SKILLS_ROOT = "/app/skills";
export const DEFAULT_SKILLS_OVERLAY_ROOT = "/sage-state/workspace/skills";

export interface SkillDefinition {
  id: string;
  name: string;
  description: string;
  tags: string[];
  skillFile: string;
  referencesDir?: string;
  skillsRoot?: string;
}

export function getSkillsRoot(): string {
  return process.env["SKILLS_ROOT"] || DEFAULT_SKILLS_ROOT;
}

export function getSkillsRoots(): string[] {
  const roots: string[] = [DEFAULT_SKILLS_ROOT];
  const envRoot = process.env["SKILLS_ROOT"]?.trim();
  const overlayRoot = (process.env["SKILLS_OVERLAY_ROOT"] || DEFAULT_SKILLS_OVERLAY_ROOT).trim();
  if (envRoot) roots.push(envRoot);
  if (overlayRoot) roots.push(overlayRoot);
  const seen = new Set<string>();
  return roots.filter((root) => {
    const normalized = root.replaceAll("\\", "/").replace(/\/+$/, "");
    if (!normalized || seen.has(normalized)) return false;
    seen.add(normalized);
    return true;
  });
}

export function loadSkillRegistry(skillsRoot?: string): Record<string, SkillDefinition> {
  if (!skillsRoot) {
    return loadSkillRegistryFromRoots(getSkillsRoots());
  }
  return loadSkillRegistryFromRoots([skillsRoot]);
}

export function loadSkillRegistryFromRoots(skillsRoots: string[]): Record<string, SkillDefinition> {
  const registry: Record<string, SkillDefinition> = {};
  for (const skillsRoot of skillsRoots) {
    if (!fs.existsSync(skillsRoot)) continue;

    for (const entry of fs.readdirSync(skillsRoot, { withFileTypes: true })) {
      if (!entry.isDirectory() || entry.name.startsWith(".")) continue;

      const skillFile = findSkillFile(skillsRoot, entry.name);
      if (!skillFile) continue;

      const content = fs.readFileSync(path.join(skillsRoot, skillFile), "utf-8");
      const metadata = readMetadata(path.join(skillsRoot, entry.name, "metadata.json"));
      const id = stringField(metadata, "id") || entry.name;
      const name = stringField(metadata, "name") || titleFromMarkdown(content) || titleFromId(id);
      const description =
        stringField(metadata, "description") ||
        stringField(metadata, "abstract") ||
        descriptionFromMarkdown(content);
      const tags = stringArrayField(metadata, "tags");
      const referencesDir =
        stringField(metadata, "referencesDir") ||
        findReferencesDir(skillsRoot, path.dirname(skillFile));

      // Later roots override earlier roots for the same skill id.
      registry[id] = {
        id,
        name,
        description,
        tags: tags.length > 0 ? tags : tagsFromId(id),
        skillFile,
        skillsRoot,
        ...(referencesDir ? { referencesDir } : {}),
      };
    }
  }

  return Object.fromEntries(Object.entries(registry).sort(([a], [b]) => a.localeCompare(b)));
}

// Backward-compatible startup snapshot. Service code calls loadSkillRegistry()
// directly so skills added at runtime are discovered without editing code.
export const SKILL_REGISTRY = loadSkillRegistry();

function findSkillFile(skillsRoot: string, skillDir: string): string | null {
  const direct = path.join(skillDir, "SKILL.md");
  if (fs.existsSync(path.join(skillsRoot, direct))) return direct;

  const root = path.join(skillsRoot, skillDir);
  const candidates = findMarkdownFiles(root)
    .filter((filePath) => path.basename(filePath) === "SKILL.md")
    .sort();
  if (candidates.length === 0) return null;
  return path.relative(skillsRoot, candidates[0]).replaceAll(path.sep, "/");
}

function findReferencesDir(skillsRoot: string, skillFileDir: string): string | undefined {
  const direct = path.join(skillsRoot, skillFileDir, "references");
  if (fs.existsSync(direct) && fs.statSync(direct).isDirectory()) {
    return path.join(skillFileDir, "references").replaceAll(path.sep, "/");
  }
  return undefined;
}

function findMarkdownFiles(root: string): string[] {
  if (!fs.existsSync(root)) return [];
  const out: string[] = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      out.push(...findMarkdownFiles(fullPath));
    } else if (entry.isFile() && entry.name.endsWith(".md")) {
      out.push(fullPath);
    }
  }
  return out;
}

function readMetadata(filePath: string): Record<string, unknown> {
  if (!fs.existsSync(filePath)) return {};
  try {
    const parsed = JSON.parse(fs.readFileSync(filePath, "utf-8")) as unknown;
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function stringField(obj: Record<string, unknown>, key: string): string | undefined {
  const value = obj[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function stringArrayField(obj: Record<string, unknown>, key: string): string[] {
  const value = obj[key];
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && item.trim() !== "");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function titleFromMarkdown(content: string): string | undefined {
  const heading = content.split(/\r?\n/).find((line) => line.startsWith("# "));
  if (!heading) return undefined;
  return heading.replace(/^#\s+/, "").replace(/\s+[—-]\s+.*$/, "").trim();
}

function descriptionFromMarkdown(content: string): string {
  const lines = content.split(/\r?\n/);
  let sawHeading = false;
  const paragraph: string[] = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (!sawHeading) {
      sawHeading = trimmed.startsWith("# ");
      continue;
    }
    if (!trimmed || trimmed === "---") {
      if (paragraph.length > 0) break;
      continue;
    }
    if (trimmed.startsWith("#")) break;
    paragraph.push(trimmed);
  }
  const text = paragraph.join(" ").trim();
  return text.length > 260 ? `${text.slice(0, 257)}...` : text;
}

function titleFromId(id: string): string {
  return id
    .split("-")
    .filter(Boolean)
    .map((part) => part[0]?.toUpperCase() + part.slice(1))
    .join(" ");
}

function tagsFromId(id: string): string[] {
  return id.split("-").filter(Boolean);
}
