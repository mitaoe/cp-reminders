# Formatter and selection rules

Two pure modules decide what the bot says and about which contests:

- `worker/src/core/select.ts` picks the contests that get a reminder and decides when each reminder is due.
- `worker/src/core/format.ts` renders a contest into the reminder text.

The Worker calls both when it queues a reminder ([architecture.md](architecture.md#tick-workers-cron-every-5-minutes)).

## Message format

The text follows the community's own reminder posts. Its exact layout, the values each field may take and the rules for free text are defined in [reminder-format.md](reminder-format.md), which the sender enforces.

```
🚨 Contest Reminder 🚨

📍 Platform: Codeforces
🏆 Contest Name: Codeforces Round 1123 (Div. 2)
🕒 Timing: 8:05 PM
⏳ Duration: 2:15 hours
🔗 Contest Link: https://codeforces.com/contests/2267

📌 Tip: …
```

The tip line and the blank line before it appear only when a tip applies. How the formatter fills each field:

| Field | Rule |
|---|---|
| Platform | `Codeforces`, `Codechef`, `Atcoder` or `LeetCode`. |
| Contest name | The platform's display name, made valid free text ([below](#contest-names)). |
| Timing | Start time in IST on a 12-hour clock, such as `8:05 PM`. Start times without a zone are UTC. |
| Duration | `H:MM hours`, or `H hours` for whole hours, rounded to the minute. |
| Contest link | The contest page in the platform's shape ([below](#contest-links)). |

## Contest names

`contestName` first takes the platform's display name:

| Platform | Display name |
|---|---|
| Codeforces | Sponsored names become the round name: `WorldQuant Round 1 (Codeforces Round 1128, Div. 1 + Div. 2)` → `Codeforces Round 1128 (Div. 1 + Div. 2)`. |
| CodeChef | The trailing rating note is dropped: `Starters 254 (Rated till 6 star)` → `Starters 254`. |
| AtCoder | Sponsored names become the contest name, with ASCII or full-width parentheses: `JIJ Programming Contest 2026（AtCoder Beginner Contest 476）` → `AtCoder Beginner Contest 476`. |
| LeetCode | The name as given. |

It then makes the name valid free text as [reminder-format.md](reminder-format.md#free-text) defines it, in this order:

1. Format characters, such as bidirectional overrides, zero-width spaces and the zero-width joiner, are removed, so `evil`, a zero-width space (U+200B) and `.com` become `evil.com`, which the next steps defuse.
2. Every run of other characters that are not letters, marks, numbers, punctuation or symbols (spaces of any kind, tabs, line breaks, control characters) becomes one space, and the name is trimmed.
3. The dots of every domain and IP address become `․` (U+2024, one dot leader), until none is left: `Refact.ai Match 1` is posted as `Refact․ai Match 1`, and `1.2.3.4.5.6.7` as `1․2․3․4․5․6․7`. The name reads the same and WhatsApp does not turn it into a link.
4. A name longer than 200 code points is cut to 199 and ends with `…`.
5. If the result is still not valid free text, which happens for a name that is empty or holds a phone number, the name is `<Platform> contest`, such as `Codeforces contest`. `contestName` reports the replacement, and the Worker logs `contest name replaced by the fallback name: <key>` when it queues that reminder.

Every name the formatter renders therefore passes the sender's check.

## Contest links

`contestLink` reads clist's link leniently and writes the platform's contest page in the one shape [reminder-format.md](reminder-format.md#fields) defines. It accepts `http` or `https`, the host with or without `www.`, and a trailing `/`, and drops any query and fragment; it refuses another host or subdomain, a user name or a port. From the path it takes the contest's id:

| Platform | Path read | Contest link written |
|---|---|---|
| Codeforces | `/contest/<digits>` or `/contests/<digits>` | `https://codeforces.com/contests/<digits>`, the page that opens before the round starts; a `/contest/<id>` link opens only once the round has begun. |
| Codechef | `/<letters and digits>` | `https://www.codechef.com/<id>` |
| Atcoder | `/contests/<letters, digits, _ and ->` | `https://atcoder.jp/contests/<id>` |
| LeetCode | `/contest/<lowercase letters, digits and ->` | `https://leetcode.com/contest/<slug>/` |

Any other link, such as a Codeforces blog entry announcing a round, a ranking or problem page, or a link that would be longer than 200 characters, has no contest link. Such a contest is still selected, because whether a contest should be reminded does not depend on its link. It cannot be reminded, though, so it is reported twice:

- **Ahead of time.** Each snapshot refresh counts the selected contests whose deadline is still ahead and that have no contest link, stores the count as `unlinked_contests` and logs it. `/health` shows it as `unlinkedContests`, and the badge turns yellow (`degraded: contest link`), so the formatter can be fixed before the reminder is due.
- **When it is due.** The tick records its reminder as `failed` with `last_error` `no_contest_link` and no text, and logs `no contest link: <key>`. It is a miss: `/health` counts it in `failed24h` and the badge turns red. `/preview` shows the contest with that outcome instead of a text.

Every contest the rules select from clist's data of August 2025 to November 2026 has a contest link, which a test checks.

## Selection

`worker/config/rules.json`:

| Key | Value | Meaning |
|---|---|---|
| `include` | per platform, regular expressions on the contest name | A contest is posted only if its name matches one of its platform's patterns: every Codeforces contest, CodeChef Starters, LeetCode Weekly and Biweekly contests, AtCoder Beginner Contests (including sponsored ones, whose names contain "AtCoder Beginner Contest"). |
| `exclude` | per platform, regular expressions on the contest name | A contest whose name matches one of its platform's patterns is skipped even when included: on Codeforces, Kotlin Heroes, unrated rounds (`Unrated` in the name, which also covers ICPC mirrors and testing rounds) and the April Fools contest. Codeforces names its rated rounds in many ways (Hello 2026, Good Bye 2025, Pinely Round 5, sponsored rounds), so it is selected by exclusion. |
| `maxDurationHours` | `5` | Longer events are skipped, such as Kotlin Heroes practice rounds, heuristic contests and Placement Prep weekends. |
| `mergeCodeforcesDivs` | `true` | A Div. 1 and a Div. 2 round starting together get one reminder: the Div. 2 link, named `(Div. 1 + Div. 2)`. |
| `leadMinutes` | `45` | A reminder becomes due 45 minutes before the start. |
| `minLeadMinutes` | `10` | A reminder is not sent once the start is less than 10 minutes away. |

Contests of other platforms are never selected. The link plays no part in selection; a selected contest without a [contest link](#contest-links) is reported instead.

A reminder is therefore sendable from 45 to 10 minutes before the start. Each contest is reminded at most once, even if it is rescheduled ([architecture.md](architecture.md#design-decisions)).

## Tips

`worker/config/tips.json`:

- `rules` are checked in order and the first match wins. A rule can require a `platform`, a start before a given IST time (`startsBeforeIST`, such as `"12:00"` for morning contests) and a contest name matching `eventMatches`, and holds a pool of `texts`.
- One text from the matching rule's pool is used per contest, chosen from a hash of the contest link. The same contest always gets the same tip, so the preview shows exactly what is sent, and consecutive contests usually differ.
- `overrides` map a contest link, as clist gives it or as the reminder shows it, to a tip for that contest alone; an empty string suppresses the tip.

| Rule | Pool |
|---|---|
| Codeforces Div. 4 | Beginner encouragement: "never started cp? scared of cf?" and similar. |
| Codeforces Div. 3 | "never miss a div 3" and similar. |
| Codeforces Educational | "edu rounds are like leg days, you shouldn't skip them" and similar. |
| AtCoder Beginner Contest | "Best CP platform, so highly recommended for beginners and everyone." and similar. |
| LeetCode before noon IST | "morning contests build discipline" and similar. |

Every other contest, including CodeChef Starters, Codeforces Div. 2 and combined rounds and LeetCode Biweekly, has no tip unless an override gives it one. The tip texts are the owner's own lines and lines written in the same voice and approved by the owner; nothing is generated when a reminder is sent.

One-off tips set through `PUT /tips` are stored in D1 and take precedence over both ([scheduler.md](scheduler.md)).

Every tip, configured or one-off, is free text of at most 500 code points as [reminder-format.md](reminder-format.md#free-text) defines it: no link, domain, IP address, phone number or invisible character. `PUT /tips` refuses any other tip, a stored one-off tip that does not satisfy the rule is ignored in favour of the configured tip and logged as `ignoring the stored tip for <href>: not a valid tip`, and a test checks every tip in `tips.json`.

## Tests

`worker/test/core/format.test.ts` runs with the Worker tests against frozen data in `worker/test/fixtures/`: every reminder the community posted by hand from August 2025 to November 2026 (`history.json`), clist's contests for the same period (`clist-history.json`) and clist's upcoming contests (`clist-upcoming.json`).

- **Golden tests** use the real `rules.json` on purpose: they fail when a configuration change would break the community's format or skip a contest the community posts. For every manual reminder of the four platforms, the bot's platform, contest name, timing, duration and link for the same contest must equal the manual post; the tip line is not compared, because the manual tips were written per post. Every manually posted contest must be selected. The exceptions are listed in the test with a reason each, and a test fails if a listed post stops differing: 32 manual slips (a contest that started 10 to 15 minutes after the posted time, a duration or contest number copied from another post, a wrong division label, a singular Codeforces link, a sponsor name kept, a double space or typo) and 4 posts the rules do not select (2 unrated rounds, skipped by decision, and 2 links that are not clist's).
- **Template test.** The bot's text equals the community's latest post for the same contest byte for byte, apart from the tip line.
- **Name and link tests.** Hostile names are defused, cleaned, shortened or replaced by the fallback name, every variant of each platform's link maps to its contest link and every other link to none, selection ignores the link, and a contest without a contest link cannot be rendered. The Worker's tests cover how such a contest is reported at refresh, at tick, in `/health`, on the badge and in `/preview`.
- **Contract tests.** `testdata/reminders.json` must equal every reminder the formatter renders for the contests of the same period, with the configured tips, followed by the synthetic edge-case contests in `worker/test/contract.ts`; the sender's tests parse every text in it with the sender's own check. Every configured tip must be valid. The free-text checks of both languages are tested against `testdata/free-text.json` ([reminder-format.md](reminder-format.md#shared-test-data)).
- **Rule tests** define their own rules and tips in the test, so they hold whatever the configuration says: include and exclude patterns, the duration limit, Div. 1 and Div. 2 merging, the due window, sponsored names, the reminder key, tip rules, tip pools and overrides.

The Worker's other tests take every time and limit from the configuration and from the constants exported by its modules (lease, attempts, claim size, wake debounce, snapshot age, badge threshold, length limits), so changing any of them needs no test changes.

After any change to the formatter, `rules.json`, `tips.json` or the contest fixtures, regenerate `testdata/reminders.json` with `pnpm --filter cp-reminders-worker run contract` and commit it with the change.
