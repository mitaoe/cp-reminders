export interface Contest {
  resource_id: number;
  event: string;
  start: string;
  duration: number;
  href: string;
}

export interface Tip {
  platform?: string;
  startsBeforeIST?: string;
  eventMatches?: string;
  texts: string[];
}

export type TipOverrides = Record<string, string>;

export const PLATFORM: Readonly<Record<number, string>> = {
  1: "Codeforces",
  2: "Codechef",
  93: "Atcoder",
  102: "LeetCode",
};

export const IST_OFFSET_MS = 330 * 60_000;

export const isoUtc = (ms: number): string => new Date(ms).toISOString();

export function utcMs(s: string): number {
  const iso = /[zZ]|[+-]\d\d:?\d\d$/.test(s) ? s : `${s}Z`;
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) throw new Error(`bad start time: ${s}`);
  return ms;
}

export function istClock(ms: number): { h: number; m: number } {
  const d = new Date(ms + IST_OFFSET_MS);
  return { h: d.getUTCHours(), m: d.getUTCMinutes() };
}

function hhmmToMinutes(s: string): number {
  const [h, m] = s.split(":").map(Number);
  return h * 60 + m;
}

export function formatTime(h: number, m: number): string {
  return `${h % 12 || 12}:${String(m).padStart(2, "0")} ${h >= 12 ? "PM" : "AM"}`;
}

export function formatDuration(seconds: number): string {
  const mins = Math.round(seconds / 60);
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return m === 0 ? `${h} hours` : `${h}:${String(m).padStart(2, "0")} hours`;
}

const SPONSORED_CODEFORCES = /\((Codeforces Round \d+),\s*([^)]*)\)\s*$/;
const SPONSORED_ATCODER = /[（(]\s*(AtCoder (?:Beginner|Regular|Grand) Contest\S*\s*\d+)\s*[)）]\s*$/;
const TRAILING_PARENTHETICAL = /\s*\([^)]*\)\s*$/;

function displayName(c: Contest): string {
  switch (c.resource_id) {
    case 1: {
      const m = c.event.match(SPONSORED_CODEFORCES);
      return m ? `${m[1]} (${m[2]})` : c.event.trim();
    }
    case 2:
      return c.event.replace(TRAILING_PARENTHETICAL, "").trim();
    case 93: {
      const m = c.event.match(SPONSORED_ATCODER);
      return m ? m[1] : c.event.trim();
    }
    default:
      return c.event.trim();
  }
}

const CODEFORCES_CONTEST_LINK = /^https:\/\/codeforces\.com\/contests?\/(\d+)\/?$/;

function displayLink(c: Contest): string {
  switch (c.resource_id) {
    case 1: {
      const m = c.href.match(CODEFORCES_CONTEST_LINK);
      return m ? `https://codeforces.com/contests/${m[1]}` : c.href;
    }
    case 102:
      return c.href.endsWith("/") ? c.href : `${c.href}/`;
    default:
      return c.href;
  }
}

function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 0x01000193);
  return h >>> 0;
}

function pickTip(c: Contest, tips: readonly Tip[], overrides: TipOverrides = {}): string | undefined {
  for (const key of [c.href, displayLink(c)]) {
    if (Object.hasOwn(overrides, key)) return overrides[key] || undefined;
  }
  const platform = PLATFORM[c.resource_id];
  const { h, m } = istClock(utcMs(c.start));
  const tip = tips.find(
    (t) =>
      (!t.platform || t.platform === platform) &&
      (!t.startsBeforeIST || h * 60 + m < hhmmToMinutes(t.startsBeforeIST)) &&
      (!t.eventMatches || new RegExp(t.eventMatches, "i").test(c.event)),
  );
  return tip?.texts.length ? tip.texts[fnv1a(c.href) % tip.texts.length] : undefined;
}

export const HEADER = "🚨 Contest Reminder 🚨";

export function formatReminder(c: Contest, tips: readonly Tip[] = [], overrides: TipOverrides = {}): string {
  const platform = PLATFORM[c.resource_id];
  if (!platform) throw new Error(`unknown resource_id ${c.resource_id}`);
  const { h, m } = istClock(utcMs(c.start));
  const lines = [
    HEADER,
    "",
    `📍 Platform: ${platform}`,
    `🏆 Contest Name: ${displayName(c)}`,
    `🕒 Timing: ${formatTime(h, m)}`,
    `⏳ Duration: ${formatDuration(c.duration)}`,
    `🔗 Contest Link: ${displayLink(c)}`,
  ];
  const tip = pickTip(c, tips, overrides);
  if (tip) lines.push("", `📌 Tip: ${tip}`);
  return lines.join("\n");
}
