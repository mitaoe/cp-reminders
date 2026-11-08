import { isoUtc } from "./core/format.ts";
import { json } from "./http.ts";
import { getAllMeta } from "./meta.ts";
import type { Route } from "./router.ts";

const DAY_MS = 24 * 3600_000;

export const COUNTS_KEY = "reminder_counts";

export const REMINDER_COUNTS_SQL =
  "SELECT " +
  "(SELECT COUNT(*) FROM reminders WHERE status = 'queued') AS queued, " +
  "(SELECT COUNT(*) FROM reminders WHERE status = 'claimed') AS claimed, " +
  "(SELECT COUNT(*) FROM reminders WHERE status = 'failed' AND updated_at >= ?1) AS failed24h, " +
  "(SELECT COUNT(*) FROM reminders WHERE status = 'expired' AND updated_at >= ?1) AS expired24h";

interface Counts {
  queued: number;
  claimed: number;
  failed24h: number;
  expired24h: number;
}

export interface Health extends Counts {
  lastTickAt: string | null;
  lastFetchAt: string | null;
  lastFetchSource: string | null;
  lastFetchError: string | null;
  lastWakeError: string | null;
  unlinkedContests: number;
}

export const storeCountsStatement = (db: D1Database, nowMs: number): D1PreparedStatement =>
  db
    .prepare(
      "INSERT INTO meta (k, v) " +
        "SELECT ?2, json_object('queued', queued, 'claimed', claimed, 'failed24h', failed24h, 'expired24h', expired24h) " +
        `FROM (${REMINDER_COUNTS_SQL}) WHERE true ` +
        "ON CONFLICT(k) DO UPDATE SET v = excluded.v",
    )
    .bind(isoUtc(nowMs - DAY_MS), COUNTS_KEY);

const count = (value: unknown): number => (Number.isSafeInteger(value) && (value as number) >= 0 ? (value as number) : 0);

function parseCounts(value: string | undefined): Counts {
  let stored: Record<string, unknown> = {};
  try {
    const parsed: unknown = JSON.parse(value ?? "{}");
    if (parsed !== null && typeof parsed === "object") stored = parsed as Record<string, unknown>;
  } catch {
    stored = {};
  }
  return {
    queued: count(stored.queued),
    claimed: count(stored.claimed),
    failed24h: count(stored.failed24h),
    expired24h: count(stored.expired24h),
  };
}

export async function readHealth(db: D1Database): Promise<Health> {
  const meta = await getAllMeta(db);
  return {
    lastTickAt: meta.get("last_tick_at") ?? null,
    lastFetchAt: meta.get("last_fetch_at") ?? null,
    lastFetchSource: meta.get("last_fetch_source") ?? null,
    lastFetchError: meta.get("last_fetch_error") ?? null,
    lastWakeError: meta.get("last_wake_error") ?? null,
    ...parseCounts(meta.get(COUNTS_KEY)),
    unlinkedContests: count(Number(meta.get("unlinked_contests") ?? 0)),
  };
}

export const healthRoute: Route = {
  method: "GET",
  path: "/health",
  handle: async ({ env }) => json(await readHealth(env.DB)),
};
