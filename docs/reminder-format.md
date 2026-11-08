# Reminder format

A reminder is the only kind of message the bot posts. This document is the contract between the two components that handle its text:

- the **formatter** in the Worker (`worker/src/core/format.ts`, TypeScript) renders it from a contest ([formatter.md](formatter.md));
- the **sender** (`sender/internal/reminder`, Go) parses every text it is asked to post and refuses anything that is not a reminder in this format ([sender.md](sender.md#content-checks)).

The sender does not trust the Worker, so the format is defined tightly enough that a text which passes can only be a contest reminder with a link to the contest's own platform.

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
| Contest name | Free text ([below](#free-text)), at most 200 characters. |
| Timing | A 12-hour time: hour `1` to `12` without a leading zero, two-digit minutes, a space and `AM` or `PM`, such as `8:05 PM`. |
| Duration | Whole hours as `2 hours`, or hours and minutes as `2:15 hours`, with one or two digits for the hours. |
| Link | `https://`, optionally `www.`, the platform's host, and an optional path of `/`, ASCII letters, digits, `_`, `.` and `-`. No user name, port, query or fragment. |
| Tip | Free text, at most 500 characters. |

| Platform | Host |
|---|---|
| Codeforces | `codeforces.com` |
| Codechef | `codechef.com` |
| Atcoder | `atcoder.jp` |
| LeetCode | `leetcode.com` |

## Free text

The contest name and the tip are the only fields whose content comes from outside: clist's contest names, and tips from `worker/config/tips.json` or `PUT /tips`. WhatsApp turns domains and phone numbers in a message into links and tap-to-call, so free text must not contain:

- `://`;
- a domain: letters, digits or hyphens joined by dots into a name whose last part is at least two letters, with the ASCII dot `.` or the dots `。` (U+3002), `．` (U+FF0E) and `｡` (U+FF61);
- an IPv4 address, with the same dots;
- a phone number: ten or more digits, optionally separated by spaces, hyphens, dots or parentheses;
- any character that is not a letter, mark, number, punctuation, symbol or space separator: control characters, line and paragraph separators, and format characters such as bidirectional overrides and zero-width spaces.

It must also not be empty or only spaces.

## Who enforces what

| Component | Rule |
|---|---|
| Formatter | Renders the layout and the fields. Contest names pass through `defuseLinks`, which replaces the dots of every domain and IP address with `․` (U+2024, one dot leader), so a name such as `Refact.ai Match 1` is posted as `Refact․ai Match 1`, reads the same and is not a link. |
| `PUT /tips` | Accepts only a tip that is valid free text of at most 500 characters, or the empty string, which suppresses the tip for that contest. |
| Tip override loader | Ignores a stored override that is not a valid tip, so the contest gets its configured tip instead. |
| Configured tips | A Worker test checks every tip in `worker/config/tips.json`. |
| Sender | Parses the whole text with `reminder.Parse` and refuses it unless every rule above holds. |

The free-text rules exist twice, in `worker/src/core/text.ts` and in `sender/internal/reminder`, so that the Worker can produce texts the sender accepts and the sender can refuse anything else on its own.

## Shared test data

Both implementations are tested against the same files in `testdata/`:

| File | Content | Checked by |
|---|---|---|
| `reminders.json` | Every reminder the formatter renders, with the configured tips, for the contests of August 2025 to November 2026 in `worker/test/fixtures/`. | A Worker test requires the file to equal the formatter's current output; a Go test requires `reminder.Parse` to accept every text in it. |
| `free-text.json` | Safe and unsafe free-text examples. Characters that would be invisible in the file are written as `<U+XXXX>`. | The TypeScript and the Go free-text checks must classify every example the same way. |

After a change to the formatter, its configuration or the contest fixtures, regenerate `reminders.json` with:

```sh
pnpm --filter cp-reminders-worker run contract
```

and commit it with the change. The Go test then shows whether the sender still accepts every reminder.
