import { env, exports } from "cloudflare:workers";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { isoUtc } from "../src/core/format.ts";
import { storeCountsStatement } from "../src/health.ts";
import { LEASE_MS, MAX_ATTEMPTS } from "../src/reminders.ts";
import { NOW, TARGET_KEY, getReminder, mockFetch, resetDb, seedReminder } from "./helpers.ts";

beforeEach(async () => {
  await resetDb();
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
  const mock = mockFetch();
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mock.fetcher(String(input), init));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const api = (method: string, path: string, token: string, body?: unknown) =>
  exports.default.fetch(`https://w.test${path}`, {
    method,
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

it("tick -> claim -> failed ack -> reclaim -> sent ack, with a lost sender in between", async () => {
  expect((await (await api("POST", "/tick", env.TICK_TOKEN)).json())).toMatchObject({ queued: 1, woke: true });

  const first = (await (await api("POST", "/outbox/claim", env.SENDER_TOKEN, { claimId: "run-1" })).json()) as {
    items: { key: string }[];
  };
  expect(first.items.map((i) => i.key)).toEqual([TARGET_KEY]);

  vi.setSystemTime(NOW + LEASE_MS);
  const second = (await (await api("POST", "/outbox/claim", env.SENDER_TOKEN, { claimId: "run-2" })).json()) as {
    items: { key: string }[];
  };
  expect(second.items).toHaveLength(1);
  expect(await getReminder(TARGET_KEY)).toMatchObject({ claim_id: "run-2", attempts: 2 });

  const stale = await api("POST", "/outbox/ack", env.SENDER_TOKEN, {
    claimId: "run-1",
    results: [{ key: TARGET_KEY, status: "sent", messageId: "late" }],
  });
  expect(await stale.json()).toEqual({ applied: 0 });

  const fresh = await api("POST", "/outbox/ack", env.SENDER_TOKEN, {
    claimId: "run-2",
    results: [{ key: TARGET_KEY, status: "sent", messageId: "WAMID.OK" }],
  });
  expect(await fresh.json()).toEqual({ applied: 1 });

  await storeCountsStatement(env.DB, Date.now()).run();
  const health = await (await exports.default.fetch("https://w.test/health")).json();
  expect(health).toMatchObject({ queued: 0, claimed: 0 });
  expect(await getReminder(TARGET_KEY)).toMatchObject({ status: "sent", message_id: "WAMID.OK" });
});

it("a reminder whose sender never acks ends failed after the last attempt", async () => {
  const deadline = NOW + (MAX_ATTEMPTS + 2) * LEASE_MS;
  await seedReminder({ key: "unacked", start_utc: isoUtc(deadline + LEASE_MS), deadline_utc: isoUtc(deadline) });
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    vi.setSystemTime(NOW + (attempt - 1) * LEASE_MS);
    const res = (await (await api("POST", "/outbox/claim", env.SENDER_TOKEN, { claimId: `run-${attempt}` })).json()) as {
      items: unknown[];
    };
    expect(res.items).toHaveLength(1);
  }
  vi.setSystemTime(NOW + MAX_ATTEMPTS * LEASE_MS);
  const none = (await (await api("POST", "/outbox/claim", env.SENDER_TOKEN, { claimId: "run-last" })).json()) as {
    items: unknown[];
  };
  expect(none.items).toEqual([]);
  expect(await getReminder("unacked")).toMatchObject({ status: "failed", attempts: MAX_ATTEMPTS });
  await storeCountsStatement(env.DB, Date.now()).run();
  const health = await (await exports.default.fetch("https://w.test/health")).json();
  expect(health).toMatchObject({ failed24h: 1, queued: 0, claimed: 0 });
});
