import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { type Contest, formatReminder, utcMs } from "../src/core/format.ts";
import { selectContests } from "../src/core/select.ts";
import clistHistory from "./fixtures/clist-history.json" with { type: "json" };
import clistUpcoming from "./fixtures/clist-upcoming.json" with { type: "json" };

export function renderContract(): string[] {
  const contests = [...(clistHistory.objects as unknown as Contest[]), ...(clistUpcoming.objects as unknown as Contest[])];
  const unique = [...new Map(contests.map((c) => [c.href, c])).values()];
  return selectContests(unique, rules)
    .sort((a, b) => utcMs(a.start) - utcMs(b.start) || a.href.localeCompare(b.href))
    .map((c) => formatReminder(c, tipRules, baseOverrides));
}
