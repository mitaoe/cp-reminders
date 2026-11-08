import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { SNAPSHOT_TTL_MS } from "../src/contests.ts";
import { type Contest, PLATFORM, formatReminder, isoUtc, utcMs } from "../src/core/format.ts";
import { reminderKey, selectContests } from "../src/core/select.ts";
import type { Fetcher } from "../src/env.ts";
import { MISSED_WINDOW } from "../src/errors.ts";
import { readHealth } from "../src/health.ts";
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

const TICK_MS = 5 * MIN;

let consoleError: ReturnType<typeof vi.spyOn>;
const configuredExclude = rules.exclude;

beforeEach(async () => {
  await resetDb();
  consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  rules.exclude = configuredExclude;
  vi.restoreAllMocks();
});

const count = async (where = "1 = 1") =>
  (await env.DB.prepare(`SELECT COUNT(*) AS n FROM reminders WHERE ${where}`).first<{ n: number }>())!.n;

const targetInFixtures = fixtureContests.find((c) => c.href === TARGET.href)!;
const onlyContests = (...contests: typeof fixtureContests) => mockFetch({ clist: () => jsonResponse({ objects: contests }) });

async function tickEvery5Minutes(fromMs: number, untilMs: number, fetcher: Fetcher): Promise<void> {
  for (let t = fromMs; t <= untilMs; t += TICK_MS) await runTick(env, t, fetcher);
}

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
    expect(await count("status = 'queued'")).toBe(0);
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

  it("ignores a stored override that is not a valid tip, and logs it", async () => {
    await env.DB.prepare("INSERT INTO tip_overrides (href, tip, updated_at) VALUES (?1, 'join evil.com', ?2)")
      .bind(link, isoUtc(NOW))
      .run();
    const { fetcher } = mockFetch();
    await runTick(env, NOW, fetcher);
    expect((await getReminder(TARGET_KEY))?.text).toBe(formatReminder(TARGET, tipRules, baseOverrides));
    expect(consoleError).toHaveBeenCalledWith(`ignoring the stored tip for ${link}: not a valid tip`);
  });
});

describe("runTick: contests the formatter cannot render as given", () => {
  const unlinked = { ...targetInFixtures, href: `${targetInFixtures.href.replace(/\/$/, "")}/ranking/page` };
  const unlinkedKey = reminderKey(unlinked);

  it("records a due contest without a contest link as failed at once, without text", async () => {
    const { fetcher, calls } = onlyContests(unlinked);
    const result = await runTick(env, NOW, fetcher);
    expect(result).toMatchObject({ queued: 0, woke: false });
    expect(result.errors).toContain(`no contest link: ${unlinkedKey}`);
    expect(await getReminder(unlinkedKey)).toMatchObject({
      href: unlinked.href,
      start_utc: isoUtc(TARGET_START),
      deadline_utc: isoUtc(TARGET_DEADLINE),
      text: "",
      status: "failed",
      attempts: 0,
      claim_id: null,
      last_error: "no_contest_link",
      created_at: isoUtc(NOW),
      updated_at: isoUtc(NOW),
    });
    expect(calls.github).toHaveLength(0);
    expect(consoleError).toHaveBeenCalledWith(`no contest link: ${unlinkedKey}`);
  });

  it("records it once, however many ticks see it due", async () => {
    const { fetcher } = onlyContests(unlinked);
    await runTick(env, NOW, fetcher);
    const second = await runTick(env, NOW + MIN, fetcher);
    expect(second.errors).not.toContain(`no contest link: ${unlinkedKey}`);
    expect(await count()).toBe(1);
    expect(await getReminder(unlinkedKey)).toMatchObject({ status: "failed", updated_at: isoUtc(NOW) });
  });

  it("still queues the linked contests due in the same tick", async () => {
    const { fetcher } = onlyContests(targetInFixtures, unlinked);
    expect((await runTick(env, NOW, fetcher)).queued).toBe(1);
    expect(await count("status = 'queued'")).toBe(1);
    expect(await count("status = 'failed'")).toBe(1);
  });

  it("logs a contest name replaced by the platform's fallback name", async () => {
    const phone = { ...targetInFixtures, event: `${targetInFixtures.event} call 98765 43210` };
    const { fetcher } = onlyContests(phone);
    expect((await runTick(env, NOW, fetcher)).queued).toBe(1);
    expect(consoleError).toHaveBeenCalledWith(`contest name replaced by the fallback name: ${TARGET_KEY}`);
  });
});

describe("runTick: windows that lie entirely between two successful ticks", () => {
  const beforeWindow = NOW - 2 * SNAPSHOT_TTL_MS;
  const afterWindow = TARGET_DEADLINE + MIN;

  it("records a selected contest whose window lay in the gap as expired with missed_window", async () => {
    const { fetcher, calls } = onlyContests(targetInFixtures);
    await runTick(env, beforeWindow, fetcher);
    const result = await runTick(env, afterWindow, fetcher);

    expect(result).toMatchObject({ queued: 0, woke: false });
    expect(result.errors).toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await count()).toBe(1);
    expect(await getReminder(TARGET_KEY)).toMatchObject({
      start_utc: isoUtc(TARGET_START),
      deadline_utc: isoUtc(TARGET_DEADLINE),
      text: "",
      status: "expired",
      attempts: 0,
      last_error: MISSED_WINDOW,
      created_at: isoUtc(afterWindow),
      updated_at: isoUtc(afterWindow),
    });
    expect(calls.github).toHaveLength(0);
    expect(consoleError).toHaveBeenCalledWith(`missed reminder window: ${TARGET_KEY}`);
    expect((await readHealth(env.DB)).expired24h).toBe(1);
  });

  it("records it once", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    await runTick(env, beforeWindow, fetcher);
    await runTick(env, afterWindow, fetcher);
    const third = await runTick(env, afterWindow + MIN, fetcher);
    expect(third.errors).not.toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await count()).toBe(1);
  });

  it("records a contest that has already started when ticks resume", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    await runTick(env, beforeWindow, fetcher);
    await runTick(env, TARGET_START + 3 * 60 * MIN, fetcher);
    expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "expired", last_error: MISSED_WINDOW });
  });

  it("leaves a contest that has a row untouched", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    await runTick(env, beforeWindow, fetcher);
    await seedReminder({ key: TARGET_KEY, status: "sent", attempts: 1, text: "original", updated_at: isoUtc(NOW) });
    const result = await runTick(env, afterWindow, fetcher);
    expect(result.errors).not.toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "sent", text: "original", last_error: null });
  });

  it("queues rather than records a contest whose window is open", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    await runTick(env, beforeWindow, fetcher);
    expect((await runTick(env, NOW, fetcher)).queued).toBe(1);
    expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "queued", last_error: null });
  });

  it("records nothing on the first tick ever", async () => {
    const { fetcher } = onlyContests(targetInFixtures);
    const first = await runTick(env, afterWindow, fetcher);
    const second = await runTick(env, afterWindow + TICK_MS, fetcher);
    expect([...first.errors, ...second.errors]).not.toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await count()).toBe(0);
  });

  it("records nothing when a tick ran while the window was open", async () => {
    let listed = fixtureContests.filter((c) => c.href !== TARGET.href);
    const { fetcher } = mockFetch({ clist: () => jsonResponse({ objects: listed }) });
    await runTick(env, NOW + TICK_MS, fetcher);
    listed = [targetInFixtures];
    const result = await runTick(env, NOW + TICK_MS + SNAPSHOT_TTL_MS, fetcher);
    expect(result.errors).not.toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await getReminder(TARGET_KEY)).toBeNull();
  });

  it("records nothing for a past contest that a rules change newly selects", async () => {
    rules.exclude = { ...configuredExclude, [PLATFORM[TARGET.resource_id]!]: ["."] };
    const { fetcher } = onlyContests(targetInFixtures);
    await tickEvery5Minutes(beforeWindow, TARGET_DEADLINE, fetcher);
    rules.exclude = configuredExclude;
    const result = await runTick(env, TARGET_DEADLINE + TICK_MS, fetcher);
    expect(result.errors).not.toContain(`missed reminder window: ${TARGET_KEY}`);
    expect(await count()).toBe(0);
  });

  it("records nothing for a Div. 2 twin listed after the Div. 1 reminder was queued", async () => {
    const div1 = fixtureContests.find((c) => c.resource_id === 1 && /\(Div\. 1\)/.test(c.event))!;
    const div2 = fixtureContests.find((c) => c.start === div1.start && /\(Div\. 2\)/.test(c.event))!;
    const merged: Contest = selectContests([div1, div2], rules)[0]!;
    const opensMs = utcMs(div1.start) - rules.leadMinutes * MIN;
    let listed = [div1];
    const { fetcher } = mockFetch({ clist: () => jsonResponse({ objects: listed }) });

    await tickEvery5Minutes(opensMs, opensMs + SNAPSHOT_TTL_MS - TICK_MS, fetcher);
    expect(await getReminder(reminderKey(div1))).not.toBeNull();
    listed = [div1, div2];
    const result = await runTick(env, opensMs + SNAPSHOT_TTL_MS, fetcher);

    expect(reminderKey(merged)).not.toBe(reminderKey(div1));
    expect(result.errors).not.toContain(`missed reminder window: ${reminderKey(merged)}`);
    expect(await getReminder(reminderKey(merged))).toBeNull();
    expect(await count()).toBe(1);
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
