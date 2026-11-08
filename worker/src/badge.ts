import { type Health, readHealth } from "./health.ts";
import { json } from "./http.ts";
import type { Route } from "./router.ts";

const MIN_MS = 60_000;
export const DOWN_AFTER_MS = 15 * MIN_MS;
export const CACHE_CONTROL = "max-age=300";

export const LABEL_COLOR = "2f373f";
export const BADGE_COLOR = { ok: "29c64d", failing: "de2a39", degraded: "dbab09" } as const;

type BadgeColor = (typeof BADGE_COLOR)[keyof typeof BADGE_COLOR];

interface Badge {
  schemaVersion: 1;
  label: "bot";
  labelColor: typeof LABEL_COLOR;
  message: string;
  color: BadgeColor;
}

function status(health: Health, nowMs: number): Pick<Badge, "message" | "color"> {
  const lastTickMs = health.lastTickAt === null ? Number.NaN : Date.parse(health.lastTickAt);
  if (Number.isNaN(lastTickMs)) return { message: "never ticked", color: BADGE_COLOR.failing };
  const sinceTick = nowMs - lastTickMs;
  if (sinceTick > DOWN_AFTER_MS) {
    return { message: `no tick for ${Math.floor(sinceTick / MIN_MS)}m`, color: BADGE_COLOR.failing };
  }
  if (health.lastWakeError !== null) return { message: "dispatch failing", color: BADGE_COLOR.failing };
  const missed = health.failed24h + health.expired24h;
  if (missed > 0) return { message: `${missed} missed in 24h`, color: BADGE_COLOR.failing };
  if (health.unlinkedContests > 0) return { message: "degraded: contest link", color: BADGE_COLOR.degraded };
  if (health.lastFetchError !== null) return { message: "degraded: clist", color: BADGE_COLOR.degraded };
  return { message: "ok", color: BADGE_COLOR.ok };
}

export const badgeRoute: Route = {
  method: "GET",
  path: "/badge",
  async handle({ env, now }) {
    const badge: Badge = {
      schemaVersion: 1,
      label: "bot",
      labelColor: LABEL_COLOR,
      ...status(await readHealth(env.DB, now), now),
    };
    return json(badge, 200, CACHE_CONTROL);
  },
};
