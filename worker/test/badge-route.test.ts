import { env, exports } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BADGE_COLOR, CACHE_CONTROL, DOWN_AFTER_MS, LABEL_COLOR } from "../src/badge.ts";
import { isoUtc } from "../src/core/format.ts";
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
const badgeBody = async () => (await badge()).json();
const meta = (key: string, value: string) => setMeta(env.DB, key, value).run();
const tickedMinutesAgo = (minutes: number) => meta("last_tick_at", isoUtc(NOW - minutes * MIN));

const failing = (message: string) => ({ message, color: BADGE_COLOR.failing });

describe("GET /badge", () => {
  it("is public and returns a cacheable shields.io endpoint document in GitHub's badge colours", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    const res = await badge();
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toMatch(/^application\/json/);
    expect(res.headers.get("Cache-Control")).toBe(CACHE_CONTROL);
    expect(await res.json()).toEqual({ schemaVersion: 1, label: "bot", labelColor: LABEL_COLOR, message: "ok", color: BADGE_COLOR.ok });
  });

  it("uses the label colour in every state", async () => {
    expect(await badgeBody()).toMatchObject({ labelColor: LABEL_COLOR });
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500");
    expect(await badgeBody()).toMatchObject({ labelColor: LABEL_COLOR });
  });

  it("is down when the scheduler never ticked", async () => {
    expect(await badgeBody()).toMatchObject(failing("never ticked"));
  });

  it("is down when the last tick is older than the down threshold", async () => {
    await tickedMinutesAgo(DOWN_MINUTES + 8);
    expect(await badgeBody()).toMatchObject(failing(`no tick for ${DOWN_MINUTES + 8}m`));
  });

  it("is not down when the last tick is exactly at the down threshold", async () => {
    await tickedMinutesAgo(DOWN_MINUTES);
    expect(await badgeBody()).toMatchObject({ message: "ok", color: BADGE_COLOR.ok });
  });

  it("is down one second past the down threshold", async () => {
    await meta("last_tick_at", isoUtc(NOW - DOWN_AFTER_MS - 1000));
    expect(await badgeBody()).toMatchObject(failing(`no tick for ${DOWN_MINUTES}m`));
  });

  it("is failing when reminders failed or expired in the last 24 hours", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await seedReminder({ key: "f", status: "failed", updated_at: isoUtc(NOW - 60 * MIN) });
    await seedReminder({ key: "e", status: "expired", updated_at: isoUtc(NOW - 23 * 60 * MIN) });
    await seedReminder({ key: "old", status: "failed", updated_at: isoUtc(NOW - 25 * 60 * MIN) });
    expect(await badgeBody()).toMatchObject(failing("2 missed in 24h"));
  });

  it("reports down before failing", async () => {
    await tickedMinutesAgo(2 * DOWN_MINUTES);
    await seedReminder({ key: "f", status: "failed" });
    expect(await badgeBody()).toMatchObject(failing(`no tick for ${2 * DOWN_MINUTES}m`));
  });

  it("is failing when the last dispatch to the sender failed", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401: Bad credentials");
    expect(await badgeBody()).toMatchObject(failing("dispatch failing"));
  });

  it("reports down before dispatch failing", async () => {
    await tickedMinutesAgo(2 * DOWN_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401");
    expect(await badgeBody()).toMatchObject(failing(`no tick for ${2 * DOWN_MINUTES}m`));
  });

  it("reports dispatch failing before missed reminders", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_wake_error", "workflow dispatch HTTP 401");
    await seedReminder({ key: "e", status: "expired" });
    expect(await badgeBody()).toMatchObject(failing("dispatch failing"));
  });

  it("is degraded when the last fetch recorded an error", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500");
    expect(await badgeBody()).toMatchObject({ message: "degraded: clist", color: BADGE_COLOR.degraded });
  });

  it("reports failing before degraded", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500");
    await seedReminder({ key: "e", status: "expired" });
    expect(await badgeBody()).toMatchObject(failing("1 missed in 24h"));
  });

  it("exposes only the badge fields, never message text, errors or secrets", async () => {
    await tickedMinutesAgo(RECENT_MINUTES);
    await meta("last_fetch_error", "clist HTTP 500 boom detail");
    await meta("last_wake_error", "dispatch wake detail");
    await seedReminder({ key: "k", text: "TOP SECRET MESSAGE BODY", href: "https://private.test/contest", status: "failed", last_error: "send detail" });
    const raw = await (await badge()).text();
    expect(Object.keys(JSON.parse(raw)).sort()).toEqual(["color", "label", "labelColor", "message", "schemaVersion"]);
    for (const leaked of ["TOP SECRET", "private.test", "boom detail", "wake detail", "send detail", env.TICK_TOKEN, env.SENDER_TOKEN, env.ADMIN_TOKEN, env.GH_TOKEN, env.CLIST_API_KEY]) {
      expect(raw).not.toContain(leaked);
    }
  });

  it("rejects non-GET methods", async () => {
    expect((await exports.default.fetch("https://w.test/badge", { method: "POST" })).status).toBe(405);
  });
});
