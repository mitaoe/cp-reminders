import { env, exports } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BADGE_COLOR, CACHE_CONTROL, DOWN_AFTER_MS, LABEL_COLOR } from "../src/badge.ts";
import { isoUtc } from "../src/core/format.ts";
import { storeCountsStatement } from "../src/health.ts";
import { setMeta } from "../src/meta.ts";
import { MIN, NOW, resetDb, seedReminder } from "./helpers.ts";

const DOWN_MINUTES = DOWN_AFTER_MS / MIN;
const RECENT_MINUTES = Math.max(1, Math.floor(DOWN_MINUTES / 5));

beforeEach(async () => {
  await resetDb();
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
});

afterEach(() => {
  vi.useRealTimers();
});

const badge = () => exports.default.fetch("https://w.test/badge");
const meta = (key: string, value: string) => setMeta(env.DB, key, value).run();
const tickedMinutesAgo = (minutes: number) => meta("last_tick_at", isoUtc(NOW - minutes * MIN));

async function seedCounted(row: Parameters<typeof seedReminder>[0]) {
  await seedReminder(row);
  await storeCountsStatement(env.DB, NOW).run();
}

async function expectBadge(message: string, color: string) {
  expect(await (await badge()).json()).toEqual({ schemaVersion: 1, label: "bot", labelColor: LABEL_COLOR, message, color });
}

describe("GET /badge", () => {
  it("is public and returns a cacheable shields.io endpoint document", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    const res = await badge();
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toMatch(/^application\/json/);
    expect(res.headers.get("Cache-Control")).toBe(CACHE_CONTROL);
    await expectBadge("ok", BADGE_COLOR.ok);
  });

  it("is down when the scheduler never ticked", async () => {
    await expectBadge("never ticked", BADGE_COLOR.failing);
  });

  it("is down when the last tick time cannot be read", async () => {
    await meta("last_tick_at", "not a time");
    await expectBadge("never ticked", BADGE_COLOR.failing);
  });

  it("is down when the last tick is older than the down threshold", async () => {
    await tickedMinutesAgo(DOWN_MINUTES + 8);
    await expectBadge(`no tick for ${DOWN_MINUTES + 8}m`, BADGE_COLOR.failing);
  });

  it("is not down when the last tick is exactly at the down threshold", async () => {
    await tickedMinutesAgo(DOWN_MINUTES);
    await expectBadge("ok", BADGE_COLOR.ok);
  });

  it("is down one second past the down threshold", async () => {
    await meta("last_tick_at", isoUtc(NOW - DOWN_AFTER_MS - 1000));
    await expectBadge(`no tick for ${DOWN_MINUTES}m`, BADGE_COLOR.failing);
  });

  it("is failing when reminders failed or expired in the last 24 hours", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await seedCounted({ key: "f", status: "failed", updated_at: isoUtc(NOW - 60 * MIN) });
    await seedCounted({ key: "e", status: "expired", updated_at: isoUtc(NOW - 23 * 60 * MIN) });
    await seedCounted({ key: "old", status: "failed", updated_at: isoUtc(NOW - 25 * 60 * MIN) });
    await expectBadge("2 missed in 24h", BADGE_COLOR.failing);
  });

  it("reports down before failing", async () => {
    await tickedMinutesAgo(2 * DOWN_MINUTES);
    await seedCounted({ key: "f", status: "failed" });
    await expectBadge(`no tick for ${2 * DOWN_MINUTES}m`, BADGE_COLOR.failing);
  });

  it("is failing when the last dispatch to the sender failed", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401: Bad credentials");
    await expectBadge("dispatch failing", BADGE_COLOR.failing);
  });

  it("reports down before dispatch failing", async () => {
    await tickedMinutesAgo(2 * DOWN_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401");
    await expectBadge(`no tick for ${2 * DOWN_MINUTES}m`, BADGE_COLOR.failing);
  });

  it("reports dispatch failing before missed reminders", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401");
    await seedCounted({ key: "e", status: "expired" });
    await expectBadge("dispatch failing", BADGE_COLOR.failing);
  });

  it("is degraded when the last fetch recorded an error", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500");
    await expectBadge("degraded: clist", BADGE_COLOR.degraded);
  });

  it("is degraded when an upcoming selected contest has no contest link", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("unlinked_contests", "1");
    await expectBadge("degraded: contest link", BADGE_COLOR.degraded);
  });

  it("reports a missing contest link before a failing clist", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("unlinked_contests", "1");
    await meta("last_fetch_error", "clist HTTP 500");
    await expectBadge("degraded: contest link", BADGE_COLOR.degraded);
  });

  it("reports failing before a missing contest link", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("unlinked_contests", "1");
    await seedCounted({ key: "f", status: "failed", last_error: "no_contest_link" });
    await expectBadge("1 missed in 24h", BADGE_COLOR.failing);
  });

  it("reports failing before degraded", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500");
    await seedCounted({ key: "e", status: "expired" });
    await expectBadge("1 missed in 24h", BADGE_COLOR.failing);
  });

  it("never exposes message text, errors or secrets", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500 boom detail");
    await meta("last_wake_error", "dispatch wake detail");
    await seedCounted({ key: "k", text: "TOP SECRET MESSAGE BODY", href: "https://private.test/contest", status: "failed", last_error: "send detail" });
    const raw = await (await badge()).text();
    for (const leaked of ["TOP SECRET", "private.test", "boom detail", "wake detail", "send detail", env.TICK_TOKEN, env.SENDER_TOKEN, env.ADMIN_TOKEN, env.GH_TOKEN, env.CLIST_API_KEY]) {
      expect(raw).not.toContain(leaked);
    }
  });

  it("rejects non-GET methods", async () => {
    expect((await exports.default.fetch("https://w.test/badge", { method: "POST" })).status).toBe(405);
  });
});
