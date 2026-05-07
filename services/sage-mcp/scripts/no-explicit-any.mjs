import { promises as fs } from "node:fs";
import path from "node:path";

const root = process.cwd();
const targets = [
  path.join(root, "src"),
  path.join(root, "tests"),
];

const exts = new Set([".ts", ".tsx"]);
const offenders = [];

const patterns = [
  { label: "type annotation", regex: /:\s*any\b/g },
  { label: "type cast", regex: /\bas\s+any\b/g },
  { label: "generic any", regex: /<\s*any\s*>/g },
  { label: "array any", regex: /\bany\[\]/g },
];

async function walk(dir) {
  let entries;
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      await walk(full);
      continue;
    }
    if (!entry.isFile()) continue;
    if (!exts.has(path.extname(entry.name))) continue;
    await checkFile(full);
  }
}

async function checkFile(file) {
  const text = await fs.readFile(file, "utf8");
  const lines = text.split(/\r?\n/);
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (!line) continue;
    for (const pattern of patterns) {
      pattern.regex.lastIndex = 0;
      if (!pattern.regex.test(line)) continue;
      offenders.push({
        file: path.relative(root, file),
        line: i + 1,
        label: pattern.label,
        text: line.trim(),
      });
    }
  }
}

for (const dir of targets) {
  await walk(dir);
}

if (offenders.length > 0) {
  process.stderr.write("Explicit `any` usage found:\n");
  for (const offender of offenders) {
    process.stderr.write(
      ` - ${offender.file}:${offender.line} (${offender.label}) ${offender.text}\n`,
    );
  }
  process.exit(1);
}

process.stdout.write("No explicit `any` usage found.\n");
