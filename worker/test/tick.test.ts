import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { SNAPSHOT_TTL_MS } from "../src/contests.ts";
import { formatReminder, isoUtc } from "../src/core/format.ts";
import { reminderKey } from "../src/core/select.ts";
import { getMeta } from "../src/meta.ts";
import { runTick } from "../src/tick.ts";
import { WAKE_DEBOUNCE_MS } from "../src/wake.ts";
import {
  MIN,
  NOW,
  TARGET,
  TARGET_DEADLINE,
  TARGET_KEY,
  TARGET_START,
  fixtureContests,
  getReminder,
  jsonResponse,
  mockFetch,
  resetDb,
  seedReminder,
} from "./helpers.ts";

beforeEach(async () => {
  await resetDb();
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

const count = async (where = "1 = 1") =>
  (await env.DB.prepare(`SELECT COUNT(*) AS n FROM reminders WHERE ${where}`).first<{ n: number }>())!.n;

const targetInFixtures = fixtureContests.find((c) => c.href === TARGET.href)!;
const onlyContests = (...contests: typeof fixtureContests) => mockFetch({ clist: () => jsonResponse({ objects: contests }) });

describe("runTick: queueing", () => {
  it("queues a due reminder with its rendered text and deadline", async () => {
    const { fetcher } = mockFetch();
    const result = await runTick(env, NOW, fetcher);

    expect(result).toMatchObject({ fetched: true, queued: 1 });
    const row = await getReminder(TARGET_KEY);
    expect(row).toMatchObject({
      resource_id: TARGET.resource_id,
      href: TARGET.href,
      start_utc: isoUtc(TARGET_START),
      deadline_utc: isoUtc(TARGET_DEADLINE),
      status: "queued",
      attempts: 0,
      created_at: isoUtc(NOW),
    });
    expect(row?.text).toBe(formatReminder(TARGET, tipRules, baseOverrides));
  });

  it("queues nothing outside the send window", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    expect((await runTick(env, NOW - MIN, fetcher)).queued).toBe(0);
    expect((await runTick(env, TARGET_DEADLINE + 1000, fetcher)).queued).toBe(0);
    expect(await count()).toBe(0);
  });

  it("is idempotent across ticks", async () => {
    const { fetcher } = mockFetch();
    const first = await runTick(env, NOW, fetcher);
    const second = await runTick(env, NOW + MIN, fetcher);
    expect([first.queued, second.queued]).toEqual([1, 0]);
    expect(await count()).toBe(1);
  });

  it("is idempotent under two overlapping ticks", async () => {
    const { fetcher } = mockFetch();
    const [a, b] = await Promise.all([runTick(env, NOW, fetcher), runTick(env, NOW, fetcher)]);
    expect(a.queued + b.queued).toBe(1);
    expect(await count()).toBe(1);
  });

  it("does not requeue a reminder that was already sent", async () => {
    await seedReminder({ key: TARGET_KEY, status: "sent", attempts: 1, text: "original" });
    const { fetcher } = mockFetch();
    expect((await runTick(env, NOW, fetcher)).queued).toBe(0);
    expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "sent", text: "original" });
  });

  it("sends no second reminder for a contest rescheduled after its reminder", async () => {
    const movedStart = TARGET_START + 15 * MIN;
    const rescheduled = { ...targetInFixtures, start: isoUtc(movedStart).slice(0, 19) };
    await seedReminder({ key: TARGET_KEY, status: "sent", attempts: 1, text: "original" });
    const { fetcher } = onlyContests(rescheduled);
    expect((await runTick(env, movedStart - rules.leadMinutes * MIN, fetcher)).queued).toBe(0);
    expect(await count()).toBe(1);
    expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "sent", text: "original" });
  });

  it("reminds a contest rescheduled before its reminder at the new time", async () => {
    const movedStart = TARGET_START + 60 * MIN;
    const rescheduled = { ...targetInFixtures, start: isoUtc(movedStart).slice(0, 19) };
    const { fetcher } = onlyContests(rescheduled);
    expect((await runTick(env, NOW, fetcher)).queued).toBe(0);
    expect((await runTick(env, movedStart - rules.leadMinutes * MIN, fetcher)).queued).toBe(1);
    expect(await getReminder(reminderKey(rescheduled))).toMatchObject({ start_utc: isoUtc(movedStart), status: "queued" });
  });

  it("keeps queueing from the previous snapshot when every source fails", async () => {
    const { fetcher: ok } = mockFetch();
    await runTick(env, NOW - 2 * SNAPSHOT_TTL_MS, ok);
    const { fetcher } = mockFetch({ clist: () => new Response("x", { status: 500 }) });
    const result = await runTick(env, NOW, fetcher);
    expect(result).toMatchObject({ fetched: false, queued: 1 });
    expect(result.errors.join(" ")).toMatch(/clist HTTP 500/);
  });
});

describe("runTick: tips", () => {
  const link = TARGET.href;

  it("merges D1 tip overrides into the rendered text", async () => {
    await env.DB.prepare("INSERT INTO tip_overrides (href, tip, updated_at) VALUES (?1, 'bring snacks', ?2)")
      .bind(link, isoUtc(NOW))
      .run();
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect((await getReminder(TARGET_KEY))?.text).toMatch(/\n\n📌 Tip: bring snacks$/);
  });

  it("treats an empty override as suppressing the tip", async () => {
    await env.DB.prepare("INSERT INTO tip_overrides (href, tip, updated_at) VALUES (?1, '', ?2)").bind(link, isoUtc(NOW)).run();
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect((await getReminder(TARGET_KEY))?.text).not.toContain("Tip");
  });

  it("ignores a stored override that is not a valid tip", async () => {
    await env.DB.prepare("INSERT INTO tip_overrides (href, tip, updated_at) VALUES (?1, 'join evil.com', ?2)")
      .bind(link, isoUtc(NOW))
      .run();
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect((await getReminder(TARGET_KEY))?.text).toBe(formatReminder(TARGET, tipRules, baseOverrides));
  });
});

describe("runTick: housekeeping and wake", () => {
  it("expires reminders whose deadline passed", async () => {
    await seedReminder({ key: "late", deadline_utc: isoUtc(NOW - MIN), start_utc: isoUtc(NOW + 9 * MIN) });
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect((await getReminder("late"))?.status).toBe("expired");
  });

  it("records last_tick_at", async () => {
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect(await getMeta(env.DB, "last_tick_at")).toBe(isoUtc(NOW));
  });

  it("wakes the workflow once for a new reminder and debounces the next tick", async () => {
    const { fetcher, calls } = mockFetch();
    const first = await runTick(env, NOW, fetcher);
    const second = await runTick(env, NOW + WAKE_DEBOUNCE_MS - 1000, fetcher);
    expect([first.woke, second.woke]).toEqual([true, false]);
    expect(calls.github).toHaveLength(1);
  });

  it("does not wake when nothing is pending", async () => {
    const { fetcher, calls } = mockFetch();
    const result = await runTick(env, NOW - MIN, fetcher);
    expect(result.woke).toBe(false);
    expect(calls.github).toHaveLength(0);
  });

  it("keeps the reminder queued and retries the wake when dispatch fails", async () => {
    const bad = mockFetch({ github: () => new Response("down", { status: 502 }) });
    const first = await runTick(env, NOW, bad.fetcher);
    expect(first).toMatchObject({ queued: 1, woke: false });
    expect(first.errors.join(" ")).toMatch(/dispatch HTTP 502/);
    expect(await getMeta(env.DB, "last_wake_at")).toBeNull();

    const good = mockFetch();
    const second = await runTick(env, NOW + MIN, good.fetcher);
    expect(second).toMatchObject({ queued: 0, woke: true });
    expect(good.calls.github).toHaveLength(1);
  });
});
