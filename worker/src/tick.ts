import { type Contest, type Tip, type TipOverrides, contestLink, contestName, formatReminder, isoUtc, utcMs } from "./core/format.ts";
import { dueReminders, reminderKey } from "./core/select.ts";
import { rules, tipRules } from "./config.ts";
import { ensureSnapshot } from "./contests.ts";
import type { Fetcher } from "./env.ts";
import { NO_CONTEST_LINK } from "./errors.ts";
import { setMeta } from "./meta.ts";
import { sweepStatements } from "./reminders.ts";
import { loadOverrides } from "./tips.ts";
import { maybeWake } from "./wake.ts";

export interface TickResult {
  fetched: boolean;
  queued: number;
  woke: boolean;
  errors: string[];
}

type Row =
  | { key: string; status: "queued"; text: string; nameReplaced: boolean }
  | { key: string; status: "failed"; text: ""; lastError: typeof NO_CONTEST_LINK };

function rowFor(c: Contest, tips: readonly Tip[], overrides: TipOverrides): Row {
  const key = reminderKey(c);
  if (contestLink(c) === undefined) return { key, status: "failed", text: "", lastError: NO_CONTEST_LINK };
  return { key, status: "queued", text: formatReminder(c, tips, overrides), nameReplaced: contestName(c).replaced };
}

function insertStatement(db: D1Database, c: Contest, row: Row, nowMs: number): D1PreparedStatement {
  const startMs = utcMs(c.start);
  return db
    .prepare(
      "INSERT INTO reminders (key, resource_id, href, start_utc, deadline_utc, text, status, last_error, created_at, updated_at) " +
        "VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?9) ON CONFLICT(key) DO NOTHING",
    )
    .bind(
      row.key,
      c.resource_id,
      c.href,
      isoUtc(startMs),
      isoUtc(startMs - rules.minLeadMinutes * 60_000),
      row.text,
      row.status,
      row.status === "failed" ? row.lastError : null,
      isoUtc(nowMs),
    );
}

function reportInserted(row: Row, errors: string[]): void {
  if (row.status === "queued") {
    if (row.nameReplaced) console.error(`contest name replaced by the fallback name: ${row.key}`);
    return;
  }
  const error = `no contest link: ${row.key}`;
  console.error(error);
  errors.push(error);
}

export async function runTick(env: Env, nowMs: number, fetcher: Fetcher): Promise<TickResult> {
  const errors: string[] = [];
  const snapshot = await ensureSnapshot(env, nowMs, fetcher);
  if (snapshot.error) errors.push(`contest fetch: ${snapshot.error}`);

  const due = dueReminders(snapshot.contests, rules, nowMs);
  const overrides = due.length > 0 ? await loadOverrides(env) : {};
  const rows = due.map((c) => rowFor(c, tipRules, overrides));

  const sweeps = sweepStatements(env.DB, nowMs);
  const outcomes = await env.DB.batch([
    ...sweeps,
    ...due.map((c, i) => insertStatement(env.DB, c, rows[i]!, nowMs)),
    setMeta(env.DB, "last_tick_at", isoUtc(nowMs)),
  ]);
  const inserted = rows.filter((_, i) => outcomes[sweeps.length + i]!.meta.changes > 0);
  for (const row of inserted) reportInserted(row, errors);
  const queued = inserted.filter((row) => row.status === "queued").length;

  const wake = await maybeWake(env, nowMs, fetcher);
  if (wake.error) errors.push(wake.error);
  return { fetched: snapshot.fetched, queued, woke: wake.woke, errors };
}
