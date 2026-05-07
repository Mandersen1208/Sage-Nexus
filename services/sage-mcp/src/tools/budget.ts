import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const BUDGET_API_URL = process.env.BUDGET_API_URL ?? "http://host.docker.internal:3001";

async function budgetGet(path: string): Promise<unknown> {
  const url = `${BUDGET_API_URL}${path}`;
  console.error(`[budget] GET ${url}`);

  let resp: Response;
  try {
    resp = await fetch(url, {
      headers: {
        "Accept": "application/json",
        "User-Agent": "sage-mcp/1.0",
      },
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    throw new Error(`Budget API request failed: ${err instanceof Error ? err.message : String(err)}`);
  }

  if (!resp.ok) {
    throw new Error(`Budget API returned HTTP ${resp.status}`);
  }

  return resp.json();
}

function formatJSON(data: unknown): string {
  return JSON.stringify(data, null, 2);
}

const MONTH_NAMES: Record<string, string> = {
  January: "01", February: "02", March: "03", April: "04",
  May: "05", June: "06", July: "07", August: "08",
  September: "09", October: "10", November: "11", December: "12",
};

const MONTH_NAMES_LOWER = [
  "january", "february", "march", "april", "may", "june",
  "july", "august", "september", "october", "november", "december",
];

function systemMonthKey(): string {
  const now = new Date();
  return `${MONTH_NAMES_LOWER[now.getMonth()]}-${now.getFullYear()}`;
}

// Resolves the "current month" id — prefers the app's explicit default, falls
// back to matching the system clock's month_key against the months list.
async function currentMonthId(): Promise<string | number | null> {
  try {
    const current = (await budgetGet(`/api/months/defaults/current`)) as { id?: string | number };
    if (current?.id !== undefined && current.id !== null) {
      return current.id;
    }
  } catch (e) {
    console.error(`[budget] defaults/current failed: ${e instanceof Error ? e.message : String(e)}`);
  }
  // Fallback: match system clock's month_key in the months list.
  const target = systemMonthKey();
  try {
    const months = (await budgetGet(`/api/months`)) as Array<{ id: string | number; month_key?: string }>;
    const hit = months.find((m) => m.month_key === target);
    if (hit) {
      console.error(`[budget] resolved current month via system clock → ${target} (id=${hit.id})`);
      return hit.id;
    }
    console.error(`[budget] no month matches system clock key ${target}; available: ${months.map((m) => m.month_key).join(", ")}`);
  } catch (e) {
    console.error(`[budget] months list lookup failed: ${e instanceof Error ? e.message : String(e)}`);
  }
  return null;
}

function systemYYYYMM(): string {
  const now = new Date();
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  return `${y}-${m}`;
}

async function currentYYYYMM(): Promise<string> {
  const id = await currentMonthId();
  if (id !== null) {
    try {
      const month = (await budgetGet(`/api/months/${encodeURIComponent(String(id))}`)) as {
        year?: number;
        month_name?: string;
      };
      if (month.year && month.month_name && MONTH_NAMES[month.month_name]) {
        return `${month.year}-${MONTH_NAMES[month.month_name]}`;
      }
    } catch (e) {
      console.error(`[budget] month detail lookup failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  }
  const fallback = systemYYYYMM();
  console.error(`[budget] using system-clock YYYY-MM ${fallback}`);
  return fallback;
}

export function registerBudgetTools(server: McpServer): void {
  server.tool(
    "budget_get_current_month",
    "Fetch the current month's full budget: income, bank, line items with planned vs actual. Read-only. Use this first for any question about how spending is going right now.",
    {},
    async () => {
      try {
        const id = await currentMonthId();
        if (id === null) {
          return {
            content: [{ type: "text", text: `No current month is set in the budget app and none matches the system clock month (${systemMonthKey()}). Call budget_list_months to see what exists, or ask the user which month they mean.` }],
          };
        }
        const month = await budgetGet(`/api/months/${encodeURIComponent(String(id))}`);
        return { content: [{ type: "text", text: formatJSON(month) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_get_current_month] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_get_current_month error: ${msg}` }],
          isError: true,
        };
      }
    }
  );

  server.tool(
    "budget_list_months",
    "List all budget months the user has on record (id, label, dates). Read-only. Use this to find historical months to compare against.",
    {},
    async () => {
      try {
        const months = await budgetGet(`/api/months`);
        return { content: [{ type: "text", text: formatJSON(months) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_list_months] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_list_months error: ${msg}` }],
          isError: true,
        };
      }
    }
  );

  server.tool(
    "budget_get_month",
    "Fetch a specific month's full budget by id. Read-only. Use this to look at historical or future months after finding the id via budget_list_months.",
    {
      month_id: z.union([z.string(), z.number()]).describe("The month id returned by budget_list_months"),
    },
    async ({ month_id }) => {
      try {
        const month = await budgetGet(`/api/months/${encodeURIComponent(String(month_id))}`);
        return { content: [{ type: "text", text: formatJSON(month) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_get_month] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_get_month error: ${msg}` }],
          isError: true,
        };
      }
    }
  );

  server.tool(
    "budget_get_month_transactions",
    "Fetch the full raw transaction list for a budget month (date, description, amount, category, account). Read-only. Amounts are negative for debits, positive for credits. Use this to explain WHY a category is over or to answer 'where did I spend on X'.",
    {
      month_id: z.union([z.string(), z.number()]).describe("The month id from budget_list_months, or the id from budget_get_current_month"),
    },
    async ({ month_id }) => {
      try {
        const txns = await budgetGet(`/api/sophtron/transactions/${encodeURIComponent(String(month_id))}`);
        return { content: [{ type: "text", text: formatJSON(txns) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_get_month_transactions] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_get_month_transactions error: ${msg}` }],
          isError: true,
        };
      }
    }
  );

  server.tool(
    "budget_get_spend_by_category",
    "Fetch ACTUAL spend totals grouped by category for a month (excludes Transfer/Income/Afterpay). Read-only. This is the canonical source of actuals — budget_get_current_month only has planned amounts. Use this to compare planned vs actual.",
    {
      month: z.string().optional().describe("YYYY-MM (e.g. '2026-04'). If omitted, defaults to the current month."),
    },
    async ({ month }) => {
      try {
        const m = month ?? (await currentYYYYMM());
        const data = await budgetGet(`/api/sophtron/spend-by-category?month=${encodeURIComponent(m)}`);
        return { content: [{ type: "text", text: formatJSON(data) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_get_spend_by_category] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_get_spend_by_category error: ${msg}` }],
          isError: true,
        };
      }
    }
  );

  server.tool(
    "budget_get_transactions_by_category",
    "Fetch all transactions for a specific category in a given month. Read-only. Use as a follow-up to budget_get_spend_by_category when the user wants to see the individual transactions behind a category total.",
    {
      category: z.string().describe("Category name, e.g. 'Out to Eat', 'Groceries' (must match the category used in the budget app)"),
      month: z.string().optional().describe("YYYY-MM (e.g. '2026-04'). If omitted, defaults to the current month."),
    },
    async ({ category, month }) => {
      try {
        const m = month ?? (await currentYYYYMM());
        const qs = `category=${encodeURIComponent(category)}&month=${encodeURIComponent(m)}`;
        const txns = await budgetGet(`/api/sophtron/transactions-by-category?${qs}`);
        return { content: [{ type: "text", text: formatJSON(txns) }] };
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[budget_get_transactions_by_category] Error:`, msg);
        return {
          content: [{ type: "text", text: `budget_get_transactions_by_category error: ${msg}` }],
          isError: true,
        };
      }
    }
  );
}
