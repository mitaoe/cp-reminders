import { type Contest, PLATFORM, contestLink, utcMs } from "./format.ts";

export interface Rules {
  include: Record<string, string[]>;
  exclude?: Record<string, string[]>;
  maxDurationHours: number;
  mergeCodeforcesDivs: boolean;
  leadMinutes: number;
  minLeadMinutes: number;
}

const PURE_DIV = { 1: /Div\.\s*1(?!\s*\+)\b/, 2: /Div\.\s*2\b/ } as const;
const COMBINED_DIV = /Div\.\s*1\s*\+/;

function isPureDiv(c: Contest, div: 1 | 2): boolean {
  return c.resource_id === 1 && PURE_DIV[div].test(c.event) && !COMBINED_DIV.test(c.event);
}

function matchesRules(c: Contest, rules: Rules): boolean {
  const platform = PLATFORM[c.resource_id];
  if (!platform || c.duration > rules.maxDurationHours * 3600) return false;
  const matches = (patterns: string[] | undefined) => (patterns ?? []).some((p) => new RegExp(p, "i").test(c.event));
  return matches(rules.include[platform]) && !matches(rules.exclude?.[platform]);
}

function mergeCodeforcesDivs(contests: Contest[]): Contest[] {
  const hasTwin = (c: Contest, div: 1 | 2) => contests.some((o) => o !== c && o.start === c.start && isPureDiv(o, div));
  return contests.flatMap((c) => {
    if (isPureDiv(c, 1) && hasTwin(c, 2)) return [];
    if (isPureDiv(c, 2) && hasTwin(c, 1)) return [{ ...c, event: c.event.replace(PURE_DIV[2], "Div. 1 + Div. 2") }];
    return [c];
  });
}

export function selectContests(list: readonly Contest[], rules: Rules): Contest[] {
  const kept = list.filter((c) => matchesRules(c, rules));
  return rules.mergeCodeforcesDivs ? mergeCodeforcesDivs(kept) : kept;
}

export const reminderKey = (c: Contest): string => `${c.resource_id}|${c.href}`;

export function dueReminders(list: readonly Contest[], rules: Rules, nowMs: number): Contest[] {
  return selectContests(list, rules).filter((c) => {
    const start = utcMs(c.start);
    return nowMs >= start - rules.leadMinutes * 60_000 && nowMs <= start - rules.minLeadMinutes * 60_000;
  });
}

export function unlinkedContests(list: readonly Contest[], rules: Rules, nowMs: number): Contest[] {
  return selectContests(list, rules).filter(
    (c) => nowMs <= utcMs(c.start) - rules.minLeadMinutes * 60_000 && contestLink(c) === undefined,
  );
}
