import { describe, expect, test } from "vitest";
import { baseOverrides, rules, tipRules } from "../../src/config.ts";
import { type Contest, type Tip, formatDuration, formatReminder, formatTime } from "../../src/core/format.ts";
import { type Rules, dueReminders, reminderKey, selectContests } from "../../src/core/select.ts";
import { isValidTip } from "../../src/core/text.ts";
import contractTexts from "../../../testdata/reminders.json" with { type: "json" };
import { renderContract } from "../contract.ts";
import clistHistory from "../fixtures/clist-history.json" with { type: "json" };
import clistUpcoming from "../fixtures/clist-upcoming.json" with { type: "json" };
import history from "../fixtures/history.json" with { type: "json" };

const past = clistHistory.objects as unknown as Contest[];
const upcoming = clistUpcoming.objects as unknown as Contest[];
const all = [...new Map([...past, ...upcoming].map((c) => [c.href, c])).values()];
const norm = (u: string) =>
  u.trim().toLowerCase().replace(/\/+$/, "").replace("codeforces.com/contest/", "codeforces.com/contests/");
const byEvent = (event: string) => all.find((c) => c.event === event)!;

const FIELDS = ["Platform", "Contest Name", "Timing", "Duration", "Contest Link"] as const;
const field = (text: string, name: string) => text.match(new RegExp(`${name}:\\s*(.+)`))?.[1]?.trim();
const canon = (name: string, value: string | undefined) =>
  name === "Duration" ? value?.replace(/^(\d+):00 hours$/, "$1 hours") : value;
const PLATFORMS = new Set(["codeforces", "codechef", "leetcode", "atcoder"]);
const posts = history
  .map((h) => ({ date: h.postedIST.slice(0, 10), text: h.text, link: field(h.text, "Contest Link") }))
  .filter((p) => p.link && PLATFORMS.has(field(p.text, "Platform")?.toLowerCase() ?? ""));
const postKey = (p: { date: string; link?: string }) => `${p.date} ${p.link}`;

const TEST_RULES: Rules = {
  include: {
    Codeforces: ["Codeforces Round", "Kotlin Heroes"],
    Codechef: ["^Starters"],
    LeetCode: ["Contest"],
    Atcoder: ["Beginner Contest"],
  },
  maxDurationHours: 5,
  mergeCodeforcesDivs: true,
  leadMinutes: 60,
  minLeadMinutes: 15,
};

const NOT_SELECTED: Record<string, string> = {
  "2025-11-03 https://codeforces.com/contests/2168": "Testing Round 20 is unrated; unrated rounds are skipped",
  "2026-04-01 https://codeforces.com/contests/2214": "April Fools Day Contest is unrated; unrated rounds are skipped",
  "2025-12-03 https://www.codechef.com/START215C": "division link; clist lists the contest as START215",
  "2026-03-14 https://codeforces.com/contests/220": "link missing a digit",
};

const MANUAL_SLIPS: Record<string, string> = {
  "2025-08-20 https://www.codechef.com/START200": "2 hours posted for a 3-hour contest",
  "2025-08-21 https://codeforces.com/contest/2132": "singular /contest/ link",
  "2025-08-24 https://codeforces.com/contest/2133": "singular /contest/ link",
  "2025-08-26 https://codeforces.com/contest/2134": "singular /contest/ link",
  "2025-09-15 https://codeforces.com/contests/2144": "name shortened to (Div 2)",
  "2025-09-17 https://www.codechef.com/START200": "Starters 204 posted with Starters 200's link",
  "2025-09-21 https://codeforces.com/contests/2146": "duration written as 2.15 Hours",
  "2025-09-24 https://codeforces.com/contests/2151": "3 hours posted for a 3:15 round",
  "2025-10-05 https://codeforces.com/contests/2155": "duration written as 2.00 Hours",
  "2025-10-10 https://codeforces.com/contests/2153": "2.00 Hours posted for a 2:15 round",
  "2025-11-06 https://codeforces.com/contests/2164": "duration written as 03:00",
  "2025-11-16 https://codeforces.com/contests/2166": "simultaneous Div. 1 and Div. 2 posted as Div. 2",
  "2025-11-26 https://www.codechef.com/START214": "started at 8:15 PM, posted as 8:00 PM",
  "2025-12-23 https://codeforces.com/contests/2179": "started at 8:15 PM, posted as 8:05 PM",
  "2026-01-12 https://codeforces.com/contests/2184": "Div. 3 round posted as Div. 2",
  "2026-01-17 https://codeforces.com/contests/2191": "written as (Div. 1 + 2)",
  "2026-02-15 https://codeforces.com/contests/2195": "double space in the name; started at 8:15 PM, posted as 8:05 PM",
  "2026-02-25 https://codeforces.com/contests/2203": "Educational Round 187 posted as 186",
  "2026-02-27 https://codeforces.com/contests/2200": "double space in the name",
  "2026-03-16 https://codeforces.com/contests/2204": "2 hours posted for a 2:15 round",
  "2026-03-28 https://codeforces.com/contests/2211": "sponsor name kept; started at 8:15 PM, posted as 8:05 PM",
  "2026-04-13 https://codeforces.com/contests/2220": "simultaneous Div. 1 and Div. 2 posted as Div. 2",
  "2026-05-13 https://www.codechef.com/START238": "2 hours posted for a 2:30 contest",
  "2026-05-23 https://codeforces.com/contests/2229": "sponsor name kept",
  "2026-06-18 https://codeforces.com/contests/2237": "sponsor name kept",
  "2026-07-16 https://codeforces.com/contests/2245": "sponsor name kept; started at 8:15 PM, posted as 8:05 PM; 2:15 posted for 2:30",
  "2026-07-29 https://www.codechef.com/START249": "2 hours posted for a 2:30 contest",
  "2026-09-23 https://www.codechef.com/START257": "started at 8:15 PM, posted as 8:00 PM",
  "2026-10-07 https://codeforces.com/contests/2275": "started at 8:15 PM, posted as 8:05 PM",
  "2026-10-10 https://codeforces.com/contests/2272": "Round 1130's link posted for Round 1126",
  "2026-10-17 https://codeforces.com/contests/2261": "typo in the contest name",
  "2026-11-04 https://codeforces.com/contests/2283": "started at 8:15 PM, posted as 8:05 PM",
};

describe("golden: the configured rules reproduce the community's manual reminders, August 2025 to November 2026", () => {
  const byLink = new Map(selectContests(all, rules).map((c) => [norm(c.href), c]));

  test("every manually posted contest is selected, except the listed ones", () => {
    const missed = posts.filter((p) => !Object.hasOwn(NOT_SELECTED, postKey(p)) && !byLink.has(norm(p.link!))).map(postKey);
    expect(missed).toEqual([]);
  });

  test("platform, name, timing, duration and link equal every manual reminder, except listed slips", () => {
    const diffs: string[] = [];
    for (const p of posts) {
      const c = byLink.get(norm(p.link!));
      if (!c || Object.hasOwn(MANUAL_SLIPS, postKey(p))) continue;
      const bot = formatReminder(c);
      for (const name of FIELDS) {
        const manual = canon(name, field(p.text, name));
        const ours = canon(name, field(bot, name));
        if (manual !== ours) diffs.push(`${postKey(p)} ${name}: manual "${manual}" | bot "${ours}"`);
      }
    }
    expect(diffs).toEqual([]);
  });

  test("every listed post exists, and the listed slips really differ from the bot", () => {
    const keys = new Set(posts.map(postKey));
    expect([...Object.keys(NOT_SELECTED), ...Object.keys(MANUAL_SLIPS)].filter((k) => !keys.has(k))).toEqual([]);
    const identical = posts
      .filter((p) => Object.hasOwn(MANUAL_SLIPS, postKey(p)))
      .filter((p) => {
        const bot = formatReminder(byLink.get(norm(p.link!))!);
        return FIELDS.every((name) => canon(name, field(p.text, name)) === canon(name, field(bot, name)));
      })
      .map(postKey);
    expect(identical).toEqual([]);
  });
});

test("template: the bot's text equals the community's latest post for the same contest, apart from the tip", () => {
  const latest = history.at(-1)!;
  const contest = selectContests(all, rules).find((c) => norm(c.href) === norm(field(latest.text, "Contest Link")!))!;
  const withoutTip = (s: string) => s.replace(/\n\n📌 Tip: .*$/, "");
  expect(withoutTip(formatReminder(contest, tipRules, baseOverrides))).toBe(withoutTip(latest.text));
});

describe("contract with the sender", () => {
  test("testdata/reminders.json holds the reminders the formatter renders, which the sender's tests check", () => {
    expect(renderContract()).toEqual(contractTexts);
  });

  test("every configured tip is valid", () => {
    const tips = [...tipRules.flatMap((t) => t.texts), ...Object.values(baseOverrides).filter((t) => t !== "")];
    expect(tips.filter((t) => !isValidTip(t))).toEqual([]);
  });

  test("domains and IP addresses in contest names are defused", () => {
    const c = { ...all.find((x) => x.resource_id === 1)!, event: "Refact.ai Match 1 (Codeforces Round 985)" };
    expect(field(formatReminder(c), "Contest Name")).toBe(`Refact${String.fromCodePoint(0x2024)}ai Match 1 (Codeforces Round 985)`);
  });
});

test("time and duration helpers", () => {
  expect(formatTime(20, 5)).toBe("8:05 PM");
  expect(formatTime(8, 0)).toBe("8:00 AM");
  expect(formatTime(0, 5)).toBe("12:05 AM");
  expect(formatTime(12, 30)).toBe("12:30 PM");
  expect(formatDuration(7200)).toBe("2 hours");
  expect(formatDuration(5400)).toBe("1:30 hours");
  expect(formatDuration(8100)).toBe("2:15 hours");
  expect(formatDuration(6000)).toBe("1:40 hours");
});

describe("selection", () => {
  const names = (r: Rules) => selectContests(all, r).map((c) => c.event);

  test("keeps only contests whose name matches their platform's patterns", () => {
    const selected = names(TEST_RULES);
    expect(selected).toContain("Starters 264");
    expect(selected.some((n) => n.startsWith("Monday Munch"))).toBe(false);
    expect(selected.some((n) => n.startsWith("Placement Prep"))).toBe(false);
  });

  test("drops contests matching their platform's exclude patterns", () => {
    const codeforces = (event: string): Contest => ({
      resource_id: 1,
      event,
      start: "2026-12-27T14:35:00",
      duration: 3 * 3600,
      href: `https://codeforces.com/contests/${event.length}`,
    });
    const r: Rules = {
      ...TEST_RULES,
      include: { ...TEST_RULES.include, Codeforces: ["."] },
      exclude: { Codeforces: ["Kotlin Heroes", "Unrated", "April Fools"] },
    };
    const kept = selectContests(
      ["Hello 2026", "Good Bye 2025", "Pinely Round 5 (Div. 1 + Div. 2)", "Kotlin Heroes: Episode 13",
        "Testing Round 20 (Unrated, Communication Problems)", "April Fools Day Contest 2026"].map(codeforces),
      r,
    ).map((c) => c.event);
    expect(kept).toEqual(["Hello 2026", "Good Bye 2025", "Pinely Round 5 (Div. 1 + Div. 2)"]);
  });

  test("drops events longer than maxDurationHours", () => {
    const selected = names(TEST_RULES);
    expect(selected).toContain("Kotlin Heroes: Episode 15");
    expect(selected).not.toContain("Kotlin Heroes: Practice 15");
    expect(names({ ...TEST_RULES, maxDurationHours: 24 * 7 })).toContain("Kotlin Heroes: Practice 15");
  });

  test("merges simultaneous Codeforces Div. 1 and Div. 2 rounds into one Div. 2 reminder", () => {
    const merged = selectContests(all, TEST_RULES).filter((c) => c.event.startsWith("Codeforces Round 1133"));
    expect(merged.map((c) => c.event)).toEqual(["Codeforces Round 1133 (Div. 1 + Div. 2)"]);
    expect(merged[0]!.href).toBe(byEvent("Codeforces Round 1133 (Div. 2)").href);
    expect(names({ ...TEST_RULES, mergeCodeforcesDivs: false })).toEqual(
      expect.arrayContaining(["Codeforces Round 1133 (Div. 1)", "Codeforces Round 1133 (Div. 2)"]),
    );
  });
});

test("Codeforces links use the /contests/ page, which opens before the round starts", () => {
  const c = byEvent("Codeforces Round 1134 (Div. 4)");
  expect(formatReminder(c)).toMatch(/\n🔗 Contest Link: https:\/\/codeforces\.com\/contests\/2290$/);
  for (const href of ["https://codeforces.com/contest/2290", "https://codeforces.com/contest/2290/"]) {
    expect(formatReminder({ ...c, href })).toMatch(/\n🔗 Contest Link: https:\/\/codeforces\.com\/contests\/2290$/);
  }
});

test("sponsored Codeforces names are normalised", () => {
  const c = past.find((x) => x.event.startsWith("DeepInfra Round 1"))!;
  expect(formatReminder(c)).toMatch(/Contest Name: Codeforces Round 1126 \(Div\. 1 \+ Div\. 2\)\n/);
});

test("sponsored AtCoder names are normalised (full-width and ASCII parens)", () => {
  for (const [prefix, want] of [
    ["JIJ Programming", "AtCoder Beginner Contest 476"],
    ["UNICORNProgramming", "AtCoder Beginner Contest 477"],
    ["SMBC Programming", "AtCoder Beginner Contest 481"],
  ] as const) {
    const c = past.find((x) => x.event.startsWith(prefix))!;
    expect(formatReminder(c)).toMatch(new RegExp(`Contest Name: ${want}\\n`));
  }
});

test("due window: from leadMinutes before the start until minLeadMinutes before it", () => {
  const c = byEvent("Starters 264");
  const start = Date.parse(c.start + "Z");
  const at = (minBefore: number) => start - minBefore * 60_000;
  expect(dueReminders([c], TEST_RULES, at(TEST_RULES.leadMinutes) - 1000)).toHaveLength(0);
  expect(dueReminders([c], TEST_RULES, at(TEST_RULES.leadMinutes))).toHaveLength(1);
  expect(dueReminders([c], TEST_RULES, at(TEST_RULES.minLeadMinutes))).toHaveLength(1);
  expect(dueReminders([c], TEST_RULES, at(TEST_RULES.minLeadMinutes) + 1000)).toHaveLength(0);
});

test("reminder key identifies the contest, whatever its source or start time", () => {
  const c = byEvent("Starters 264");
  expect(reminderKey({ ...c, start: `${c.start}Z` })).toBe(reminderKey(c));
  expect(reminderKey({ ...c, start: "2026-11-11T14:45:00" })).toBe(reminderKey(c));
  expect(reminderKey(c)).toBe("2|https://www.codechef.com/START264");
});

describe("tips", () => {
  const morning = byEvent("Weekly Contest 527");
  const evening = byEvent("Biweekly Contest 195");
  const abc = byEvent("AtCoder Beginner Contest 484");
  const tips: Tip[] = [
    { platform: "LeetCode", startsBeforeIST: "12:00", texts: ["early"] },
    { platform: "Atcoder", eventMatches: "Beginner", texts: ["beginner"] },
  ];
  const pool = ["one", "two", "three", "four"];
  const pooled: Tip[] = [{ platform: "Atcoder", eventMatches: "Beginner", texts: pool }];
  const tipOf = (c: Contest) => formatReminder(c, pooled).match(/📌 Tip: (.*)$/)?.[1];
  const beginnerContests = all.filter((c) => c.resource_id === 93 && /Beginner Contest/.test(c.event));

  test("a pooled rule gives each contest one line from its pool, always the same for that contest", () => {
    for (const c of beginnerContests) {
      expect(pool).toContain(tipOf(c));
      expect(tipOf({ ...c })).toBe(tipOf(c));
    }
  });

  test("a pooled rule spreads its lines across contests", () => {
    expect(beginnerContests.length).toBeGreaterThanOrEqual(pool.length);
    expect(new Set(beginnerContests.map(tipOf)).size).toBeGreaterThan(1);
  });

  test("the first matching rule applies; a contest matching none gets no tip", () => {
    expect(formatReminder(morning, tips)).toMatch(/\n\n📌 Tip: early$/);
    expect(formatReminder(abc, tips)).toMatch(/\n\n📌 Tip: beginner$/);
    expect(formatReminder(evening, tips)).not.toMatch(/Tip/);
  });

  test("an override sets a tip for one contest, and an empty override suppresses it", () => {
    expect(formatReminder(morning, tips, { [morning.href]: "custom" })).toMatch(/Tip: custom$/);
    expect(formatReminder(evening, tips, { [evening.href]: "custom" })).toMatch(/Tip: custom$/);
    expect(formatReminder(morning, tips, { [morning.href]: "" })).not.toMatch(/Tip/);
  });
});
