import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SNAPSHOT_LOOKBACK_MS, SNAPSHOT_TTL_MS, ensureSnapshot } from "../src/contests.ts";
import { isoUtc } from "../src/core/format.ts";
import { FAILURE, httpFailure } from "../src/errors.ts";
import { getMeta } from "../src/meta.ts";
import { MIN, NOW, fixtureContests, jsonResponse, mockFetch, resetDb } from "./helpers.ts";

let consoleError: ReturnType<typeof vi.spyOn>;

beforeEach(async () => {
  await resetDb();
  consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

const snapshotRow = () =>
  env.DB.prepare("SELECT source, fetched_at, payload FROM contest_snapshot WHERE id = 1").first<{
    source: string;
    fetched_at: string;
    payload: string;
  }>();

const cfResponse = (startSeconds: number) =>
  jsonResponse({
    status: "OK",
    result: [
      { id: 9001, name: "Codeforces Round 9001 (Div. 2)", phase: "BEFORE", durationSeconds: 7200, startTimeSeconds: startSeconds },
      { id: 9000, name: "Codeforces Round 9000 (Div. 2)", phase: "FINISHED", durationSeconds: 7200, startTimeSeconds: 1 },
    ],
  });

describe("ensureSnapshot: clist", () => {
  it("fetches clist when there is no snapshot and stores it", async () => {
    const { fetcher, calls } = mockFetch();
    const result = await ensureSnapshot(env, NOW, fetcher);

    expect(result.fetched).toBe(true);
    expect(result.contests).toHaveLength(fixtureContests.length);
    expect(calls.clist).toHaveLength(1);
    expect(calls.clist[0]!.url).toBe(
      `https://clist.by/api/v4/contest/?resource_id__in=1,2,93,102&start__gt=${isoUtc(NOW - SNAPSHOT_LOOKBACK_MS).slice(0, 19)}&order_by=start&limit=100`,
    );
    expect(calls.clist[0]!.init?.headers).toEqual({ Authorization: "ApiKey user:key" });

    const row = await snapshotRow();
    expect(row?.source).toBe("clist");
    expect(row?.fetched_at).toBe(isoUtc(NOW));
    expect(Object.keys(JSON.parse(row!.payload)[0]).sort()).toEqual(["duration", "event", "href", "resource_id", "start"]);
    expect(await getMeta(env.DB, "last_fetch_at")).toBe(isoUtc(NOW));
    expect(await getMeta(env.DB, "last_fetch_source")).toBe("clist");
    expect(await getMeta(env.DB, "last_fetch_error")).toBeNull();
  });

  it("does not refetch while the snapshot is fresh but does once it is stale", async () => {
    const { fetcher, calls } = mockFetch();
    await ensureSnapshot(env, NOW, fetcher);
    const cached = await ensureSnapshot(env, NOW + SNAPSHOT_TTL_MS - MIN, fetcher);
    expect(cached.fetched).toBe(false);
    expect(cached.contests).toHaveLength(fixtureContests.length);
    expect(calls.clist).toHaveLength(1);
    await ensureSnapshot(env, NOW + SNAPSHOT_TTL_MS, fetcher);
    expect(calls.clist).toHaveLength(2);
  });

  it("filters contests with invalid or missing timestamps from clist", async () => {
    const invalidContests = [
      { resource_id: 1, event: "Codeforces Round 9999 (Div. 2)", start: "TBD", duration: 7200, href: "https://codeforces.com/contests/9999" },
      { resource_id: 2, event: "Starters 999", start: "invalid-date", duration: 7200, href: "https://www.codechef.com/START999" },
      { resource_id: 102, event: "Weekly Contest 999", start: "", duration: 5400, href: "https://leetcode.com/contest/weekly-contest-999" },
    ];
    const { fetcher } = mockFetch({
      clist: () => jsonResponse({ objects: [...fixtureContests, ...invalidContests] }),
    });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(result.fetched).toBe(true);
    expect(result.contests).toHaveLength(fixtureContests.length);
    expect(result.contests.some((c) => c.start === "TBD")).toBe(false);
  });

  it("filters null and missing start fields in objects array without throwing", async () => {
    const corruptedObjects = [
      null,
      undefined,
      { resource_id: 1, event: "Missing start", duration: 7200, href: "https://codeforces.com/missing" },
    ];
    const { fetcher } = mockFetch({
      clist: () => jsonResponse({ objects: [...fixtureContests, ...corruptedObjects] }),
    });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(result.fetched).toBe(true);
    expect(result.contests).toHaveLength(fixtureContests.length);
  });

  it("falls back to Codeforces when clist returns only contests with invalid timestamps", async () => {
    const start = Math.floor((NOW + 60 * MIN) / 1000);
    const { fetcher, calls } = mockFetch({
      clist: () => jsonResponse({ objects: [{ resource_id: 1, event: "Round TBD", start: "TBD", duration: 7200, href: "https://codeforces.com/contests/1" }] }),
      codeforces: () => cfResponse(start),
    });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(calls.codeforces).toHaveLength(1);
    expect(result.contests.map((c) => c.href)).toEqual(["https://codeforces.com/contests/9001"]);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${FAILURE.empty}`);
  });
});

describe("ensureSnapshot: Codeforces fallback", () => {
  it("falls back when clist returns a non-2xx status", async () => {
    const { fetcher: first } = mockFetch();
    await ensureSnapshot(env, NOW - 2 * SNAPSHOT_TTL_MS, first);

    const start = Math.floor((NOW + 60 * MIN) / 1000);
    const { fetcher, calls } = mockFetch({ clist: () => new Response("boom", { status: 500 }), codeforces: () => cfResponse(start) });
    const result = await ensureSnapshot(env, NOW, fetcher);

    expect(calls.codeforces).toHaveLength(1);
    expect(calls.codeforces[0]!.url).toBe("https://codeforces.com/api/contest.list?gym=false");
    expect(result.fetched).toBe(true);
    const cf = result.contests.filter((c) => c.resource_id === 1);
    expect(cf).toEqual([
      {
        resource_id: 1,
        event: "Codeforces Round 9001 (Div. 2)",
        start: new Date(start * 1000).toISOString().slice(0, 19),
        duration: 7200,
        href: "https://codeforces.com/contests/9001",
      },
    ]);
    expect(result.contests.some((c) => c.resource_id === 2)).toBe(true);
    expect((await snapshotRow())?.source).toBe("codeforces");
    expect(await getMeta(env.DB, "last_fetch_source")).toBe("codeforces");
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${httpFailure(500)}`);
    expect(result.error).toContain("clist HTTP 500");
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("clist HTTP 500"));
  });

  it("keeps contests that started within the lookback, from Codeforces and from the previous snapshot", async () => {
    const recentStart = NOW - SNAPSHOT_LOOKBACK_MS + 60 * MIN;
    const oldStart = NOW - SNAPSHOT_LOOKBACK_MS - 60 * MIN;
    const recentStarters = { ...fixtureContests.find((c) => c.resource_id === 2)!, start: isoUtc(recentStart).slice(0, 19), href: "https://www.codechef.com/RECENT1" };
    const oldStarters = { ...recentStarters, start: isoUtc(oldStart).slice(0, 19), href: "https://www.codechef.com/OLD1" };
    const { fetcher: first } = mockFetch({ clist: () => jsonResponse({ objects: [...fixtureContests, recentStarters, oldStarters] }) });
    await ensureSnapshot(env, NOW - 2 * SNAPSHOT_TTL_MS, first);

    const { fetcher } = mockFetch({
      clist: () => new Response("x", { status: 500 }),
      codeforces: () =>
        jsonResponse({
          status: "OK",
          result: [
            { id: 9003, name: "Codeforces Round 9003 (Div. 2)", phase: "FINISHED", durationSeconds: 7200, startTimeSeconds: Math.floor(recentStart / 1000) },
            { id: 9002, name: "Codeforces Round 9002 (Div. 2)", phase: "FINISHED", durationSeconds: 7200, startTimeSeconds: Math.floor(oldStart / 1000) },
          ],
        }),
    });
    const result = await ensureSnapshot(env, NOW, fetcher);
    const hrefs = result.contests.map((c) => c.href);
    expect(hrefs).toContain("https://codeforces.com/contests/9003");
    expect(hrefs).not.toContain("https://codeforces.com/contests/9002");
    expect(hrefs).toContain("https://www.codechef.com/RECENT1");
    expect(hrefs).not.toContain("https://www.codechef.com/OLD1");
  });

  it("safely ignores previous snapshot contests with invalid start timestamps during Codeforces fallback", async () => {
    const recentStart = NOW - SNAPSHOT_LOOKBACK_MS + 60 * MIN;
    const validOther = { ...fixtureContests.find((c) => c.resource_id === 2)!, start: isoUtc(recentStart).slice(0, 19), href: "https://www.codechef.com/RECENT1" };
    const invalidOther = { ...validOther, start: "TBD", href: "https://www.codechef.com/TBD" };
    await env.DB.prepare(
      "INSERT INTO contest_snapshot (id, source, fetched_at, payload) VALUES (1, 'clist', ?1, ?2)",
    ).bind(isoUtc(NOW - 2 * SNAPSHOT_TTL_MS), JSON.stringify([validOther, invalidOther, null])).run();

    const start = Math.floor((NOW + 60 * MIN) / 1000);
    const { fetcher } = mockFetch({
      clist: () => new Response("x", { status: 500 }),
      codeforces: () => cfResponse(start),
    });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(result.fetched).toBe(true);
    expect(result.contests.some((c) => c.href === "https://www.codechef.com/RECENT1")).toBe(true);
    expect(result.contests.some((c) => c.href === "https://www.codechef.com/TBD")).toBe(false);
  });

  it("falls back when clist returns an empty list", async () => {
    const start = Math.floor((NOW + 60 * MIN) / 1000);
    const { fetcher, calls } = mockFetch({ clist: () => jsonResponse({ objects: [] }), codeforces: () => cfResponse(start) });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(calls.codeforces).toHaveLength(1);
    expect(result.contests.map((c) => c.href)).toEqual(["https://codeforces.com/contests/9001"]);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${FAILURE.empty}`);
  });

  it.each([
    ["an unparsable body", () => new Response("<html>", { status: 200 }), FAILURE.invalid],
    [
      "a timeout",
      () => {
        throw new DOMException("The operation timed out.", "TimeoutError");
      },
      FAILURE.timeout,
    ],
    [
      "a failed connection",
      () => {
        throw new TypeError("fetch failed");
      },
      FAILURE.network,
    ],
    ["a null body", () => jsonResponse(null), FAILURE.invalid],
    ["objects that are not a list", () => jsonResponse({ objects: "none" }), FAILURE.invalid],
  ])("records only a code when clist fails with %s", async (_name, clist, code) => {
    const start = Math.floor((NOW + 60 * MIN) / 1000);
    const { fetcher } = mockFetch({ clist, codeforces: () => cfResponse(start) });
    await ensureSnapshot(env, NOW, fetcher);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${code}`);
  });
});

describe("ensureSnapshot: total failure", () => {
  it("keeps the previous snapshot and records last_fetch_error", async () => {
    const { fetcher: ok } = mockFetch();
    await ensureSnapshot(env, NOW - 2 * SNAPSHOT_TTL_MS, ok);
    const before = await snapshotRow();

    const { fetcher } = mockFetch({ clist: () => new Response("x", { status: 502 }) });
    const result = await ensureSnapshot(env, NOW, fetcher);

    expect(result.fetched).toBe(false);
    expect(result.contests).toHaveLength(fixtureContests.length);
    expect(await snapshotRow()).toEqual(before);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${httpFailure(502)}; codeforces: ${httpFailure(503)}`);
    expect(result.error).toMatch(/clist HTTP 502.*codeforces HTTP 503/);
  });

  it.each([
    ["a result that is not a list", () => jsonResponse({ status: "OK", result: "none" })],
    ["a null body", () => jsonResponse(null)],
  ])("records invalid response when Codeforces answers with %s", async (_name, codeforces) => {
    const { fetcher } = mockFetch({ clist: () => new Response("x", { status: 500 }), codeforces });
    await ensureSnapshot(env, NOW, fetcher);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${httpFailure(500)}; codeforces: ${FAILURE.invalid}`);
  });

  it("lets a database failure propagate without falling back to Codeforces", async () => {
    vi.spyOn(env.DB, "batch").mockRejectedValueOnce(new Error("D1 unavailable"));
    const { fetcher, calls } = mockFetch();
    await expect(ensureSnapshot(env, NOW, fetcher)).rejects.toThrow("D1 unavailable");
    expect(calls.codeforces).toHaveLength(0);
    expect(await getMeta(env.DB, "last_fetch_error")).toBeNull();
  });

  it("records invalid response when Codeforces reports a failure", async () => {
    const { fetcher } = mockFetch({
      clist: () => new Response("x", { status: 500 }),
      codeforces: () => jsonResponse({ status: "FAILED", comment: "internal details" }),
    });
    await ensureSnapshot(env, NOW, fetcher);
    expect(await getMeta(env.DB, "last_fetch_error")).toBe(`clist: ${httpFailure(500)}; codeforces: ${FAILURE.invalid}`);
  });

  it("returns no contests when nothing was ever fetched", async () => {
    const { fetcher } = mockFetch({ clist: () => new Response("x", { status: 500 }) });
    const result = await ensureSnapshot(env, NOW, fetcher);
    expect(result).toMatchObject({ contests: [], fetched: false });
    expect(await snapshotRow()).toBeNull();
  });

  it("clears the error after a later successful fetch", async () => {
    const { fetcher: bad } = mockFetch({ clist: () => new Response("x", { status: 500 }) });
    await ensureSnapshot(env, NOW, bad);
    expect(await getMeta(env.DB, "last_fetch_error")).not.toBeNull();
    const { fetcher: good } = mockFetch();
    await ensureSnapshot(env, NOW + 5 * MIN, good);
    expect(await getMeta(env.DB, "last_fetch_error")).toBeNull();
  });
});

describe("ensureSnapshot: contests without a contest link", () => {
  const unlinked = (n: number, start: string) => ({
    resource_id: 1,
    event: `Codeforces Round ${n} (Div. 3)`,
    start,
    duration: 7200,
    href: `https://codeforces.com/blog/entry/${n}`,
  });
  const upcomingStart = isoUtc(NOW + 3 * 60 * MIN).slice(0, 19);

  it("counts the upcoming selected contests that have no contest link at each refresh", async () => {
    const withUnlinked = mockFetch({ clist: () => jsonResponse({ objects: [...fixtureContests, unlinked(1, upcomingStart), unlinked(2, upcomingStart)] }) });
    await ensureSnapshot(env, NOW, withUnlinked.fetcher);
    expect(await getMeta(env.DB, "unlinked_contests")).toBe("2");
    expect(consoleError).toHaveBeenCalledWith("2 upcoming contest(s) without a contest link");

    const { fetcher } = mockFetch();
    await ensureSnapshot(env, NOW + SNAPSHOT_TTL_MS, fetcher);
    expect(await getMeta(env.DB, "unlinked_contests")).toBeNull();
  });

  it("ignores contests that are not selected or have already started", async () => {
    const skipped = { ...unlinked(3, upcomingStart), event: "Kotlin Heroes: Episode 3" };
    const started = unlinked(4, isoUtc(NOW - MIN).slice(0, 19));
    const { fetcher } = mockFetch({ clist: () => jsonResponse({ objects: [...fixtureContests, skipped, started] }) });
    await ensureSnapshot(env, NOW, fetcher);
    expect(await getMeta(env.DB, "unlinked_contests")).toBeNull();
  });

  it("counts them in a snapshot completed by the Codeforces fallback", async () => {
    const { fetcher: first } = mockFetch({ clist: () => jsonResponse({ objects: [...fixtureContests, unlinked(5, upcomingStart)] }) });
    await ensureSnapshot(env, NOW, first);
    const { fetcher } = mockFetch({
      clist: () => new Response("x", { status: 500 }),
      codeforces: () => cfResponse(Math.floor((NOW + 60 * MIN) / 1000)),
    });
    await ensureSnapshot(env, NOW + SNAPSHOT_TTL_MS, fetcher);
    expect(await getMeta(env.DB, "unlinked_contests")).toBeNull();
  });
});
