import { env } from "cloudflare:workers";
import { beforeEach, describe, expect, it } from "vitest";
import { isoUtc } from "../src/core/format.ts";
import { LEASE_MS, MAX_ATTEMPTS, MAX_ERROR_LENGTH, ackReminders, claimReminders } from "../src/reminders.ts";
import { MIN, NOW, getReminder, resetDb, seedReminder } from "./helpers.ts";

beforeEach(resetDb);

async function claimed(key: string, attempts = 1, claimId = "c1") {
  await seedReminder({ key, status: "claimed", attempts, claim_id: claimId, claimed_at: isoUtc(NOW - 2 * MIN) });
}

describe("ackReminders", () => {
  it("marks a claimed reminder sent with the message id", async () => {
    await claimed("a");
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "sent", messageId: "WAMID.1" }], NOW);
    expect(applied).toBe(1);
    expect(await getReminder("a")).toMatchObject({
      status: "sent",
      message_id: "WAMID.1",
      sent_at: isoUtc(NOW),
      updated_at: isoUtc(NOW),
      last_error: null,
    });
  });

  it("requeues a failed reminder while attempts remain and records the error", async () => {
    await claimed("a", MAX_ATTEMPTS - 1);
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "failed", error: "socket closed" }], NOW);
    expect(applied).toBe(1);
    expect(await getReminder("a")).toMatchObject({
      status: "queued",
      claim_id: null,
      claimed_at: null,
      last_error: "socket closed",
      attempts: MAX_ATTEMPTS - 1,
    });
    const again = await claimReminders(env, "c2", NOW);
    expect(again.map((i) => i.key)).toEqual(["a"]);
  });

  it("marks a reminder failed once the last attempt fails", async () => {
    await claimed("a", MAX_ATTEMPTS);
    await ackReminders(env, "c1", [{ key: "a", status: "failed", error: "banned" }], NOW);
    expect(await getReminder("a")).toMatchObject({ status: "failed", last_error: "banned", attempts: MAX_ATTEMPTS });
    expect(await claimReminders(env, "c2", NOW)).toEqual([]);
  });

  it("marks a rejected reminder failed at once, whatever attempts remain", async () => {
    await claimed("a", 1);
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "rejected", error: "text is not in the reminder layout" }], NOW);
    expect(applied).toBe(1);
    expect(await getReminder("a")).toMatchObject({
      status: "failed",
      claim_id: "c1",
      last_error: "text is not in the reminder layout",
      attempts: 1,
      updated_at: isoUtc(NOW),
    });
    expect(await claimReminders(env, "c2", NOW + LEASE_MS + MIN)).toEqual([]);
  });

  it("ignores a rejection carrying a different claimId", async () => {
    await claimed("a", 1, "owner");
    expect(await ackReminders(env, "intruder", [{ key: "a", status: "rejected", error: "x" }], NOW)).toBe(0);
    expect(await getReminder("a")).toMatchObject({ status: "claimed", claim_id: "owner" });
  });

  it("ignores an ack carrying a different claimId", async () => {
    await claimed("a", 1, "owner");
    const applied = await ackReminders(env, "intruder", [{ key: "a", status: "sent", messageId: "x" }], NOW);
    expect(applied).toBe(0);
    expect(await getReminder("a")).toMatchObject({ status: "claimed", claim_id: "owner", message_id: null });
  });

  it("ignores an ack from a claim that was superseded after its lease expired", async () => {
    await seedReminder({ key: "a" });
    await claimReminders(env, "old", NOW);
    await claimReminders(env, "new", NOW + LEASE_MS);
    const applied = await ackReminders(env, "old", [{ key: "a", status: "failed", error: "late" }], NOW + LEASE_MS + MIN);
    expect(applied).toBe(0);
    expect(await getReminder("a")).toMatchObject({ status: "claimed", claim_id: "new" });
  });

  it("ignores unknown keys and handles a mixed batch", async () => {
    await claimed("a");
    await claimed("b");
    const applied = await ackReminders(
      env,
      "c1",
      [
        { key: "a", status: "sent", messageId: "m1" },
        { key: "missing", status: "sent", messageId: "m2" },
        { key: "b", status: "failed", error: "e" },
      ],
      NOW,
    );
    expect(applied).toBe(2);
    expect((await getReminder("a"))?.status).toBe("sent");
    expect((await getReminder("b"))?.status).toBe("queued");
  });

  it("is idempotent: a repeated ack changes nothing", async () => {
    await claimed("a");
    await ackReminders(env, "c1", [{ key: "a", status: "sent", messageId: "m1" }], NOW);
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "sent", messageId: "m2" }], NOW + MIN);
    expect(applied).toBe(0);
    expect(await getReminder("a")).toMatchObject({ message_id: "m1", sent_at: isoUtc(NOW) });
  });

  it("still records a sent ack for a reminder that expired while the message was in flight", async () => {
    await seedReminder({ key: "a", status: "expired", attempts: 1, claim_id: "c1", claimed_at: isoUtc(NOW - 20 * MIN) });
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "sent", messageId: "m1" }], NOW);
    expect(applied).toBe(1);
    expect((await getReminder("a"))?.status).toBe("sent");
  });

  it("does not resurrect an expired reminder from a failed ack", async () => {
    await seedReminder({ key: "a", status: "expired", attempts: 1, claim_id: "c1", claimed_at: isoUtc(NOW - 20 * MIN) });
    const applied = await ackReminders(env, "c1", [{ key: "a", status: "failed", error: "x" }], NOW);
    expect(applied).toBe(0);
    expect((await getReminder("a"))?.status).toBe("expired");
  });

  it("truncates oversized error text", async () => {
    await claimed("a");
    await ackReminders(env, "c1", [{ key: "a", status: "failed", error: "x".repeat(MAX_ERROR_LENGTH + 100) }], NOW);
    expect((await getReminder("a"))?.last_error).toHaveLength(MAX_ERROR_LENGTH);
  });
});
