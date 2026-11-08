import { isoUtc } from "./core/format.ts";

export interface ClaimedItem {
  key: string;
  text: string;
  startUtc: string;
  deadlineUtc: string;
}

export const LEASE_MS = 10 * 60_000;
export const MAX_ATTEMPTS = 3;
export const MAX_CLAIM = 5;

const ELIGIBLE =
  "deadline_utc >= ?1 AND attempts < ?3 AND (status = 'queued' OR (status = 'claimed' AND claimed_at <= ?2))";

export function sweepStatements(db: D1Database, nowMs: number): D1PreparedStatement[] {
  const now = isoUtc(nowMs);
  const leaseCut = isoUtc(nowMs - LEASE_MS);
  return [
    db
      .prepare("UPDATE reminders SET status = 'expired', updated_at = ?1 WHERE status = 'queued' AND deadline_utc < ?1")
      .bind(now),
    db
      .prepare(
        "UPDATE reminders SET status = 'expired', updated_at = ?1 " +
          "WHERE status = 'claimed' AND claimed_at <= ?2 AND deadline_utc < ?1",
      )
      .bind(now, leaseCut),
    db
      .prepare(
        "UPDATE reminders SET status = 'failed', last_error = 'lease expired after max attempts', updated_at = ?1 " +
          "WHERE status = 'claimed' AND claimed_at <= ?2 AND attempts >= ?3",
      )
      .bind(now, leaseCut, MAX_ATTEMPTS),
  ];
}

interface ClaimedRow {
  key: string;
  text: string;
  start_utc: string;
  deadline_utc: string;
}

export async function claimReminders(env: Env, claimId: string, nowMs: number): Promise<ClaimedItem[]> {
  const now = isoUtc(nowMs);
  const claim = env.DB
    .prepare(
      "UPDATE reminders SET status = 'claimed', claim_id = ?4, claimed_at = ?1, attempts = attempts + 1, updated_at = ?1 " +
        `WHERE key IN (SELECT key FROM reminders WHERE ${ELIGIBLE} ORDER BY start_utc, key LIMIT ?5) ` +
        "RETURNING key, text, start_utc, deadline_utc",
    )
    .bind(now, isoUtc(nowMs - LEASE_MS), MAX_ATTEMPTS, claimId, MAX_CLAIM);
  const results = await env.DB.batch<ClaimedRow>([...sweepStatements(env.DB, nowMs), claim]);
  const rows = results[results.length - 1]?.results ?? [];
  return rows
    .sort((a, b) => a.start_utc.localeCompare(b.start_utc) || a.key.localeCompare(b.key))
    .map((r) => ({ key: r.key, text: r.text, startUtc: r.start_utc, deadlineUtc: r.deadline_utc }));
}

export const ACK_STATUSES = ["sent", "failed", "rejected"] as const;

export interface AckResult {
  key: string;
  status: (typeof ACK_STATUSES)[number];
  messageId?: string;
  error?: string;
}

export const MAX_ERROR_LENGTH = 500;
const MAX_MESSAGE_ID_LENGTH = 200;

const errorText = (r: AckResult): string => (r.error ?? "unknown error").slice(0, MAX_ERROR_LENGTH);

function ackStatement(db: D1Database, claimId: string, r: AckResult, now: string): D1PreparedStatement {
  switch (r.status) {
    case "sent":
      return db
        .prepare(
          "UPDATE reminders SET status = 'sent', message_id = ?3, sent_at = ?4, updated_at = ?4, last_error = NULL " +
            "WHERE key = ?1 AND claim_id = ?2 AND status IN ('claimed', 'expired')",
        )
        .bind(r.key, claimId, (r.messageId ?? "").slice(0, MAX_MESSAGE_ID_LENGTH) || null, now);
    case "failed":
      return db
        .prepare(
          "UPDATE reminders SET " +
            "status = CASE WHEN attempts >= ?5 THEN 'failed' ELSE 'queued' END, " +
            "claim_id = CASE WHEN attempts >= ?5 THEN claim_id ELSE NULL END, " +
            "claimed_at = CASE WHEN attempts >= ?5 THEN claimed_at ELSE NULL END, " +
            "last_error = ?3, updated_at = ?4 " +
            "WHERE key = ?1 AND claim_id = ?2 AND status = 'claimed'",
        )
        .bind(r.key, claimId, errorText(r), now, MAX_ATTEMPTS);
    case "rejected":
      return db
        .prepare(
          "UPDATE reminders SET status = 'failed', last_error = ?3, updated_at = ?4 " +
            "WHERE key = ?1 AND claim_id = ?2 AND status = 'claimed'",
        )
        .bind(r.key, claimId, errorText(r), now);
  }
}

export async function ackReminders(env: Env, claimId: string, results: AckResult[], nowMs: number): Promise<number> {
  if (results.length === 0) return 0;
  const now = isoUtc(nowMs);
  const outcomes = await env.DB.batch(results.map((r) => ackStatement(env.DB, claimId, r, now)));
  return outcomes.reduce((sum, o) => sum + o.meta.changes, 0);
}

export async function hasPendingReminders(env: Env, nowMs: number): Promise<boolean> {
  const row = await env.DB
    .prepare(`SELECT 1 AS pending FROM reminders WHERE ${ELIGIBLE} LIMIT 1`)
    .bind(isoUtc(nowMs), isoUtc(nowMs - LEASE_MS), MAX_ATTEMPTS)
    .first();
  return row !== null;
}
