import { env } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isoUtc } from "../src/core/format.ts";
import { FAILURE, httpFailure } from "../src/errors.ts";
import { getMeta } from "../src/meta.ts";
import { LEASE_MS, MAX_ATTEMPTS } from "../src/reminders.ts";
import { WAKE_DEBOUNCE_MS, maybeWake } from "../src/wake.ts";
import { MIN, NOW, jsonResponse, mockFetch, resetDb, seedReminder } from "./helpers.ts";

let consoleError: ReturnType<typeof vi.spyOn>;

beforeEach(async () => {
  await resetDb();
  consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

const setLastWake = (ms: number) =>
  env.DB.prepare("INSERT INTO meta (k, v) VALUES ('last_wake_at', ?1)").bind(isoUtc(ms)).run();

describe("maybeWake", () => {
  it("does not wake when the outbox is empty", async () => {
    const { fetcher, calls } = mockFetch();
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: false });
    expect(calls.github).toHaveLength(0);
    expect(await getMeta(env.DB, "last_wake_at")).toBeNull();
  });

  it("dispatches send.yml with only a ref and records last_wake_at after a 204", async () => {
    await seedReminder({ key: "a" });
    const { fetcher, calls } = mockFetch();
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: true });

    expect(calls.github).toHaveLength(1);
    const call = calls.github[0]!;
    expect(call.url).toBe(`https://api.github.com/repos/${env.GH_REPO}/actions/workflows/send.yml/dispatches`);
    expect(call.init?.method).toBe("POST");
    expect(JSON.parse(String(call.init?.body))).toEqual({ ref: "main" });
    const headers = call.init?.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer ghp_test");
    expect(headers.Accept).toBe("application/vnd.github+json");
    expect(headers["X-GitHub-Api-Version"]).toBe("2022-11-28");
    expect(headers["User-Agent"]).toBeTruthy();
    expect(await getMeta(env.DB, "last_wake_at")).toBe(isoUtc(NOW));
  });

  it("treats a 200 response with a run id as success", async () => {
    await seedReminder({ key: "a" });
    const { fetcher } = mockFetch({ github: () => jsonResponse({ workflow_run_id: 1, run_url: "u", html_url: "h" }) });
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: true });
    expect(await getMeta(env.DB, "last_wake_at")).toBe(isoUtc(NOW));
  });

  it.each([401, 403, 404, 422, 500, 503])("does not record last_wake_at after HTTP %i", async (status) => {
    await seedReminder({ key: "a" });
    const { fetcher } = mockFetch({ github: () => new Response("nope", { status }) });
    const result = await maybeWake(env, NOW, fetcher);
    expect(result.woke).toBe(false);
    expect(result.error).toContain(`HTTP ${status}: nope`);
    expect(await getMeta(env.DB, "last_wake_at")).toBeNull();
    expect(await getMeta(env.DB, "last_wake_error")).toBe(httpFailure(status));
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining(`HTTP ${status}: nope`));
  });

  it("keeps the HTTP status when the error body cannot be read", async () => {
    await seedReminder({ key: "a" });
    const unreadable = new ReadableStream({
      start(controller) {
        controller.error(new Error("body lost"));
      },
    });
    const { fetcher } = mockFetch({ github: () => new Response(unreadable, { status: 502 }) });
    await maybeWake(env, NOW, fetcher);
    expect(await getMeta(env.DB, "last_wake_error")).toBe(httpFailure(502));
  });

  it("does not record last_wake_at when the request itself throws", async () => {
    await seedReminder({ key: "a" });
    const result = await maybeWake(env, NOW, async () => {
      throw new TypeError("fetch failed");
    });
    expect(result).toEqual({ woke: false, error: "fetch failed" });
    expect(await getMeta(env.DB, "last_wake_at")).toBeNull();
    expect(await getMeta(env.DB, "last_wake_error")).toBe(FAILURE.network);
  });

  it("records timeout when the dispatch times out", async () => {
    await seedReminder({ key: "a" });
    await maybeWake(env, NOW, async () => {
      throw new DOMException("The operation timed out.", "TimeoutError");
    });
    expect(await getMeta(env.DB, "last_wake_error")).toBe(FAILURE.timeout);
  });

  it("keeps the dispatch error until a dispatch succeeds", async () => {
    await seedReminder({ key: "a" });
    const failing = mockFetch({ github: () => new Response("Bad credentials", { status: 401 }) });
    await maybeWake(env, NOW, failing.fetcher);

    await env.DB.prepare("DELETE FROM reminders").run();
    expect(await maybeWake(env, NOW + MIN, mockFetch().fetcher)).toEqual({ woke: false });
    expect(await getMeta(env.DB, "last_wake_error")).toBe(httpFailure(401));

    await seedReminder({ key: "b" });
    expect(await maybeWake(env, NOW + 2 * MIN, mockFetch().fetcher)).toEqual({ woke: true });
    expect(await getMeta(env.DB, "last_wake_error")).toBeNull();
  });

  it("debounces wakes within the debounce interval", async () => {
    await seedReminder({ key: "a" });
    await setLastWake(NOW - WAKE_DEBOUNCE_MS + MIN);
    const { fetcher, calls } = mockFetch();
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: false });
    expect(calls.github).toHaveLength(0);
    expect(await maybeWake(env, NOW + MIN, fetcher)).toEqual({ woke: true });
    expect(calls.github).toHaveLength(1);
  });

  it("wakes for a claimed reminder whose lease expired but not for one in flight", async () => {
    await seedReminder({ key: "inflight", status: "claimed", attempts: 1, claim_id: "c", claimed_at: isoUtc(NOW - LEASE_MS / 2) });
    const { fetcher, calls } = mockFetch();
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: false });
    await seedReminder({ key: "stale", status: "claimed", attempts: 1, claim_id: "c", claimed_at: isoUtc(NOW - LEASE_MS - MIN) });
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: true });
    expect(calls.github).toHaveLength(1);
  });

  it("does not wake for reminders that are terminal, exhausted or past their deadline", async () => {
    await seedReminder({ key: "s", status: "sent" });
    await seedReminder({ key: "f", status: "failed" });
    await seedReminder({ key: "e", status: "expired" });
    await seedReminder({ key: "late", deadline_utc: isoUtc(NOW - MIN) });
    await seedReminder({ key: "worn", status: "claimed", attempts: MAX_ATTEMPTS, claim_id: "c", claimed_at: isoUtc(NOW - LEASE_MS - MIN) });
    const { fetcher, calls } = mockFetch();
    expect(await maybeWake(env, NOW, fetcher)).toEqual({ woke: false });
    expect(calls.github).toHaveLength(0);
  });
});
