import { MAX_NAME_LENGTH, codePoints, defuseLinks, isValidName } from "./text.ts";

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

export const MAX_LINK_LENGTH = 200;

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

function platformOf(c: Contest): string {
  const platform = PLATFORM[c.resource_id];
  if (!platform) throw new Error(`unknown resource_id ${c.resource_id}`);
  return platform;
}

const SPONSORED_CODEFORCES = /\((Codeforces Round \d+),\s*([^)]*)\)\s*$/;
const SPONSORED_ATCODER = /[（(]\s*(AtCoder (?:Beginner|Regular|Grand) Contest\S*\s*\d+)\s*[)）]\s*$/;
const TRAILING_PARENTHETICAL = /\s*\([^)]*\)\s*$/;

function displayName(c: Contest): string {
  switch (c.resource_id) {
    case 1: {
      const m = c.event.match(SPONSORED_CODEFORCES);
      return m ? `${m[1]} (${m[2]})` : c.event;
    }
    case 2:
      return c.event.replace(TRAILING_PARENTHETICAL, "");
    case 93: {
      const m = c.event.match(SPONSORED_ATCODER);
      return m ? m[1] : c.event;
    }
    default:
      return c.event;
  }
}

const FORMAT_CHARACTERS = /\p{Cf}/gu;
const NOT_GRAPHIC_OR_SPACE = /[^\p{L}\p{M}\p{N}\p{P}\p{S}]+/gu;
const ELLIPSIS = "…";

function shorten(name: string): string {
  const points = [...name];
  return points.length <= MAX_NAME_LENGTH ? name : `${points.slice(0, MAX_NAME_LENGTH - 1).join("").trimEnd()}${ELLIPSIS}`;
}

export function contestName(c: Contest): string {
  const cleaned = displayName(c).replace(FORMAT_CHARACTERS, "").replace(NOT_GRAPHIC_OR_SPACE, " ").trim();
  const name = shorten(defuseLinks(cleaned));
  return isValidName(name) ? name : `${platformOf(c)} contest`;
}

const LINKS: Readonly<Record<number, { shape: RegExp; link: (id: string) => string }>> = {
  1: { shape: /^https:\/\/codeforces\.com\/contests?\/([0-9]+)\/?$/, link: (id) => `https://codeforces.com/contests/${id}` },
  2: { shape: /^https:\/\/www\.codechef\.com\/([A-Za-z0-9]+)$/, link: (id) => `https://www.codechef.com/${id}` },
  93: { shape: /^https:\/\/atcoder\.jp\/contests\/([A-Za-z0-9_-]+)$/, link: (id) => `https://atcoder.jp/contests/${id}` },
  102: { shape: /^https:\/\/leetcode\.com\/contest\/([a-z0-9-]+)\/?$/, link: (id) => `https://leetcode.com/contest/${id}/` },
};

export function contestLink(c: Contest): string | undefined {
  const platform = LINKS[c.resource_id];
  const id = platform && c.href.match(platform.shape)?.[1];
  if (!id) return undefined;
  const link = platform.link(id);
  return codePoints(link) <= MAX_LINK_LENGTH ? link : undefined;
}

function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 0x01000193);
  return h >>> 0;
}

function pickTip(c: Contest, tips: readonly Tip[], overrides: TipOverrides = {}): string | undefined {
  for (const key of [c.href, contestLink(c) ?? c.href]) {
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

const HEADER = "🚨 Contest Reminder 🚨";

export function formatReminder(c: Contest, tips: readonly Tip[] = [], overrides: TipOverrides = {}): string {
  const platform = platformOf(c);
  const link = contestLink(c);
  if (!link) throw new Error(`no ${platform} contest link in ${c.href}`);
  const { h, m } = istClock(utcMs(c.start));
  const lines = [
    HEADER,
    "",
    `📍 Platform: ${platform}`,
    `🏆 Contest Name: ${contestName(c)}`,
    `🕒 Timing: ${formatTime(h, m)}`,
    `⏳ Duration: ${formatDuration(c.duration)}`,
    `🔗 Contest Link: ${link}`,
  ];
  const tip = pickTip(c, tips, overrides);
  if (tip) lines.push("", `📌 Tip: ${tip}`);
  return lines.join("\n");
}
