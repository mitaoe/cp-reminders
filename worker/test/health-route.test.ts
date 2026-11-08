import { env, exports } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isoUtc } from "../src/core/format.ts";
import { httpFailure } from "../src/errors.ts";
import { MIN, NOW, mockFetch, resetDb, seedReminder } from "./helpers.ts";

beforeEach(async () => {
  await resetDb();
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const health = () => exports.default.fetch("https://w.test/health");

describe("GET /health", () => {
  it("is public and reports nulls and zeroes on a fresh database", async () => {
    const res = await health();
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toMatch(/application\/json/);
    expect(await res.json()).toEqual({
      lastTickAt: null,
      lastFetchAt: null,
      lastFetchSource: null,
      lastFetchError: null,
      lastWakeError: null,
      queued: 0,
      claimed: 0,
      failed24h: 0,
      expired24h: 0,
    });
  });

  it("counts reminders by state and limits failed and expired to the last 24 hours", async () => {
    await seedReminder({ key: "q1" });
    await seedReminder({ key: "q2" });
    await seedReminder({ key: "c1", status: "claimed", attempts: 1, claim_id: "x", claimed_at: isoUtc(NOW) });
    await seedReminder({ key: "s1", status: "sent" });
    await seedReminder({ key: "f-new", status: "failed", updated_at: isoUtc(NOW - 23 * 60 * MIN) });
    await seedReminder({ key: "f-old", status: "failed", updated_at: isoUtc(NOW - 25 * 60 * MIN) });
    await seedReminder({ key: "e-new", status: "expired", updated_at: isoUtc(NOW - 60 * MIN) });
    await seedReminder({ key: "e-old", status: "expired", updated_at: isoUtc(NOW - 48 * 60 * MIN) });
    expect(await (await health()).json()).toMatchObject({ queued: 2, claimed: 1, failed24h: 1, expired24h: 1 });
  });

  it("reflects the last tick and fetch recorded by the scheduler", async () => {
    const mock = mockFetch({ clist: () => new Response("x", { status: 500 }) });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mock.fetcher(String(input), init));
    await exports.default.fetch("https://w.test/tick", { method: "POST", headers: { Authorization: `Bearer ${env.TICK_TOKEN}` } });
    expect(await (await health()).json()).toMatchObject({
      lastTickAt: isoUtc(NOW),
      lastFetchAt: null,
      lastFetchError: `clist: ${httpFailure(500)}; codeforces: ${httpFailure(503)}`,
    });
  });

  it("reports a failed dispatch to the sender", async () => {
    await seedReminder({ key: "q" });
    const mock = mockFetch({ github: () => new Response("Bad credentials", { status: 401 }) });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mock.fetcher(String(input), init));
    await exports.default.fetch("https://w.test/tick", { method: "POST", headers: { Authorization: `Bearer ${env.TICK_TOKEN}` } });
    const raw = await (await health()).text();
    expect(JSON.parse(raw)).toMatchObject({ lastWakeError: httpFailure(401) });
    expect(raw).not.toContain("Bad credentials");
  });

  it("exposes only the documented counters, never message text, links or secrets", async () => {
    await seedReminder({ key: "k", text: "TOP SECRET MESSAGE BODY", href: "https://private.test/contest", status: "failed", last_error: "boom detail" });
    const raw = await (await health()).text();
    expect(Object.keys(JSON.parse(raw)).sort()).toEqual(
      ["claimed", "expired24h", "failed24h", "lastFetchAt", "lastFetchError", "lastFetchSource", "lastTickAt", "lastWakeError", "queued"],
    );
    for (const leaked of ["TOP SECRET", "private.test", "boom detail", env.TICK_TOKEN, env.SENDER_TOKEN, env.ADMIN_TOKEN, env.GH_TOKEN, env.CLIST_API_KEY]) {
      expect(raw).not.toContain(leaked);
    }
  });

  it("rejects non-GET methods", async () => {
    expect((await exports.default.fetch("https://w.test/health", { method: "POST" })).status).toBe(405);
  });
});
