import { isoUtc } from "./core/format.ts";
import type { Fetcher } from "./env.ts";
import { UpstreamError, errorMessage, failureCode, httpFailure } from "./errors.ts";
import { deleteMeta, getMeta, setMeta } from "./meta.ts";
import { hasPendingReminders } from "./reminders.ts";

export interface WakeResult {
  woke: boolean;
  error?: string;
}

export const WAKE_DEBOUNCE_MS = 4 * 60_000;
const DISPATCH_TIMEOUT_MS = 8_000;

async function dispatch(env: Env, fetcher: Fetcher): Promise<void> {
  const res = await fetcher(`https://api.github.com/repos/${env.GH_REPO}/actions/workflows/send.yml/dispatches`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${env.GH_TOKEN}`,
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "cp-reminders-worker",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ ref: "main" }),
    signal: AbortSignal.timeout(DISPATCH_TIMEOUT_MS),
  });
  if (!res.ok) {
    throw new UpstreamError(
      httpFailure(res.status),
      `workflow dispatch HTTP ${res.status}: ${(await res.text()).slice(0, 200)}`,
    );
  }
}

async function shouldWake(env: Env, nowMs: number): Promise<boolean> {
  if (!(await hasPendingReminders(env, nowMs))) return false;
  const last = await getMeta(env.DB, "last_wake_at");
  return last === null || nowMs - Date.parse(last) >= WAKE_DEBOUNCE_MS;
}

export async function maybeWake(env: Env, nowMs: number, fetcher: Fetcher): Promise<WakeResult> {
  if (!(await shouldWake(env, nowMs))) return { woke: false };
  try {
    await dispatch(env, fetcher);
  } catch (e) {
    const error = errorMessage(e);
    console.error(`workflow dispatch failed: ${error}`);
    await setMeta(env.DB, "last_wake_error", failureCode(e)).run();
    return { woke: false, error };
  }
  await env.DB.batch([setMeta(env.DB, "last_wake_at", isoUtc(nowMs)), deleteMeta(env.DB, "last_wake_error")]);
  return { woke: true };
}
