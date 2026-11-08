# Sender

The sender (`sender/`, Go) is the only component that talks to WhatsApp. It claims queued reminders from the Worker's outbox, checks that each one is a reminder in the agreed format, posts it to the community's Announcements group, records what it delivered and acknowledges the results. It is an unofficial linked device built on [whatsmeow](https://pkg.go.dev/go.mau.fi/whatsmeow) and runs as a short GitHub Actions job ([workflow.md](workflow.md#sendyml-drain-the-outbox)). It never decides what to send or when, and never takes a destination from the network.

## Commands

Run from the repository root as `go -C sender run ./cmd/wa <command>`; a relative `--state` resolves from `sender/`. Every command accepts `-h`.

| Command | Behaviour | Connects to WhatsApp |
|---|---|---|
| `wa keygen` | Prints a new age identity (`AGE-SECRET-KEY-1…`) and its public key. | No |
| `wa pair --state DIR [--phone 91XXXXXXXXXX] [--force]` | Links a new device, by QR code or, with `--phone`, by an 8-character pairing code. Waits for the first login, stays connected 10 s, disconnects and writes `DIR/session.db.age`. Refuses to replace an existing session without `--force`. | Yes |
| `wa groups --state DIR` | Lists joined groups with their community flags and our admin status, marks every community Announcements group with an `ANNOUNCEMENTS CANDIDATE:` line, and saves the session. | Yes |
| `wa drain --state DIR [--jitter 90s] [--dry-run]` | One claim, deliver and acknowledge cycle ([Drain](#drain)). | Only when something is pending |

Shared flags: `--log-level` (whatsmeow and libsignal log level, `WARN` by default) and `--connect-timeout` (60 s). `drain` and `groups` take `--timeout` for the whole run (4 minutes and 90 seconds); `pair` allows 5 minutes.

Exit codes: `0` on success, including an empty outbox; `1` on any failure; `2` on a usage error.

## Environment

| Variable | Commands | Meaning |
|---|---|---|
| `AGE_IDENTITY` | pair, groups, drain | The age X25519 identity that encrypts and decrypts `session.db.age`. Surrounding whitespace is ignored; the value is never printed, including in parse errors. |
| `ANNOUNCE_JID` | drain | The only chat the sender may post to: a group JID such as `120363…@g.us`, found with `wa groups`. |
| `WORKER_URL` | drain | Base URL of the Worker; the sender calls `/outbox/claim` and `/outbox/ack`. |
| `SENDER_TOKEN` | drain | Bearer token for `/outbox/*`. |
| `GITHUB_STEP_SUMMARY` | drain | Set by GitHub Actions. On failure the sender appends every unsent reminder that passed validation to it, in a fenced block ready to paste. |

## State directory

| File | Content |
|---|---|
| `session.db.age` | The whatsmeow SQLite store, encrypted to the public key of `AGE_IDENTITY`, so one secret both reads and writes it. Replaced atomically on every save. |
| `sent.json` | The last 500 delivered reminders as `{key, sentAt, messageId}`, written after every send. |

While a command runs, the decrypted database lives in a private temporary directory (`cp-reminders-wa-*`), which is removed on every exit path. Signal keys rotate on every connection, so once a session has been opened, every exit that disconnects re-encrypts it; only a failed pairing writes nothing.

## Drain

1. Check the configuration before any network call: the shape of `ANNOUNCE_JID`, a valid `AGE_IDENTITY` and a readable `sent.json`.
2. `POST /outbox/claim {claimId}` with a random `claimId`. A claim is never retried: a retry after a lost response would leave the first claim's rows waiting for their lease to expire.
3. With no items, print `outbox empty` and exit without decrypting the session or connecting.
4. Validate the items ([content checks](#content-checks)). A claim of more than 5 items is refused as a whole; each key must be non-empty and unique, and each text must be a reminder in the agreed format. Refused items are acknowledged as `rejected` and never sent; the Worker marks them `failed` without another attempt.
5. Items whose key is already in `sent.json` are acknowledged as `sent` with the stored message ID and not sent again.
6. Apply the daily limit: at most 8 messages in any 24 hours, counted from `sent.json` ([content checks](#content-checks)). Pending items beyond it are acknowledged as `failed` ("daily limit") and the run fails. If nothing is left to send, WhatsApp is not contacted.
7. Wait a random time between 0 and `--jitter`, decrypt the session, connect and wait for login.
8. Check the target with `GetGroupInfo(ANNOUNCE_JID)`: it must be a community's default subgroup (`IsDefaultSubGroup`), and the sender must be admin or super admin, matched by phone-number JID or LID, ignoring device parts.
9. Send each pending item as a plain text message (`waE2E.Message{Conversation: …}`), which carries no link preview, and record it in `sent.json` straight away. The first send error or `sent.json` write error stops sending; the remaining items are reported as `failed` ("not attempted").
10. Disconnect and encrypt a snapshot of the session, so the rotated keys are saved even if the acknowledgement hangs. The decrypted store stays open ([shutdown](#shutdown)).
11. `POST /outbox/ack {claimId, results}`, retrying network errors, 5xx and 429 up to 3 attempts with 1 s and 2 s backoff, within its own 60-second budget.
12. Release the session, whatever the result of the run: save it unless the most recent save succeeded, then close the store and remove the decrypted copy.

Any failure (refused item, daily limit, connect, logged out, stream replaced, temporary ban, refused target, send, acknowledgement or session save) exits with code 1 and writes the job summary.

Each claimed item is acknowledged with one of three statuses:

| Status | When | What the Worker does |
|---|---|---|
| `sent` | Delivered in this run, or found in `sent.json`. | Marks the row `sent` with the message ID. |
| `rejected` | Refused by validation. | Marks the row `failed` at once, whatever attempts remain. |
| `failed` | Valid, but not delivered: daily limit, connection or target failure, send error, not attempted. | Queues the row again while attempts remain, otherwise marks it `failed`. Each claim uses one of the 3 attempts. |

`--dry-run` claims, connects, checks the target and prints the texts; it does not send, write `sent.json` or acknowledge, so the claimed rows return to the queue when their 10-minute lease expires.

Message text appears only in `--dry-run` output and in the failure summary, never in the normal log.

### Content checks

The sender trusts nothing the Worker hands it. Whoever controls the Worker, its deployment or `ADMIN_TOKEN` can put text into the outbox, so the sender, which holds the WhatsApp session, decides what may be posted.

- **Format.** `internal/reminder` parses every text against the reminder format ([reminder-format.md](reminder-format.md)): the fixed lines in their order, a known platform, a time and a duration of the agreed shape, a link of at most 200 characters in the exact shape of the platform's contest page, and a contest name of at most 200 and a tip of at most 500 code points that contain no link, domain, IP address, phone number or invisible character. A text that does not parse is refused whole; nothing in it is posted.
- **Daily limit.** At most 8 messages are sent in any 24 hours, counted from `sent.json`; no 24 hours of the 15 months of contests in the test data needed more than 5. The limit keeps a flood of queued texts from becoming a burst of posts that risks a ban of the number. A held-back reminder is valid, so it is acknowledged as `failed` and uses an attempt; later runs send it while attempts remain and its deadline has not passed. `sent.json` reaches branch `state` only through the Persist State job, and whoever holds the Worker's `GH_TOKEN` can cancel a run before that job saves it, so the count can be rolled back: the limit bounds bursts within the runs that complete, not the total over time.

### Shutdown

The decrypted store holds the device's Signal state: its identity, sessions, sender keys and pre-keys, plus app state and contact details. It does not hold the sent messages; the record of what was delivered is `sent.json`.

whatsmeow keeps using the store after `Disconnect()` returns ([library behaviour](#library-behaviour-the-code-depends-on)), so the session is saved without closing the store, and closed only when the command ends:

1. **Save** runs after `Disconnect()` returns. whatsmeow writes key changes to the store before sending anything that depends on them, and nothing can be sent after `Disconnect()`, so the store then holds every key a later run needs. Save writes a consistent copy of the open store with SQLite's `VACUUM INTO`, encrypts it into `session.db.age` and deletes the copy. Readers continue while it runs; writers wait for it, up to the 10-second `busy_timeout`.
2. **The store stays open** while the drain acknowledges, so whatsmeow's leftover goroutines do not fail on a closed database. With the socket closed they fail at their network step instead: app state sync logs that at debug level, and others, such as a pre-key upload, may still log an error. What they write to the store after the snapshot is not kept, for example sessions deleted after an identity-change notification; the next run starts from the snapshot.
3. **Release** closes the store and removes the decrypted copy, after a successful or a failed run. Unless the most recent save succeeded, it saves first: after a failed save, and also for a session that was opened but never saved, so a session is never closed without having been written.

`--dry-run` and `wa groups` release right after saving, and `wa pair` and a failed connection save and close in one step, so in those cases leftover goroutines can still log errors about a closed database.

Errors are reported as follows:

- A failed first save, right after `Disconnect()`, is part of the run's result: the run fails, the job summary shows it as `close session: save session: …`, and the command exits with code 1. A successful save in Release afterwards does not clear that error; the run still exits with code 1.
- A failure inside Release itself (its save or closing the store) makes the command exit with code 1, so the workflow run fails and GitHub emails the owner, but no job summary is written for it.

### Public output

Workflow logs and job summaries of the deployment repository are public, so `drain` keeps WhatsApp identities out of them.

`internal/redact` rewrites:

- every WhatsApp ID as `types.JID` writes it, to `<id>@` and its server (`<id>@s.whatsapp.net`, `<id>@g.us`, `<id>@lid`);
- every standalone run of ten or more digits, including the agent suffix of a Signal address (`123456789012345_1:5` becomes `<number>:5`), to `<number>`. Digits that follow a dot, such as the last part of the WhatsApp client version, are kept.

It is applied to:

| Output | How |
|---|---|
| whatsmeow's log lines | Its logger is wrapped, at every level and in every sub-logger. |
| libsignal's log lines | libsignal's process-wide logger is set to the same wrapped logger, as sub-logger `Signal`, so it also follows `--log-level`. Only its errors, warnings and info lines are forwarded; its debug lines contain message keys and ciphertext and are dropped at every level. |
| The sender's own log lines | Every line it prints. |
| The job summary | The run error, each reminder's failure reason and the acknowledgement error. |
| The final `wa: …` line | The error that ends the run. |

The target group is reported only as verified or refused, never by name.

Reminder texts are printed unchanged, in the failure summary and in `--dry-run` output, so they can be posted as they are. Reminder keys, claim IDs and message IDs identify no person and are not treated as identifiers.

The job summary is rendered as Markdown on the public run page, and the keys, times, message IDs and error texts in it come from the Worker or from libraries. Each is shown on one line, with whitespace collapsed, control characters escaped as `\uXXXX` and long values shortened with `…`: the run error and the acknowledgement error after 1,000 characters, followed by "(full error in the run log)", every other value after 200. It is written as an inline code span whose backtick delimiter is longer than any run of backticks in the value, padded with a space when the value starts or ends with a backtick ([CommonMark code spans](https://spec.commonmark.org/0.31.2/#code-spans)), so it cannot add headings, links, formatting or reordered text. GitHub's Markdown renderer recognises code span delimiters of at most 80 backticks, so in a value with a run of 16 or more backticks every backtick is written in the same `\uXXXX` form (U+0060) and the delimiter stays a single backtick.

The summary has a section for each kind of unsent item:

- **To post by hand**: items that passed validation but were not delivered, each with its key, times, reason and text. The texts are in fenced blocks whose fence is at least three backticks and longer than any run of backticks in the text. GitHub closes a fenced block on any line of 255 or more of its fence character; texts with a run of 64 or more backticks, or tildes as a precaution, are not shown, and their entry says why the text is missing.
- **Refused by validation**: items refused by validation, and any item whose validation result is unknown, with their key and reason only. They appear in no other section, and their texts are never shown: a refused text did not come from the formatter, so it must not be posted, by the bot or by hand.
- **Delivered but not acknowledged**: items posted in this run whose acknowledgement failed.

Each section shows at most 5 items and then the number not listed, which keeps the summary at most about 120 KB whatever a claim contains.

Not covered: phone numbers written with spaces or dashes, which neither whatsmeow nor libsignal writes, and the message of a Go runtime panic, which the runtime prints itself.

`wa groups` and `wa pair` print IDs and phone numbers on purpose; they are run locally, never in a workflow.

## Pairing

The paired number must be an admin of the community so that it can post in Announcements, and its phone must open WhatsApp at least every 14 days: linked devices are logged out after 14 days without the phone being used ([WhatsApp FAQ](https://faq.whatsapp.com/378279804439436/)).

1. Create the key once with `go -C sender run ./cmd/wa keygen`. Store the `AGE-SECRET-KEY-1…` line as the secret `AGE_IDENTITY` of the deployment repository (`gh secret set AGE_IDENTITY --repo adityakotkar47/cp-reminders`) and in a password manager; never commit it.
2. Put it in the shell without writing it to history:
   - bash: `read -rs AGE_IDENTITY && export AGE_IDENTITY`
   - PowerShell 7: `$env:AGE_IDENTITY = Read-Host -MaskInput AGE_IDENTITY`
3. Check out branch `state` of the deployment repository in a separate worktree so the session never lands on `main`. With that repository as remote `origin` and `mitaoe/cp-reminders` as `upstream`: `git fetch origin state:state` and `git worktree add ../cp-reminders-state state`, or `git worktree add --orphan -b state ../cp-reminders-state` when the branch does not exist.
4. Pair, by QR code or by pairing code:
   - `go -C sender run ./cmd/wa pair --state ../../cp-reminders-state`, then on the phone: Linked devices → Link a device, and scan. Codes rotate; the window closes after about 160 seconds.
   - `go -C sender run ./cmd/wa pair --state ../../cp-reminders-state --phone 91XXXXXXXXXX` (international format without `+`), then on the phone: Linked devices → Link a device → Link with phone number instead, and enter the printed code.

   Add `--force` when the worktree already holds a session, and remove the previous device from the phone's linked devices first. Wait for `session saved to …`.
5. Find the Announcements JID with `go -C sender run ./cmd/wa groups --state ../../cp-reminders-state` ([below](#announcements-jid)) and store it as the secret `ANNOUNCE_JID` of the deployment repository.
6. Commit `session.db.age` on branch `state`, push it to the deployment repository (`git push origin state`), and remove the worktree with `git worktree remove ../cp-reminders-state`. Only the workflow may use the session from then on: two clients with the same keys end in `StreamReplaced`.

## Announcements JID

A community has a parent group (`PARENT=yes`) that nobody posts into, and an Announcements group: a separate `@g.us` group with `DEFAULT_SUB=yes`, `LINKED_PARENT` set to the community and, normally, `ANNOUNCE=yes` (admins only). `wa groups` marks it:

```
ANNOUNCEMENTS CANDIDATE: 120363xxxxxxxxxxxx@g.us name="CP Community" community=120363yyyyyyyyyyyy@g.us admin=yes
```

Use the candidate whose `community` is the right community and whose line shows `admin=yes`. `wa drain` checks both properties again before every send.

## Troubleshooting

| Message | Cause | Fix |
|---|---|---|
| `no session in state directory; run wa pair first` | Branch `state` is empty or not checked out at `--state`. | Check the path, or pair. |
| `AGE_IDENTITY: invalid age X25519 identity`, `unseal …: no identity matched` | Wrong or truncated secret, or a session encrypted with another key. | Fix the secret, or pair again with the current key. |
| `whatsapp: logged out (reason: …)` | The device was removed on the phone, the phone went unused for 14 days, or WhatsApp revoked the session. | Pair again with `--force`. Claimed rows are acknowledged as `failed`; later runs retry them while attempts remain and their deadline has not passed. |
| `session is not paired` | The store holds no device; whatsmeow deletes it on `device_removed`. | Pair again. |
| `whatsapp: stream replaced …` | Another process used the same session, such as a local `wa groups` during a run. | Make sure only the workflow uses the session. |
| `whatsapp: temporarily banned: …` | Account restriction. | Stop sending, use the phone normally, and wait for the stated expiry. |
| `whatsapp: client outdated` | WhatsApp rejects this whatsmeow version. | `go -C sender get go.mau.fi/whatsmeow@latest && go -C sender mod tidy`, then test and push. |
| `whatsapp: timed out waiting for connection` | Network problem or a slow initial sync. | Run again; raise `--connect-timeout` if it recurs. |
| `target is not a community announcements group` | `ANNOUNCE_JID` is a normal group or the community parent. | Use the `ANNOUNCEMENTS CANDIDATE` JID. |
| `sender is not an admin of the target group` | The number is not a community admin. | Make it an admin from the owner's number. |
| `passkey pairing is not supported` | The phone asked for passkey linking. | Use the other pairing method. |
| `invalid item "…": …` naming a layout line, the platform, the timing, the duration, the contest link, or a contest name or tip that contains a link, domain, IP address, phone number or invisible character | The Worker queued a text that is not a reminder in the agreed format ([reminder-format.md](reminder-format.md)). It is acknowledged as `rejected` and the Worker marks it `failed`, so it fails this one run and is not claimed again. | Find out how the text was produced: a formatter change that breaks the contract test, a stored tip, or a Worker that is not ours. Do not post refused texts. |
| `… held back: daily limit of 8 messages in 24 hours reached` | 8 messages were already sent in the last 24 hours. | The held-back reminders are listed to post by hand: post them if they are genuine, and check why so many were queued. Otherwise later runs send them while attempts remain. |
| Summary says "delivered but not acknowledged" | The message was posted but `/outbox/ack` failed. | Nothing to post; the next run acknowledges it from `sent.json`. |
| `outbox claim: HTTP 401` | `SENDER_TOKEN` differs between the Worker and GitHub. | Set the same value in both. |

## Code

| Package | Responsibility |
|---|---|
| `cmd/wa` | Flags, environment, wiring, the groups table and the pairing prompts. |
| `internal/drain` | Claim, validate, apply the daily limit, deliver and acknowledge, and the failure summary. Depends only on the `Outbox`, `Session` and `Ledger` interfaces, so it is tested with fakes. |
| `internal/reminder` | The reminder format: `Parse` checks a text against it and returns its fields; `Reminder.String` writes one. |
| `internal/outbox` | HTTP client for `/outbox/claim` and `/outbox/ack` with per-request timeouts and acknowledgement retries, and the three acknowledgement statuses. |
| `internal/redact` | Redacts WhatsApp IDs and long numbers from text, and wraps whatsmeow's and libsignal's loggers to do the same. |
| `internal/state` | The age vault, the encrypted session store and the `sent.json` ledger. |
| `internal/wa` | whatsmeow wrapper: fatal-event watcher, connect and wait, plain text sending, QR and phone pairing, admin check. |

```sh
go -C sender vet ./...
go -C sender test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C sender build ./cmd/wa
```

## Library behaviour the code depends on

- **Pure-Go SQLite store.** `sqlstore.NewWithDB(db, "sqlite3", log)` takes the SQL dialect separately from the `database/sql` driver, and `Container.Upgrade` requires `PRAGMA foreign_keys` ([sqlstore](https://pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore#New)). [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) registers driver `sqlite` and applies `_pragma=` DSN values to every connection, so the DSN is `session.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(DELETE)` and the binary builds with `CGO_ENABLED=0`. `DELETE` journaling leaves no `-wal` or `-shm` files.
- **Snapshots.** `VACUUM INTO` writes a new database file from the content of a read transaction on the open database, so the copy is consistent and contains only committed data ([VACUUM INTO](https://www.sqlite.org/lang_vacuum.html#vacuuminto)). With rollback journaling, other readers continue while it runs and a writer waits for it to finish, within `busy_timeout` ([locking](https://www.sqlite.org/lockingv3.html)).
- **Group fields.** `types.GroupInfo` exposes `Name`, `IsParent`, `LinkedParentJID`, `IsDefaultSubGroup` and `IsAnnounce`; participants carry `JID`, `PhoneNumber`, `LID`, `IsAdmin` and `IsSuperAdmin` ([types](https://pkg.go.dev/go.mau.fi/whatsmeow/types#GroupInfo)). In LID-addressed groups a participant's `JID` is a LID, so the admin check compares against both the device's phone JID and its LID.
- **Pairing.** `GetQRChannel` is called before `Connect` and emits code, success, timeout, error and passkey items ([GetQRChannel](https://pkg.go.dev/go.mau.fi/whatsmeow#Client.GetQRChannel)). `PairPhone` is called after the first QR event; its display name must have the form `Browser (OS)` and is `Chrome (Linux)` ([PairPhone](https://pkg.go.dev/go.mau.fi/whatsmeow#Client.PairPhone)). A successful pairing is followed by a reconnection, so `pair` waits for `Connected`. Passkey pairing needs a WebAuthn response, which a command line cannot give.
- **Fatal events.** `LoggedOut`, `StreamReplaced`, `TemporaryBan`, `ConnectFailure`, `ClientOutdated`, `StreamError` and `PairError` each end the run with a distinct error ([events](https://pkg.go.dev/go.mau.fi/whatsmeow/types/events)).
- **Shutdown.** `Disconnect()` stops the websocket and waits up to 5 seconds for the handler queue loop to end, but not for the work it started: the loop runs each incoming node's handler in its own goroutine and leaves it running when the connection closes, and several notification handlers, such as app state sync after a `server_sync` notification, start goroutines of their own. These keep using the store after `Disconnect()` returns ([client.go](https://github.com/tulir/whatsmeow/blob/main/client.go), [notification.go](https://github.com/tulir/whatsmeow/blob/main/notification.go)). The [shutdown](#shutdown) order depends on this.
- **App state sync after disconnecting.** When `FetchAppState` fails with `ErrIQDisconnected` or `ErrNotConnected`, the `server_sync` notification handler logs at debug level and stops syncing the remaining collections; other failures are logged as errors ([notification.go](https://github.com/tulir/whatsmeow/blob/main/notification.go), `handleAppStateNotification`).
- **Sending.** `SendMessage` returns a `SendResponse` whose `ID` is the message ID stored in `sent.json` and sent in the acknowledgement ([SendMessage](https://pkg.go.dev/go.mau.fi/whatsmeow#Client.SendMessage)).
- **Encryption.** [filippo.io/age](https://pkg.go.dev/filippo.io/age): the `Encrypt` writer must be closed to flush the last chunk. Encrypted files are written to a temporary file in the same directory and renamed, so a crash never leaves a truncated `session.db.age`.
