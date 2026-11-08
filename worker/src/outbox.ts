import { badRequest, isRecord, json, readJson } from "./http.ts";
import { ACK_STATUSES, type AckResult, ackReminders, claimReminders } from "./reminders.ts";
import type { Route } from "./router.ts";

export const MAX_CLAIM_ID_LENGTH = 100;
export const MAX_ACK_RESULTS = 100;

function parseClaimId(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 && value.length <= MAX_CLAIM_ID_LENGTH ? value : undefined;
}

const isAckStatus = (value: unknown): value is AckResult["status"] => ACK_STATUSES.some((s) => s === value);

function parseResult(value: unknown): AckResult | undefined {
  if (!isRecord(value)) return undefined;
  const { key, status, messageId, error } = value;
  if (typeof key !== "string" || key.length === 0) return undefined;
  if (!isAckStatus(status)) return undefined;
  if (messageId !== undefined && typeof messageId !== "string") return undefined;
  if (error !== undefined && typeof error !== "string") return undefined;
  return { key, status, messageId, error };
}

export const outboxRoutes: Route[] = [
  {
    method: "POST",
    path: "/outbox/claim",
    secret: "SENDER_TOKEN",
    async handle({ request, env, now }) {
      const body = await readJson(request);
      const claimId = isRecord(body) ? parseClaimId(body.claimId) : undefined;
      if (claimId === undefined) return badRequest("claimId must be a non-empty string up to 100 characters");
      return json({ items: await claimReminders(env, claimId, now) });
    },
  },
  {
    method: "POST",
    path: "/outbox/ack",
    secret: "SENDER_TOKEN",
    async handle({ request, env, now }) {
      const body = await readJson(request);
      if (!isRecord(body)) return badRequest("body must be a JSON object");
      const claimId = parseClaimId(body.claimId);
      if (claimId === undefined) return badRequest("claimId must be a non-empty string up to 100 characters");
      if (!Array.isArray(body.results) || body.results.length > MAX_ACK_RESULTS) {
        return badRequest(`results must be an array of at most ${MAX_ACK_RESULTS} entries`);
      }
      const results: AckResult[] = [];
      for (const raw of body.results) {
        const parsed = parseResult(raw);
        if (!parsed) return badRequest(`each result needs a key and a status of ${ACK_STATUSES.join(", ")}`);
        results.push(parsed);
      }
      return json({ applied: await ackReminders(env, claimId, results, now) });
    },
  },
];
