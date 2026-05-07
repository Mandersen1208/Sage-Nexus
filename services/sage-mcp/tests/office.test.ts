import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createDocxArtifact, createIcsArtifact, createXlsxArtifact, listOfficeArtifacts } from "../src/tools/office.js";

const root = fs.mkdtempSync(path.join(os.tmpdir(), "sage-office-tools-"));
process.env["SAGE_ARTIFACTS_DIR"] = root;

const docx = await createDocxArtifact({
  title: "Sage Test Document",
  filename: "handoff",
  sections: [
    {
      heading: "Summary",
      paragraphs: ["Generated from the Office document agent tool."],
      bullets: ["DOCX artifact", "Safe artifact directory"],
      table: {
        headers: ["Name", "Value"],
        rows: [["status", "ok"]],
      },
    },
  ],
});

assert.equal(docx["filename"], "handoff.docx");
assert.equal(fs.existsSync(docx["path"] as string), true);
assert.equal(path.dirname(docx["path"] as string), root);
assert.ok((docx["bytes"] as number) > 0);

const xlsx = await createXlsxArtifact({
  filename: "budget.xlsx",
  sheets: [
    {
      name: "Summary",
      columns: ["Category", "Amount"],
      rows: [["Groceries", 42.5]],
    },
  ],
});

assert.equal(xlsx["filename"], "budget.xlsx");
assert.equal(fs.existsSync(xlsx["path"] as string), true);
assert.equal(path.dirname(xlsx["path"] as string), root);
assert.ok((xlsx["bytes"] as number) > 0);

const ics = createIcsArtifact({
  filename: "doctor-visit",
  calendarName: "Sage Test Calendar",
  events: [
    {
      title: "Doctor visit",
      description: "Bring paperwork",
      location: "Clinic",
      start: "2026-05-12T14:00:00-04:00",
      end: "2026-05-12T14:30:00-04:00",
      attendees: [{ name: "Matt", email: "matt@example.com" }],
    },
  ],
});

assert.equal(ics["filename"], "doctor-visit.ics");
assert.equal(fs.existsSync(ics["path"] as string), true);
assert.equal(path.dirname(ics["path"] as string), root);
const icsBody = fs.readFileSync(ics["path"] as string, "utf8");
assert.match(icsBody, /BEGIN:VCALENDAR/);
assert.match(icsBody, /BEGIN:VEVENT/);
assert.match(icsBody, /SUMMARY:Doctor visit/);
assert.match(icsBody, /DTSTART:20260512T180000Z/);
assert.match(icsBody, /ATTENDEE;CN="Matt":mailto:matt@example.com/);
assert.ok((ics["bytes"] as number) > 0);

const list = listOfficeArtifacts();
assert.equal(list["count"], 3);

console.log("office tool tests passed");
