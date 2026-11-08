import { type Contest, PLATFORM, isoUtc, utcMs } from "./core/format.ts";
import type { Fetcher } from "./env.ts";
import { FAILURE, UpstreamError, errorMessage, failureCode, httpFailure } from "./errors.ts";
import { deleteMeta, setMeta } from "./meta.ts";

export type Source = "clist" | "codeforces";

export interface SnapshotResult {
  contests: Contest[];
  fetched: boolean;
  error?: string;
}

export const SNAPSHOT_TTL_MS = 60 * 60_000;
const REQUEST_TIMEOUT_MS = 8_000;

const isoNoZ = (ms: number) => isoUtc(ms).slice(0, 19);
const byStart = (a: Contest, b: Contest) => a.start.localeCompare(b.start);

interface CodeforcesContest {
  id: number;
  name: string;
  phase: string;
  durationSeconds: number;
  startTimeSeconds?: number;
}

interface CodeforcesResponse {
  status: string;
  comment?: string;
  result?: CodeforcesContest[];
}

async function fetchClist(env: Env, nowMs: number, fetcher: Fetcher): Promise<Contest[]> {
  const ids = Object.keys(PLATFORM).join(",");
  const url =
    `https://clist.by/api/v4/contest/?resource_id__in=${ids}` +
    `&start__gt=${isoNoZ(nowMs)}&order_by=start&limit=100`;
  const res = await fetcher(url, {
    headers: { Authorization: `ApiKey ${env.CLIST_USERNAME}:${env.CLIST_API_KEY}` },
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
  if (!res.ok) throw new UpstreamError(httpFailure(res.status), `clist HTTP ${res.status}`);
  const body = (await res.json()) as { objects?: unknown } | null;
  if (!Array.isArray(body?.objects)) throw new UpstreamError(FAILURE.invalid, "clist returned no contest list");
  const contests = (body.objects as Contest[]).map(({ resource_id, event, start, duration, href }) => ({
    resource_id,
    event,
    start,
    duration,
    href,
  }));
  if (contests.length === 0) throw new UpstreamError(FAILURE.empty, "clist returned no contests");
  return contests;
}

async function fetchCodeforces(fetcher: Fetcher): Promise<Contest[]> {
  const res = await fetcher("https://codeforces.com/api/contest.list?gym=false", {
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
  if (!res.ok) throw new UpstreamError(httpFailure(res.status), `codeforces HTTP ${res.status}`);
  const body = (await res.json()) as CodeforcesResponse | null;
  if (body?.status !== "OK" || !Array.isArray(body.result)) {
    throw new UpstreamError(FAILURE.invalid, `codeforces: ${body?.comment ?? body?.status ?? "no contest list"}`);
  }
  const contests: Contest[] = [];
  for (const c of body.result) {
    if (c.phase !== "BEFORE" || c.startTimeSeconds === undefined) continue;
    contests.push({
      resource_id: 1,
      event: c.name,
      start: isoNoZ(c.startTimeSeconds * 1000),
      duration: c.durationSeconds,
      href: `https://codeforces.com/contests/${c.id}`,
    });
  }
  if (contests.length === 0) throw new UpstreamError(FAILURE.empty, "codeforces returned no upcoming contests");
  return contests.sort(byStart);
}

interface SnapshotRow {
  fetched_at: string;
  payload: string;
}

async function saveSnapshot(
  env: Env,
  nowMs: number,
  source: Source,
  contests: Contest[],
  error: string | null,
): Promise<void> {
  const fetchedAt = isoUtc(nowMs);
  await env.DB.batch([
    env.DB
      .prepare(
        "INSERT INTO contest_snapshot (id, source, fetched_at, payload) VALUES (1, ?1, ?2, ?3) " +
          "ON CONFLICT(id) DO UPDATE SET source = excluded.source, fetched_at = excluded.fetched_at, payload = excluded.payload",
      )
      .bind(source, fetchedAt, JSON.stringify(contests)),
    setMeta(env.DB, "last_fetch_at", fetchedAt),
    setMeta(env.DB, "last_fetch_source", source),
    error === null ? deleteMeta(env.DB, "last_fetch_error") : setMeta(env.DB, "last_fetch_error", error),
  ]);
}

export async function ensureSnapshot(env: Env, nowMs: number, fetcher: Fetcher): Promise<SnapshotResult> {
  const row = await env.DB.prepare("SELECT fetched_at, payload FROM contest_snapshot WHERE id = 1").first<SnapshotRow>();
  const previous: Contest[] = row ? (JSON.parse(row.payload) as Contest[]) : [];
  if (row && nowMs - Date.parse(row.fetched_at) < SNAPSHOT_TTL_MS) return { contests: previous, fetched: false };

  const clist = await attempt("clist", () => fetchClist(env, nowMs, fetcher));
  if ("contests" in clist) {
    await saveSnapshot(env, nowMs, "clist", clist.contests, null);
    return { contests: clist.contests, fetched: true };
  }

  const codeforces = await attempt("codeforces", () => fetchCodeforces(fetcher));
  if ("contests" in codeforces) {
    const others = previous.filter((c) => c.resource_id !== 1 && utcMs(c.start) > nowMs);
    const contests = [...codeforces.contests, ...others].sort(byStart);
    await saveSnapshot(env, nowMs, "codeforces", contests, clist.failure.code);
    return { contests, fetched: true, error: clist.failure.detail };
  }

  await setMeta(env.DB, "last_fetch_error", `${clist.failure.code}; ${codeforces.failure.code}`).run();
  return { contests: previous, fetched: false, error: `${clist.failure.detail}; ${codeforces.failure.detail}` };
}

interface Failure {
  code: string;
  detail: string;
}

type Attempt = { contests: Contest[] } | { failure: Failure };

async function attempt(source: Source, fetch: () => Promise<Contest[]>): Promise<Attempt> {
  try {
    return { contests: await fetch() };
  } catch (e) {
    const detail = errorMessage(e);
    console.error(`${source} fetch failed: ${detail}`);
    return { failure: { code: `${source}: ${failureCode(e)}`, detail } };
  }
}
