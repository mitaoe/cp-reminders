import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { type Contest, formatReminder, utcMs } from "../src/core/format.ts";
import { selectContests } from "../src/core/select.ts";
import clistHistory from "./fixtures/clist-history.json" with { type: "json" };
import clistUpcoming from "./fixtures/clist-upcoming.json" with { type: "json" };

const ch = (cp: number) => String.fromCodePoint(cp);

const edgeCase = (resource_id: number, href: string, event: string): Contest => ({
  resource_id,
  event,
  start: "2026-12-01T14:35:00",
  duration: 8100,
  href,
});

export const EDGE_CASES: readonly Contest[] = [
  edgeCase(1, "https://codeforces.com/contests/9001", "Refact.ai Match 1 (Codeforces Round 985)"),
  edgeCase(1, "https://codeforces.com/contest/9002/", "Round 1.2.3.4.5.6.7 at evil.कॉम"),
  edgeCase(1, "https://codeforces.com/contests/9003", ch(0x1f525).repeat(260)),
  edgeCase(1, "https://codeforces.com/contests/9004", `Round${ch(0x202e)} 5${ch(0x200b)}\n(Div.${ch(0xa0)}2)`),
  edgeCase(1, "https://codeforces.com/contests/9005", "Call 98765 43210 for the round"),
  edgeCase(2, "https://www.codechef.com/START9006", `Starters 9006 at evil${ch(0xfe52)}com (Rated till 5 star)`),
  edgeCase(102, "https://leetcode.com/contest/weekly-contest-9007", "Weekly Contest 9007"),
];

export function renderContract(): string[] {
  const contests = [...(clistHistory.objects as unknown as Contest[]), ...(clistUpcoming.objects as unknown as Contest[])];
  const unique = [...new Map(contests.map((c) => [c.href, c])).values()];
  const selected = selectContests(unique, rules).sort((a, b) => utcMs(a.start) - utcMs(b.start) || a.href.localeCompare(b.href));
  return [...selected, ...EDGE_CASES].map((c) => formatReminder(c, tipRules, baseOverrides));
}
