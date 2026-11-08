import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { REMINDER_COUNTS_SQL } from "../src/health.ts";
import { PENDING_SQL, SWEEP_SQL } from "../src/reminders.ts";

async function plan(sql: string): Promise<string[]> {
  const params = sql.match(/\?\d+/g) ?? [];
  const highest = Math.max(0, ...params.map((p) => Number(p.slice(1))));
  const { results } = await env.DB.prepare(`EXPLAIN QUERY PLAN ${sql}`)
    .bind(...Array.from({ length: highest }, () => "2026-11-08T00:00:00.000Z"))
    .all<{ detail: string }>();
  return results.map((r) => r.detail);
}

const scansReminders = (detail: string) => /^SCAN reminders\b/.test(detail) && !/USING (COVERING )?INDEX/.test(detail);

describe("queries that run on every tick or public request search an index instead of scanning reminders", () => {
  it.each([
    ["the stored reminder counts", REMINDER_COUNTS_SQL],
    ["the pending check", PENDING_SQL],
    ...SWEEP_SQL.map((sql, i) => [`sweep ${i + 1}`, sql] as [string, string]),
  ])("%s", async (_name, sql) => {
    const details = await plan(sql);
    expect(details.length).toBeGreaterThan(0);
    expect(details.filter(scansReminders)).toEqual([]);
  });
});
