import { describe, expect, test } from "vitest";
import { type Contest, utcMs } from "../../src/core/format.ts";
import { type Rules, dueReminders, missedReminders, reminderWindow, unlinkedContests } from "../../src/core/select.ts";

const MIN = 60_000;

const RULES: Rules = {
  include: { Codechef: ["^Starters"], Codeforces: ["Codeforces Round"] },
  maxDurationHours: 5,
  mergeCodeforcesDivs: true,
  leadMinutes: 60,
  minLeadMinutes: 15,
};

const contest: Contest = {
  resource_id: 2,
  event: "Starters 264",
  start: "2026-11-11T14:30:00",
  duration: 7200,
  href: "https://www.codechef.com/START264",
};
const start = utcMs(contest.start);
const opens = start - RULES.leadMinutes * MIN;
const closes = start - RULES.minLeadMinutes * MIN;

describe("reminderWindow", () => {
  test("opens leadMinutes and closes minLeadMinutes before the start", () => {
    expect(reminderWindow(contest, RULES)).toEqual({ opensMs: opens, closesMs: closes });
  });
});

describe("dueReminders", () => {
  test("includes a contest from the moment its window opens until it closes", () => {
    expect(dueReminders([contest], RULES, opens - 1000)).toHaveLength(0);
    expect(dueReminders([contest], RULES, opens)).toHaveLength(1);
    expect(dueReminders([contest], RULES, closes)).toHaveLength(1);
    expect(dueReminders([contest], RULES, closes + 1000)).toHaveLength(0);
  });
});

describe("missedReminders", () => {
  test("includes a contest whose window lies entirely between the previous tick and now", () => {
    expect(missedReminders([contest], RULES, closes, opens - 1000)).toHaveLength(0);
    expect(missedReminders([contest], RULES, closes + 1000, opens - 1000)).toHaveLength(1);
    expect(missedReminders([contest], RULES, start + 10 * 60 * MIN, opens - 1000)).toHaveLength(1);
  });

  test("excludes a contest whose window was open at the previous tick", () => {
    expect(missedReminders([contest], RULES, closes + 1000, opens)).toHaveLength(0);
    expect(missedReminders([contest], RULES, closes + 1000, closes)).toHaveLength(0);
    expect(missedReminders([contest], RULES, closes + 1000, opens + 1000)).toHaveLength(0);
  });

  test("leaves every window that closes between two ticks to exactly one of them", () => {
    const times = [opens - 1000, opens, opens + 1000, closes - 1000, closes, closes + 1000];
    for (const sinceMs of times) {
      for (const nowMs of times.filter((t) => t > sinceMs)) {
        const dueBefore = dueReminders([contest], RULES, sinceMs).length;
        const dueNow = dueReminders([contest], RULES, nowMs).length;
        const missed = missedReminders([contest], RULES, nowMs, sinceMs).length;
        expect(missed * (dueBefore + dueNow)).toBe(0);
        if (sinceMs <= closes && closes < nowMs) expect(dueBefore + missed).toBe(1);
      }
    }
  });

  test("considers only selected contests", () => {
    const other = { ...contest, event: "Monday Munch 12", href: "https://www.codechef.com/MM12" };
    expect(missedReminders([other], RULES, closes + 1000, opens - 1000)).toHaveLength(0);
  });
});

describe("unlinkedContests", () => {
  const unlinked = { ...contest, resource_id: 1, event: "Codeforces Round 1 (Div. 3)", href: "https://codeforces.com/blog/entry/1" };

  test("counts selected contests without a contest link until their window closes", () => {
    expect(unlinkedContests([unlinked, contest], RULES, closes)).toEqual([unlinked]);
    expect(unlinkedContests([unlinked], RULES, closes + 1000)).toHaveLength(0);
  });
});
