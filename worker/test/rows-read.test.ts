import { env } from "cloudflare:workers";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { isoUtc } from "../src/core/format.ts";
import { selectContests } from "../src/core/select.ts";
import { COUNTS_KEY, readHealth, storeCountsStatement } from "../src/health.ts";
import { allMetaStatement } from "../src/meta.ts";
import {
  LEASE_MS,
  MAX_ATTEMPTS,
  ackStatement,
  claimStatement,
  pendingStatement,
  sweepStatements,
} from "../src/reminders.ts";
import { dueRow, insertStatement, missedRow } from "../src/tick.ts";
import { MIN, NOW, TARGET, fixtureContests, resetDb, seedReminder } from "./helpers.ts";

const MISSED = selectContests(fixtureContests, rules).find((c) => c.href !== TARGET.href)!;

const HISTORY = 300;
const SMALL = 20;
const DAY = 24 * 60 * MIN;
const CLAIM_ID = "probe-claim";

const old = (i: number) => NOW - 2 * DAY - i * 60 * MIN;

const historyRow = (key: string, status: string, updatedMs: number): D1PreparedStatement =>
  env.DB.prepare(
    "INSERT INTO reminders (key, resource_id, href, start_utc, deadline_utc, text, status, created_at, updated_at) " +
      "VALUES (?1, 2, ?2, ?3, ?4, 't', ?5, ?6, ?6)",
  ).bind(key, `https://example.test/${key}`, isoUtc(updatedMs + 10 * MIN), isoUtc(updatedMs), status, isoUtc(updatedMs));

const historyRows = (status: string, count: number) =>
  Array.from({ length: count }, (_, i) => historyRow(`${status}-${i}`, status, old(i)));

async function seedLiveRows() {
  await seedReminder({ key: "queued" });
  await seedReminder({
    key: "lapsed",
    status: "claimed",
    attempts: MAX_ATTEMPTS,
    claim_id: "earlier-claim",
    claimed_at: isoUtc(NOW - 2 * LEASE_MS),
  });
  await seedReminder({ key: "held", status: "claimed", attempts: 1, claim_id: CLAIM_ID, claimed_at: isoUtc(NOW) });
  await seedReminder({ key: "failed-recent", status: "failed", updated_at: isoUtc(NOW - 60 * MIN) });
  await seedReminder({ key: "expired-recent", status: "expired", updated_at: isoUtc(NOW - 60 * MIN) });
}

const hotStatements: [string, () => D1PreparedStatement][] = [
  ["the stored reminder counts", () => storeCountsStatement(env.DB, NOW)],
  ["the pending check", () => pendingStatement(env.DB, NOW)],
  ["the meta read behind /health and /badge", () => allMetaStatement(env.DB)],
  ["the sweep of queued reminders past their deadline", () => sweepStatements(env.DB, NOW)[0]!],
  ["the sweep of lapsed claims past their deadline", () => sweepStatements(env.DB, NOW)[1]!],
  ["the sweep of lapsed claims out of attempts", () => sweepStatements(env.DB, NOW)[2]!],
  ["the claim", () => claimStatement(env.DB, CLAIM_ID, NOW)],
  ["an acknowledgement", () => ackStatement(env.DB, CLAIM_ID, { key: "held", status: "failed", error: "probe" }, NOW)],
  ["queueing a due reminder", () => insertStatement(env.DB, TARGET, dueRow(TARGET, tipRules, baseOverrides), NOW)],
  ["recording a missed window", () => insertStatement(env.DB, MISSED, missedRow(MISSED), NOW)],
  ["recording a missed window again", () => insertStatement(env.DB, MISSED, missedRow(MISSED), NOW)],
];

const run = async (statement: D1PreparedStatement) => (await statement.run()).meta;

describe("statements that run on every tick, send run or public request read a bounded number of rows", () => {
  describe("against a history of every final status", () => {
    beforeAll(async () => {
      await resetDb();
      await env.DB.batch([
        ...historyRows("sent", HISTORY),
        ...historyRows("failed", HISTORY / 3),
        ...historyRows("expired", HISTORY / 3),
      ]);
      await seedLiveRows();
    });

    it.each(hotStatements)("%s", async (_name, statement) => {
      expect((await run(statement())).rows_read).toBeLessThan(SMALL);
    });
  });

  describe("against a history of sent reminders only, with the planner's statistics gathered over it", () => {
    beforeAll(async () => {
      await resetDb();
      await env.DB.batch(historyRows("sent", HISTORY));
      await env.DB.prepare("ANALYZE").run();
      await seedLiveRows();
    });

    afterAll(async () => {
      await env.DB.batch([env.DB.prepare("DELETE FROM sqlite_stat1"), env.DB.prepare("ANALYZE sqlite_schema")]);
    });

    it("the statistics describe the history", async () => {
      const stat = await env.DB.prepare("SELECT stat FROM sqlite_stat1 WHERE idx = 'reminders_by_status'").first<{
        stat: string;
      }>();
      expect(stat?.stat.split(" ")[1]).toBe(String(HISTORY));
    });

    it.each(hotStatements)("%s", async (_name, statement) => {
      expect((await run(statement())).rows_read).toBeLessThan(SMALL);
    });
  });

  describe("the statements do the work they are measured on", () => {
    beforeAll(async () => {
      await resetDb();
      await seedLiveRows();
    });

    it("the sweep of lapsed claims out of attempts fails the lapsed claim", async () => {
      expect((await run(sweepStatements(env.DB, NOW)[2]!)).changes).toBe(1);
    });

    it("recording a missed window inserts its row once", async () => {
      expect((await run(insertStatement(env.DB, MISSED, missedRow(MISSED), NOW))).changes).toBe(1);
      expect((await run(insertStatement(env.DB, MISSED, missedRow(MISSED), NOW))).changes).toBe(0);
    });

    it("the claim takes the queued reminder", async () => {
      expect((await run(claimStatement(env.DB, CLAIM_ID, NOW))).changes).toBe(1);
    });

    it("the acknowledgement updates the reminder its claim holds", async () => {
      const ack = ackStatement(env.DB, CLAIM_ID, { key: "held", status: "failed", error: "probe" }, NOW);
      expect((await run(ack)).changes).toBe(1);
    });
  });
});

describe("migration 0002", () => {
  const migration = () => env.TEST_MIGRATIONS.find((m) => m.name.startsWith("0002"));

  it("stores the counts of the reminders present when it runs", async () => {
    await resetDb();
    await seedReminder({ key: "q" });
    await seedReminder({ key: "f", status: "failed", updated_at: isoUtc(Date.now() - 60 * MIN) });
    await seedReminder({ key: "f-old", status: "failed", updated_at: isoUtc(Date.now() - 2 * DAY) });
    const seed = migration()?.queries.find((q) => q.includes(COUNTS_KEY));
    expect(seed).toBeDefined();
    await env.DB.prepare(seed!).run();
    expect(await readHealth(env.DB)).toMatchObject({ queued: 1, claimed: 0, failed24h: 1, expired24h: 0 });
  });

  it("leaves the planner's statistics alone", () => {
    expect(migration()?.queries.some((q) => /\b(ANALYZE|PRAGMA\s+optimize)\b/i.test(q))).toBe(false);
  });
});
