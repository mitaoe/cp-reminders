import { env } from "cloudflare:workers";
import { beforeAll, describe, expect, it } from "vitest";
import { isoUtc } from "../src/core/format.ts";
import { COUNTS_KEY, readHealth, storeCountsStatement } from "../src/health.ts";
import { allMetaStatement } from "../src/meta.ts";
import { LEASE_MS, MAX_ATTEMPTS, ackStatement, claimStatement, pendingStatement, sweepStatements } from "../src/reminders.ts";
import { MIN, NOW, resetDb, seedReminder } from "./helpers.ts";

const HISTORY = 300;
const SMALL = 20;
const DAY = 24 * 60 * MIN;

async function seedHistory() {
  const insert = (key: string, status: string, updatedMs: number) =>
    env.DB.prepare(
      "INSERT INTO reminders (key, resource_id, href, start_utc, deadline_utc, text, status, created_at, updated_at) " +
        "VALUES (?1, 2, ?2, ?3, ?4, 't', ?5, ?6, ?6)",
    ).bind(key, `https://example.test/${key}`, isoUtc(updatedMs + 10 * MIN), isoUtc(updatedMs), status, isoUtc(updatedMs));
  const rows = [];
  for (let i = 0; i < HISTORY; i++) rows.push(insert(`sent-${i}`, "sent", NOW - 2 * DAY - i * 60 * MIN));
  for (let i = 0; i < HISTORY / 3; i++) rows.push(insert(`failed-${i}`, "failed", NOW - 2 * DAY - i * 60 * MIN));
  for (let i = 0; i < HISTORY / 3; i++) rows.push(insert(`expired-${i}`, "expired", NOW - 2 * DAY - i * 60 * MIN));
  await env.DB.batch(rows);
  await seedReminder({ key: "queued" });
  await seedReminder({ key: "claimed", status: "claimed", attempts: 1, claim_id: "c", claimed_at: isoUtc(NOW - 2 * LEASE_MS) });
  await seedReminder({ key: "failed-recent", status: "failed", updated_at: isoUtc(NOW - 60 * MIN) });
  await seedReminder({ key: "expired-recent", status: "expired", updated_at: isoUtc(NOW - 60 * MIN) });
}

const rowsRead = async (statement: D1PreparedStatement) => (await statement.run()).meta.rows_read;

describe("statements that run on every tick, send run or public request read a bounded number of rows", () => {
  beforeAll(async () => {
    await resetDb();
    await seedHistory();
  });

  it("the table holds the history the bounds are measured against", async () => {
    const row = await env.DB.prepare("SELECT COUNT(*) AS n FROM reminders").first<{ n: number }>();
    expect(row?.n).toBeGreaterThan(HISTORY);
  });

  it.each([
    ["the stored reminder counts", () => storeCountsStatement(env.DB, NOW)],
    ["the pending check", () => pendingStatement(env.DB, NOW)],
    ["the meta read behind /health and /badge", () => allMetaStatement(env.DB)],
    ...sweepStatements(env.DB, NOW).map((s, i) => [`sweep ${i + 1}`, () => s] as [string, () => D1PreparedStatement]),
    ["the claim", () => claimStatement(env.DB, "probe-claim", NOW)],
    ["an acknowledgement", () => ackStatement(env.DB, "c", { key: "claimed", status: "failed", error: "probe" }, isoUtc(NOW), MAX_ATTEMPTS)],
  ])("%s", async (_name, statement) => {
    expect(await rowsRead(statement())).toBeLessThan(SMALL);
  });
});

describe("migration 0002", () => {
  it("stores the counts of the reminders already in the table", async () => {
    await resetDb();
    await seedReminder({ key: "q" });
    await seedReminder({ key: "f", status: "failed", updated_at: isoUtc(Date.now() - 60 * MIN) });
    await seedReminder({ key: "f-old", status: "failed", updated_at: isoUtc(Date.now() - 2 * DAY) });
    const migration = env.TEST_MIGRATIONS.find((m) => m.name.startsWith("0002"));
    const seed = migration?.queries.find((q) => q.includes(COUNTS_KEY));
    expect(seed).toBeDefined();
    await env.DB.prepare(seed!).run();
    expect(await readHealth(env.DB)).toMatchObject({ queued: 1, claimed: 0, failed24h: 1, expired24h: 0 });
  });
});
