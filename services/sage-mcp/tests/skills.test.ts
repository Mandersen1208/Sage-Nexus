import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { loadSkillRegistry } from "../src/constants.js";
import { getSkillContent, listSkills, searchSkills } from "../src/services/skills.js";

function write(filePath: string, content: string): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, content, "utf-8");
}

const root = fs.mkdtempSync(path.join(os.tmpdir(), "sage-skills-"));
process.env["SKILLS_ROOT"] = root;

write(path.join(root, "alpha-skill", "SKILL.md"), `# Alpha Skill

Alpha dynamic skill description.
`);

write(path.join(root, "agent-setup", "metadata.json"), JSON.stringify({
  id: "agent-setup",
  name: "Agent Setup",
  description: "Create Sage agents safely.",
  tags: ["sage", "agent"],
}, null, 2));
write(path.join(root, "agent-setup", "SKILL.md"), "# Agent Setup\n\nUse for adding agents.");

const registry = loadSkillRegistry();
assert.equal(Object.keys(registry).length, 2);
assert.equal(registry["alpha-skill"]?.name, "Alpha Skill");
assert.equal(registry["agent-setup"]?.description, "Create Sage agents safely.");

const listed = listSkills();
assert.equal(listed.some((skill) => skill.id === "agent-setup"), true);
assert.equal(searchSkills("agent").some((skill) => skill.id === "agent-setup"), true);

const content = getSkillContent("alpha-skill");
assert.equal(content?.content.includes("Alpha dynamic skill description."), true);

console.log("skill registry tests passed");
