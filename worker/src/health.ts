import { isoUtc } from "./core/format.ts";
import { json } from "./http.ts";
import { getAllMeta } from "./meta.ts";
import type { Route } from "./router.ts";

const DAY_MS = 24 * 3600_000;

export interface Health {
  lastTickAt: string | null;
  lastFetchAt: string | null;
  lastFetchSource: string | null;
  lastFetchError: string | null;
  lastWakeError: string | null;
  queued: number;
  claimed: number;
  failed24h: number;
  expired24h: number;
  unlinkedContests: number;
}

interface Counts {
  queued: number | null;
  claimed: number | null;
  failed24h: number | null;
  expired24h: number | null;
}

export async function readHealth(db: D1Database, nowMs: number): Promise<Health> {
  const [counts, meta] = await Promise.all([
    db
      .prepare(
        "SELECT " +
          "SUM(status = 'queued') AS queued, " +
          "SUM(status = 'claimed') AS claimed, " +
          "SUM(status = 'failed' AND updated_at >= ?1) AS failed24h, " +
          "SUM(status = 'expired' AND updated_at >= ?1) AS expired24h " +
          "FROM reminders",
      )
      .bind(isoUtc(nowMs - DAY_MS))
      .first<Counts>(),
    getAllMeta(db),
  ]);
  return {
    lastTickAt: meta.get("last_tick_at") ?? null,
    lastFetchAt: meta.get("last_fetch_at") ?? null,
    lastFetchSource: meta.get("last_fetch_source") ?? null,
    lastFetchError: meta.get("last_fetch_error") ?? null,
    lastWakeError: meta.get("last_wake_error") ?? null,
    queued: counts?.queued ?? 0,
    claimed: counts?.claimed ?? 0,
    failed24h: counts?.failed24h ?? 0,
    expired24h: counts?.expired24h ?? 0,
    unlinkedContests: Number(meta.get("unlinked_contests") ?? 0) || 0,
  };
}

export const healthRoute: Route = {
  method: "GET",
  path: "/health",
  handle: async ({ env, now }) => json(await readHealth(env.DB, now)),
};
