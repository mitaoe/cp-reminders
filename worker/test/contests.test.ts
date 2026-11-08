import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SNAPSHOT_TTL_MS, ensureSnapshot } from "../src/contests.ts";
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
      `https://clist.by/api/v4/contest/?resource_id__in=1,2,93,102&start__gt=${isoUtc(NOW).slice(0, 19)}&order_by=start&limit=100`,
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
