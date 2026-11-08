import { env, exports } from "cloudflare:workers";
import { beforeEach, describe, expect, it } from "vitest";
import { MAX_ACK_RESULTS, MAX_CLAIM_ID_LENGTH } from "../src/outbox.ts";
import { MAX_ATTEMPTS } from "../src/reminders.ts";
import { NOW, getReminder, resetDb, seedReminder } from "./helpers.ts";

beforeEach(resetDb);

const post = (path: string, body: unknown, token: string | null = env.SENDER_TOKEN) =>
  exports.default.fetch(`https://w.test${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });

describe.each(["/outbox/claim", "/outbox/ack"])("%s auth", (path) => {
  it("rejects missing, wrong and other-role tokens", async () => {
    await seedReminder({ key: "a" });
    const body = { claimId: "c", results: [] };
    expect((await post(path, body, null)).status).toBe(401);
    expect((await post(path, body, "wrong")).status).toBe(401);
    expect((await post(path, body, env.TICK_TOKEN)).status).toBe(401);
    expect((await post(path, body, env.ADMIN_TOKEN)).status).toBe(401);
    expect((await getReminder("a"))?.status).toBe("queued");
  });

  it("rejects GET with 405", async () => {
    const res = await exports.default.fetch(`https://w.test${path}`, {
      headers: { Authorization: `Bearer ${env.SENDER_TOKEN}` },
    });
    expect(res.status).toBe(405);
  });
});

describe("POST /outbox/claim", () => {
  it("returns claimed items in the documented shape", async () => {
    const fakeNow = Date.now();
    await seedReminder({
      key: "a",
      start_utc: new Date(fakeNow + 70 * 60_000).toISOString(),
      deadline_utc: new Date(fakeNow + 60 * 60_000).toISOString(),
    });
    const res = await post("/outbox/claim", { claimId: "run-1" });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      items: [
        {
          key: "a",
          text: "text for a",
          startUtc: new Date(fakeNow + 70 * 60_000).toISOString(),
          deadlineUtc: new Date(fakeNow + 60 * 60_000).toISOString(),
        },
      ],
    });
    expect(await getReminder("a")).toMatchObject({ status: "claimed", claim_id: "run-1" });
  });

  it("returns an empty list when the outbox is empty", async () => {
    expect(await (await post("/outbox/claim", { claimId: "run-1" })).json()).toEqual({ items: [] });
  });

  it.each([
    ["invalid json", "{nope"],
    ["missing claimId", {}],
    ["empty claimId", { claimId: "" }],
    ["non-string claimId", { claimId: 5 }],
    ["oversized claimId", { claimId: "x".repeat(MAX_CLAIM_ID_LENGTH + 1) }],
  ])("rejects %s with 400", async (_name, body) => {
    expect((await post("/outbox/claim", body)).status).toBe(400);
  });
});

describe("POST /outbox/ack", () => {
  const claimed = (key: string, claimId = "run-1") =>
    seedReminder({ key, status: "claimed", attempts: 1, claim_id: claimId, claimed_at: new Date(NOW).toISOString() });

  it("applies results and reports how many were applied", async () => {
    await claimed("a");
    await claimed("b");
    await claimed("c");
    const res = await post("/outbox/ack", {
      claimId: "run-1",
      results: [
        { key: "a", status: "sent", messageId: "WAMID.1" },
        { key: "b", status: "failed", error: "closed" },
        { key: "c", status: "rejected", error: "text is not in the reminder layout" },
      ],
    });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ applied: 3 });
    expect(await getReminder("a")).toMatchObject({ status: "sent", message_id: "WAMID.1" });
    expect(await getReminder("b")).toMatchObject({ status: MAX_ATTEMPTS > 1 ? "queued" : "failed", last_error: "closed" });
    expect(await getReminder("c")).toMatchObject({ status: "failed", last_error: "text is not in the reminder layout" });
  });

  it("ignores an ack for a different claimId", async () => {
    await claimed("a", "owner");
    const res = await post("/outbox/ack", { claimId: "other", results: [{ key: "a", status: "sent", messageId: "m" }] });
    expect(await res.json()).toEqual({ applied: 0 });
    expect((await getReminder("a"))?.status).toBe("claimed");
  });

  it.each([
    ["invalid json", "[["],
    ["missing claimId", { results: [] }],
    ["results not an array", { claimId: "c", results: "x" }],
    ["unknown status", { claimId: "c", results: [{ key: "a", status: "done" }] }],
    ["missing key", { claimId: "c", results: [{ status: "sent" }] }],
    ["too many results", { claimId: "c", results: Array.from({ length: MAX_ACK_RESULTS + 1 }, (_, i) => ({ key: `k${i}`, status: "sent" })) }],
  ])("rejects %s with 400", async (_name, body) => {
    expect((await post("/outbox/ack", body)).status).toBe(400);
  });
});
