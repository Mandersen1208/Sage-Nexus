import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Document, HeadingLevel, Packer, Paragraph, Table, TableCell, TableRow, TextRun, WidthType } from "docx";
import ExcelJS from "exceljs";
import fs from "fs";
import path from "path";
import { z } from "zod";

const CellValueSchema = z.union([z.string(), z.number(), z.boolean(), z.null()]);

const TableSchema = z.object({
  headers: z.array(z.string()).optional(),
  rows: z.array(z.array(CellValueSchema)).default([]),
}).strict();

const DocxSectionSchema = z.object({
  heading: z.string().optional(),
  paragraphs: z.array(z.string()).optional(),
  bullets: z.array(z.string()).optional(),
  table: TableSchema.optional(),
}).strict();

const WorkbookSheetSchema = z.object({
  name: z.string().min(1).max(31),
  columns: z.array(z.string()).optional(),
  rows: z.array(z.array(CellValueSchema)).default([]),
}).strict();

const DocxInputSchema = z.object({
  title: z.string().optional(),
  filename: z.string().optional(),
  sections: z.array(DocxSectionSchema).min(1),
  metadata: z.record(z.unknown()).optional(),
}).strict();

const XlsxInputSchema = z.object({
  filename: z.string().optional(),
  sheets: z.array(WorkbookSheetSchema).min(1),
  metadata: z.record(z.unknown()).optional(),
}).strict();

const CalendarAttendeeSchema = z.object({
  name: z.string().optional(),
  email: z.string().email(),
}).strict();

const CalendarEventSchema = z.object({
  title: z.string().min(1),
  description: z.string().optional(),
  location: z.string().optional(),
  start: z.string().min(1),
  end: z.string().min(1),
  allDay: z.boolean().optional(),
  url: z.string().url().optional(),
  attendees: z.array(CalendarAttendeeSchema).optional(),
}).strict();

const IcsInputSchema = z.object({
  filename: z.string().optional(),
  calendarName: z.string().optional(),
  events: z.array(CalendarEventSchema).min(1),
  metadata: z.record(z.unknown()).optional(),
}).strict();

type CellValue = z.infer<typeof CellValueSchema>;
type DocxInput = z.infer<typeof DocxInputSchema>;
type XlsxInput = z.infer<typeof XlsxInputSchema>;
type IcsInput = z.infer<typeof IcsInputSchema>;
type CalendarEvent = z.infer<typeof CalendarEventSchema>;

function artifactsRoot(): string {
  return path.resolve(process.env["SAGE_ARTIFACTS_DIR"] ?? "/sage-state/workspace/artifacts");
}

function safeFilename(input: string | undefined, fallbackBase: string, ext: ".docx" | ".xlsx" | ".ics"): string {
  const fallback = `${fallbackBase}-${new Date().toISOString().replace(/[:.]/g, "-")}${ext}`;
  const raw = (input ?? fallback).trim() || fallback;
  const base = path.basename(raw).replace(/[<>:"/\\|?*\x00-\x1F]/g, "-");
  return base.toLowerCase().endsWith(ext) ? base : `${base}${ext}`;
}

function resolveArtifactPath(filename: string): string {
  const root = artifactsRoot();
  const target = path.resolve(root, filename);
  if (target !== root && !target.startsWith(`${root}${path.sep}`)) {
    throw new Error("artifact path escapes configured artifact directory");
  }
  fs.mkdirSync(root, { recursive: true });
  return target;
}

function textCell(value: CellValue): string {
  if (value === null || value === undefined) return "";
  return String(value);
}

function docxTable(table: z.infer<typeof TableSchema>): Table {
  const rows: TableRow[] = [];
  if (table.headers && table.headers.length > 0) {
    rows.push(new TableRow({
      children: table.headers.map((header) => new TableCell({
        children: [new Paragraph({ children: [new TextRun({ text: header, bold: true })] })],
      })),
    }));
  }
  for (const row of table.rows) {
    rows.push(new TableRow({
      children: row.map((value) => new TableCell({
        children: [new Paragraph(textCell(value))],
      })),
    }));
  }
  return new Table({
    width: { size: 100, type: WidthType.PERCENTAGE },
    rows,
  });
}

export async function createDocxArtifact(input: DocxInput): Promise<Record<string, unknown>> {
  const filename = safeFilename(input.filename, "sage-document", ".docx");
  const outputPath = resolveArtifactPath(filename);
  const children: Array<Paragraph | Table> = [];

  if (input.title) {
    children.push(new Paragraph({ text: input.title, heading: HeadingLevel.TITLE }));
  }
  for (const section of input.sections) {
    if (section.heading) {
      children.push(new Paragraph({ text: section.heading, heading: HeadingLevel.HEADING_1 }));
    }
    for (const paragraph of section.paragraphs ?? []) {
      children.push(new Paragraph(paragraph));
    }
    for (const bullet of section.bullets ?? []) {
      children.push(new Paragraph({ text: bullet, bullet: { level: 0 } }));
    }
    if (section.table) {
      children.push(docxTable(section.table));
    }
  }

  const doc = new Document({
    creator: "Sage Nexus",
    title: input.title ?? filename,
    sections: [{ properties: {}, children }],
  });
  const buffer = await Packer.toBuffer(doc);
  fs.writeFileSync(outputPath, buffer);
  return {
    path: outputPath,
    filename,
    mime_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    sections: input.sections.length,
    bytes: buffer.length,
    metadata: input.metadata ?? {},
  };
}

function safeSheetName(name: string, index: number): string {
  const cleaned = name.replace(/[:\\/?*\[\]]/g, "-").trim();
  return (cleaned || `Sheet ${index + 1}`).slice(0, 31);
}

export async function createXlsxArtifact(input: XlsxInput): Promise<Record<string, unknown>> {
  const filename = safeFilename(input.filename, "sage-workbook", ".xlsx");
  const outputPath = resolveArtifactPath(filename);
  const workbook = new ExcelJS.Workbook();
  workbook.creator = "Sage Nexus";
  workbook.created = new Date();

  input.sheets.forEach((sheet, index) => {
    const worksheet = workbook.addWorksheet(safeSheetName(sheet.name, index));
    if (sheet.columns && sheet.columns.length > 0) {
      const header = worksheet.addRow(sheet.columns);
      header.font = { bold: true };
    }
    for (const row of sheet.rows) {
      worksheet.addRow(row.map((value) => value ?? ""));
    }
    worksheet.columns.forEach((column) => {
      let width = 12;
      column.eachCell?.({ includeEmpty: true }, (cell) => {
        width = Math.max(width, String(cell.value ?? "").length + 2);
      });
      column.width = Math.min(width, 48);
    });
  });

  await workbook.xlsx.writeFile(outputPath);
  const stat = fs.statSync(outputPath);
  return {
    path: outputPath,
    filename,
    mime_type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    sheets: input.sheets.map((sheet) => sheet.name),
    bytes: stat.size,
    metadata: input.metadata ?? {},
  };
}

export function createIcsArtifact(input: IcsInput): Record<string, unknown> {
  const filename = safeFilename(input.filename, "sage-calendar", ".ics");
  const outputPath = resolveArtifactPath(filename);
  const now = formatIcsDateTime(new Date());
  const events = input.events.map((event, index) => formatIcsEvent(event, now, index));
  const calendar = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Sage Nexus//Office Document Agent//EN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    input.calendarName ? foldIcsLine(`X-WR-CALNAME:${escapeIcsText(input.calendarName)}`) : undefined,
    ...events,
    "END:VCALENDAR",
    "",
  ].filter((line): line is string => Boolean(line)).join("\r\n");

  fs.writeFileSync(outputPath, calendar, "utf8");
  const stat = fs.statSync(outputPath);
  return {
    path: outputPath,
    filename,
    mime_type: "text/calendar",
    events: input.events.map((event) => event.title),
    bytes: stat.size,
    metadata: input.metadata ?? {},
  };
}

export function listOfficeArtifacts(): Record<string, unknown> {
  const root = artifactsRoot();
  if (!fs.existsSync(root)) return { root, count: 0, artifacts: [] };
  const artifacts = fs.readdirSync(root)
    .filter((name) => name.endsWith(".docx") || name.endsWith(".xlsx") || name.endsWith(".ics"))
    .map((name) => {
      const fullPath = path.join(root, name);
      const stat = fs.statSync(fullPath);
      return { filename: name, path: fullPath, bytes: stat.size, modified_at: stat.mtime.toISOString() };
    })
    .sort((a, b) => b.modified_at.localeCompare(a.modified_at));
  return { root, count: artifacts.length, artifacts };
}

function formatIcsEvent(event: CalendarEvent, now: string, index: number): string {
  const start = parseCalendarDate(event.start, "start");
  const end = parseCalendarDate(event.end, "end");
  if (end.getTime() <= start.getTime()) {
    throw new Error(`calendar event "${event.title}" end must be after start`);
  }

  const lines = [
    "BEGIN:VEVENT",
    foldIcsLine(`UID:${createEventUid(event, index)}`),
    foldIcsLine(`DTSTAMP:${now}`),
    foldIcsLine(`${event.allDay ? "DTSTART;VALUE=DATE" : "DTSTART"}:${event.allDay ? formatIcsDate(start) : formatIcsDateTime(start)}`),
    foldIcsLine(`${event.allDay ? "DTEND;VALUE=DATE" : "DTEND"}:${event.allDay ? formatIcsDate(end) : formatIcsDateTime(end)}`),
    foldIcsLine(`SUMMARY:${escapeIcsText(event.title)}`),
  ];
  if (event.description) lines.push(foldIcsLine(`DESCRIPTION:${escapeIcsText(event.description)}`));
  if (event.location) lines.push(foldIcsLine(`LOCATION:${escapeIcsText(event.location)}`));
  if (event.url) lines.push(foldIcsLine(`URL:${event.url}`));
  for (const attendee of event.attendees ?? []) {
    const commonName = attendee.name ? `;CN=${escapeIcsParam(attendee.name)}` : "";
    lines.push(foldIcsLine(`ATTENDEE${commonName}:mailto:${attendee.email}`));
  }
  lines.push("END:VEVENT");
  return lines.join("\r\n");
}

function parseCalendarDate(value: string, field: string): Date {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`calendar ${field} must be an ISO-8601 date or datetime`);
  }
  return date;
}

function formatIcsDateTime(date: Date): string {
  return date.toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");
}

function formatIcsDate(date: Date): string {
  return date.toISOString().slice(0, 10).replace(/-/g, "");
}

function createEventUid(event: CalendarEvent, index: number): string {
  const seed = `${event.title}-${event.start}-${event.end}-${index}`;
  const safe = seed.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 64);
  return `${safe || `event-${index + 1}`}@sage-nexus.local`;
}

function escapeIcsText(value: string): string {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/\n/g, "\\n")
    .replace(/,/g, "\\,")
    .replace(/;/g, "\\;");
}

function escapeIcsParam(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")}"`;
}

function foldIcsLine(line: string): string {
  const limit = 75;
  if (line.length <= limit) return line;
  const chunks: string[] = [line.slice(0, limit)];
  let rest = line.slice(limit);
  while (rest.length > 0) {
    chunks.push(` ${rest.slice(0, limit - 1)}`);
    rest = rest.slice(limit - 1);
  }
  return chunks.join("\r\n");
}

export function registerOfficeTools(server: McpServer): void {
  server.registerTool(
    "office_docx_create",
    {
      title: "Create Word DOCX Artifact",
      description: "Create a Microsoft Word .docx artifact under the configured Sage artifact directory. Use for formatted documents, reports, notes, and handoff docs.",
      inputSchema: DocxInputSchema,
      annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: false },
    },
    async (input) => {
      const output = await createDocxArtifact(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "office_xlsx_create",
    {
      title: "Create Excel XLSX Artifact",
      description: "Create a Microsoft Excel .xlsx workbook under the configured Sage artifact directory. Use for spreadsheets, tables, simple reports, and workbook outputs.",
      inputSchema: XlsxInputSchema,
      annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: false },
    },
    async (input) => {
      const output = await createXlsxArtifact(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "office_artifact_list",
    {
      title: "List Office Artifacts",
      description: "List DOCX, XLSX, and ICS artifacts in the configured Sage artifact directory.",
      inputSchema: z.object({}).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async () => {
      const output = listOfficeArtifacts();
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );

  server.registerTool(
    "office_ics_create",
    {
      title: "Create Calendar ICS Artifact",
      description: "Create a saveable .ics calendar artifact under the configured Sage artifact directory. Use for reminders, appointments, meetings, due dates, and importable calendar events.",
      inputSchema: IcsInputSchema,
      annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: false },
    },
    async (input) => {
      const output = createIcsArtifact(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );
}
