# Reminder format

A reminder is the only kind of message the bot posts. This document is the contract between the two components that handle its text:

- the **formatter** in the Worker (`worker/src/core/format.ts`, TypeScript) renders it from a contest ([formatter.md](formatter.md));
- the **sender** (`sender/internal/reminder`, Go) parses every text it is asked to post and refuses anything that is not a reminder in this format ([sender.md](sender.md#content-checks)).

The sender does not trust the Worker, so the format is defined tightly enough that a text which passes can only be a contest reminder with a link to a contest page of its own platform.

## Layout

Seven lines, or nine with a tip, separated by `\n`:

```
🚨 Contest Reminder 🚨

📍 Platform: Codeforces
🏆 Contest Name: Codeforces Round 1134 (Div. 4)
🕒 Timing: 8:05 PM
⏳ Duration: 2:15 hours
🔗 Contest Link: https://codeforces.com/contests/2290

📌 Tip: never started cp? scared of cf?
```

| Line | Content |
|---|---|
| 1 | Exactly `🚨 Contest Reminder 🚨`. |
| 2 | Empty. |
| 3 | `📍 Platform: ` and the platform. |
| 4 | `🏆 Contest Name: ` and the contest name. |
| 5 | `🕒 Timing: ` and the start time in IST. |
| 6 | `⏳ Duration: ` and the duration. |
| 7 | `🔗 Contest Link: ` and the link. |
| 8 | Empty; only with a tip. |
| 9 | `📌 Tip: ` and the tip; only with a tip. |

Nothing else may come before, between or after these lines, and no line ends with `\r`.

## Fields

| Field | Allowed values |
|---|---|
| Platform | `Codeforces`, `Codechef`, `Atcoder` or `LeetCode`. |
| Contest name | Free text ([below](#free-text)), at most 200 code points. |
| Timing | A 12-hour time: hour `1` to `12` without a leading zero, two-digit minutes, a space and `AM` or `PM`, such as `8:05 PM`. |
| Duration | Whole hours as `2 hours`, or hours and minutes as `2:15 hours`, with one or two digits for the hours. |
| Link | The contest page of the platform, in exactly the shape below, at most 200 characters. |
| Tip | Free text, at most 500 code points. |

Lengths are counted in Unicode code points, which are Go's runes and the elements of `[...text]` in TypeScript, so both components count an emoji as one.

| Platform | Link |
|---|---|
| Codeforces | `https://codeforces.com/contests/<id>`, where the id is ASCII digits. |
| Codechef | `https://www.codechef.com/<code>`, where the code is ASCII letters and digits. |
| Atcoder | `https://atcoder.jp/contests/<id>`, where the id is ASCII letters, digits, `_` and `-`. |
| LeetCode | `https://leetcode.com/contest/<slug>/`, where the slug is lowercase ASCII letters, digits and `-`, and the link ends with `/`. |

Each shape is the contest's own page and nothing else: no other host or subdomain, no other page of the platform such as a blog entry or a ranking, no query or fragment. AtCoder ids include capitals, as in `https://atcoder.jp/contests/KeioPC2025`, so the AtCoder shape allows them.

## Free text

The contest name and the tip are the only fields whose content comes from outside: clist's contest names, and tips from `worker/config/tips.json` or `PUT /tips`. WhatsApp turns domains and phone numbers in a message into links and tap-to-call, so free text must not contain:

- `://`;
- a domain: labels of letters, marks, numbers and hyphens in any script, joined by dots, whose last label is a letter followed by at least one more letter or mark, such as `evil.com`, `evil.co.uk`, `evil.कॉम` or `evil.ලංකා`;
- an IPv4 address: four groups of one to three ASCII digits joined by dots;
- a phone number: ten or more decimal digits of any script, such as `９`, `९` or `٩`, optionally separated by space separators (including the no-break space, U+3000 and U+200A), dashes (including `–` and `—`), `(`, `)`, `.`, `/`, `~` and `−` (U+2212);
- any character that is not a letter, mark, number, punctuation, symbol or space separator: control characters, line and paragraph separators, and format characters such as bidirectional overrides, zero-width spaces and the zero-width joiner.

The dots are the ASCII dot `.`, `。` (U+3002), `．` (U+FF0E), `｡` (U+FF61) and `﹒` (U+FE52), all of which a client may treat as a domain separator. `․` (U+2024, one dot leader) is not a dot, so `Refact․ai` is safe.

Ordinary contest names stay safe: `Div.4`, `U.S. Open` and `3.14159` have no last label of two letters, and `ICPC 2025-2026` has eight digits. Emoji are symbols and are allowed, including a heart with its variation selector, which is a mark; emoji sequences joined with the zero-width joiner (U+200D), such as man, joiner, laptop for the technologist emoji, are refused, because the joiner is a format character.

Free text must also not be empty or only spaces.

## Who enforces what

| Component | Rule |
|---|---|
| Formatter | Renders the layout and the fields. Contest names are made valid free text by construction ([formatter.md](formatter.md#contest-names)); a name that cannot be made valid is replaced by `<Platform> contest`. A contest without a link of its platform's shape is not selected, so it gets no reminder. |
| `PUT /tips` | Accepts only a tip that is valid free text of at most 500 code points, or the empty string, which suppresses the tip for that contest. |
| Tip override loader | Ignores a stored override that is not a valid tip, so the contest gets its configured tip instead. |
| Configured tips | A Worker test checks every tip in `worker/config/tips.json`. |
| Sender | Parses the whole text with `reminder.Parse` and refuses it unless every rule above holds. A refused reminder is acknowledged as `rejected`, which the Worker records as `failed` at once ([architecture.md](architecture.md#reminder-lifecycle)). |

The free-text rules exist twice, in `worker/src/core/text.ts` and in `sender/internal/reminder`, so that the Worker can produce texts the sender accepts and the sender can refuse anything else on its own.

## Shared test data

Both implementations are tested against the same files in `testdata/`:

| File | Content | Checked by |
|---|---|---|
| `reminders.json` | Every reminder the formatter renders, with the configured tips, for the contests of August 2025 to November 2026 in `worker/test/fixtures/`, followed by synthetic contests with hostile names: domains, a dotted run of digits, invisible and control characters, a phone number, a name longer than the limit. | A Worker test requires the file to equal the formatter's current output; a Go test requires `reminder.Parse` to accept every text in it. |
| `free-text.json` | Safe and unsafe free-text examples: domains in several scripts, all dots, phone numbers with every separator and digit script, marks, emoji, invisible characters. Characters that would be invisible in the file are written as `<U+XXXX>`. | The TypeScript and the Go free-text checks must classify every example the same way. |

After a change to the formatter, its configuration or the contest fixtures, regenerate `reminders.json` with:

```sh
pnpm --filter cp-reminders-worker run contract
```

and commit it with the change. The Go test then shows whether the sender still accepts every reminder.
