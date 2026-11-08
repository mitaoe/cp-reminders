# Architecture

The bot posts a reminder for each selected programming contest into the Announcements group of a WhatsApp Community, 45 minutes before the contest starts. It runs entirely on free hosted services.

## Requirements

| | |
|---|---|
| What | One reminder per selected contest (Codeforces, CodeChef, LeetCode, AtCoder), in the community's message format (see [formatter.md](formatter.md)). |
| Where | Exactly one chat: the community's Announcements group. Never any other group or person. |
| Delivery | Effectively once: no duplicates and no silent misses. A miss raises an alert that carries the text, ready to paste by hand. |
| Cost | $0, with no credit card. |
| Volume | 1–4 messages a day. |

## Constraints

| | |
|---|---|
| WhatsApp | No official API can post into a Community's Announcements group ([research](research/official-whatsapp-apis.md)). The sender is therefore an unofficial linked device (whatsmeow) on a spare number. Its session keys rotate on every connection and must be saved after every run, and only one connection may be live at a time. |
| Cloudflare Workers Free | 10 ms CPU per invocation, 5 cron triggers per account, 100,000 requests a day [1]. |
| Cloudflare D1 Free | 5 million rows read and 100,000 rows written a day, 500 MB per database [2]. |
| GitHub Actions | A concurrency group holds at most one pending run; a newly queued run cancels and replaces the pending one [3]. |

## Product decisions and trade-offs

| Decision | Reason | Accepted trade-off |
|---|---|---|
| Post through an unofficial linked device (whatsmeow). | No official API reaches a Community's Announcements group ([research](research/official-whatsapp-apis.md)). | Unofficial clients break WhatsApp's Terms, and the account can be restricted or banned; WhatsApp publishes no thresholds ([research](research/report.md#ban-risk)). |
| The device is a dedicated spare number that is a community admin; the owner's main number stays the community owner. | A ban then costs a SIM, not the owner's personal account or the community, and a linked device can read every chat on its account, so no personal chats are exposed. | The spare phone must open WhatsApp at least every 14 days, or the device is logged out and must be paired again. |
| Reminders go to the Announcements group only, enforced by the sender, which refuses any chat that is not a community announcements group where it is admin. | Members of the community's other groups should not receive reminders. | None; reminders appear wherever the community's Announcements group is shown. |
| Contests posted: every rated Codeforces round (Div. 1 to Div. 4, Educational, Global, Hello, Good Bye and sponsored rounds), CodeChef Starters, LeetCode Weekly and Biweekly, AtCoder Beginner Contests. Every such contest is reminded, not only the ones someone would remember to post. | The contests the community follows, checked against the community's 233 manual reminders from August 2025 to November 2026 ([formatter.md](formatter.md#tests)). | Unrated Codeforces rounds (testing rounds, April Fools, ICPC mirrors), Kotlin Heroes, CodeChef Monday Munch and Placement Prep, AtCoder ARC, AGC and heuristic contests, and anything longer than 5 hours get no reminder. Members see more reminders than before, for example every Sunday 8 AM LeetCode Weekly. Changing the selection is an edit to `worker/config/rules.json`. |
| A Codeforces Div. 1 and Div. 2 pair starting together gets one reminder, named `(Div. 1 + Div. 2)`, with the Div. 2 link. | One post per contest slot, as the community posted them. | Div. 1 participants get the Div. 2 link. |
| A reminder is due exactly 45 minutes before the start, with no rounding: an 8:00 PM contest is announced at 7:15 PM and an 8:05 PM contest at 7:20 PM. | Enough notice to join, close enough to the start to be remembered. | Posts land up to about 2 minutes after the due time: the cron fires every 5 minutes, the GitHub job takes seconds to start, and the sender waits a random 0–90 seconds so its sends are not machine-regular. |
| A reminder that cannot be sent at least 10 minutes before the start is dropped and reported. | A reminder in the last minutes before a contest is noise. | An outage longer than the 35-minute window misses reminders. The badge turns red; a GitHub email arrives only when a send run itself fails, not when the Worker stops ticking or cannot start the sender. |
| The text copies the community's manual format exactly and is sent as plain text, never with a link preview. | Familiar posts; previews clutter the group. | None. |
| Codeforces reminders always link to `https://codeforces.com/contests/<id>`. | That page opens before the round starts; `/contest/<id>` opens only once it has begun. | None. |
| Tips come from pools of owner-approved lines per contest type (Div. 3, Div. 4, Educational, AtCoder Beginner Contest, morning LeetCode), one line chosen per contest from its link, plus one-off overrides; nothing is generated at send time. | The owner's voice with variety, so the same contest type does not repeat one line every time. | Contests outside those types carry no tip unless the owner adds an override. |
| Each contest gets one reminder, ever. A contest rescheduled after its reminder was queued gets no second reminder and no correction. | Two reminders for one contest irritate members. Correcting the first is not practical: WhatsApp allows edits only within 20 minutes of sending ([whatsmeow `EditWindow`](https://pkg.go.dev/go.mau.fi/whatsmeow#pkg-constants)), which ends 25 minutes before the start, earlier than most delays are announced. | A reminder can show an outdated start time when a contest is delayed after it was posted. In the community's history, 8 of 230 reminders (3.5%) were for contests that started 10 to 15 minutes after the posted time. |
| Contest names are taken as clist reports them when the reminder is queued. | Rounds are usually named well before they start, and the snapshot refreshes hourly. | A round still under a placeholder name ("Codeforces Round (Div. 2)") is announced under it. |
| clist.by is the contest source, with the Codeforces API as the only fallback. | One authenticated call covers all four platforms; Codeforces has an official keyless API. | While clist is down, CodeChef, LeetCode and AtCoder reminders rely on the last snapshot, so contests added during the outage are missed; the badge turns yellow. |
| Everything runs on free tiers: Cloudflare Workers and D1 schedule, GitHub Actions sends. | $0 with no card. | GitHub's terms exclude runners for work "unrelated to" the software project, which a notifier arguably is; runners connect from datacenter addresses, which reports associate with more WhatsApp restrictions ([research](research/free-hosting.md)). |
| A failed send is retried by later runs until the deadline, then reported; there is no paid fallback. | Most failures (a slow connection, a dropped run) clear on the next run. | When the session is logged out, every reminder misses until the number is paired again; the failure email carries each text to post by hand. |
| The bot covers contests on Codeforces, CodeChef, LeetCode and AtCoder only. Other events (college contests, hackathons, other platforms) are posted by hand. | Those events are one-offs with registration details that differ each time; the community posted a handful in 15 months. | Someone has to post them; they do not appear in `/preview`. |
| The code is public in `mitaoe/cp-reminders`, where anyone can propose changes. The bot runs from a fork, the deployment repository, which alone holds its secrets and branch `state`; syncing the fork releases merged changes. | Contributions go through the organisation's review, and no secret or session is reachable from the repository people contribute to: anyone with write access to a repository can run its workflows from their own branch, and a workflow can read every secret of its repository. The sender redacts WhatsApp IDs, in the forms whatsmeow and libsignal write them, from its logs and from the errors in its job summary ([sender.md](sender.md#public-output)), and GitHub masks secrets in logs. | Whatever is synced into the fork runs with the bot's secrets, so changes to workflows and to the sender are reviewed before syncing. `session.db.age` is publicly downloadable from the fork, and stays unreadable without `AGE_IDENTITY`. |

## Components

```mermaid
flowchart LR
  subgraph CF[Cloudflare]
    W[Worker: scheduler and outbox API]
    D[(D1)]
    W <--> D
  end
  CRON[Workers cron, every 5 min] --> W
  W -->|contests| CL[clist.by API]
  W -.->|fallback| CFAPI[Codeforces API]
  W -->|workflow_dispatch| GH
  subgraph GH[GitHub Actions: send.yml]
    S[sender: Go, whatsmeow]
    ST[(branch state)]
    S <--> ST
  end
  S -->|claim, ack| W
  S -->|text| WA[WhatsApp Announcements group]
```

| Component | Responsibility | Never |
|---|---|---|
| Core (`worker/src/core/`) | Pure functions: select contests, decide which reminders are due, render the text. | Performs I/O. |
| Worker (`worker/`) | Fetches and caches contests, queues due reminders, wakes the sender, serves the outbox, reports health and a status badge. | Talks to WhatsApp or knows the target chat. |
| Sender (`sender/`) | Claims queued reminders, posts them to the one allowed chat, reports results, saves the WhatsApp session. | Decides what to send or when, or accepts a destination over the network. |
| Workflows (`.github/workflows/`) | In the deployment repository: `send.yml` runs the sender with its secrets, one run at a time, and saves its state; `deploy.yml` deploys the Worker. In every repository: `ci.yml` runs the checks. | Carry message text: the dispatch is only a wake-up. |

## Design decisions

1. **The sender pulls from an outbox.** The Worker writes due reminders to a durable outbox in D1, and a dispatch only means "wake up and drain it". Every run drains everything pending, so a cancelled or dropped run loses nothing, and message text never appears in workflow inputs or logs.
2. **State lives in D1, not KV.** Deduplication and claiming need atomic, read-your-writes operations: `INSERT … ON CONFLICT DO NOTHING`, `UPDATE … RETURNING`, and `batch()`, which runs as one transaction [4]. KV changes can take 60 seconds or more to reach other locations [5], and its free tier allows 1,000 writes a day. D1's documentation promises general SQLite compatibility rather than these statements by name [6], so the Worker tests run them against the local D1 engine.
3. **Contests have a natural identity.** A contest is `(resource_id, href)`, the same whichever source supplied it, and a reminder's key is `resource_id|href`, so a contest is reminded at most once, even when its start time changes.
4. **Text is rendered once, when the reminder is queued.** The stored text is exactly what is sent, which makes the outbox an audit log.
5. **Only the sender knows the destination.** `ANNOUNCE_JID` is a secret of the deployment repository. The sender refuses any chat that is not a community announcements group in which it is admin. A compromised Worker can at most post text into Announcements, five messages per run.
6. **Two independent guards against duplicates.** D1's primary key on the reminder key, and the sender's own `sent.json`, which covers a message that was sent but whose acknowledgement was lost.
7. **The session lives on a single-commit branch.** Branch `state` of the deployment repository holds `session.db.age` and `sent.json`. Each run force-pushes one commit with a lease check, so the repository does not grow, and the workflow's concurrency group serialises the writes. The push happens in a separate job that takes only the two files from the sender's job, so the only token that writes to the repository never runs alongside the sender's code ([workflow.md](workflow.md#sendyml-drain-the-outbox)).

## Data

All timestamps are stored in UTC as ISO 8601 with `Z`. IST appears only in the rendered text.

### D1 tables

Schema: [`worker/migrations/0001_init.sql`](../worker/migrations/0001_init.sql).

| Table | Role | Shape |
|---|---|---|
| `contest_snapshot` | Cache of upcoming contests | One row, replaced as a whole on each refresh. `payload` is the normalised `Contest[]` JSON. |
| `reminders` | Outbox and audit log | One row per reminder, created when it becomes due and kept indefinitely (about 1,000 rows a year). The source of truth for "already handled". |
| `tip_overrides` | One-off tips | Set at runtime without a redeploy; they take precedence over the overrides in `worker/config/tips.json`. |
| `meta` | Operational facts | The outcome of each tick step: `last_tick_at`; `last_fetch_at`, `last_fetch_source`, `last_fetch_error`; `last_wake_at`, `last_wake_error`. An error key is set when its step fails and removed when the step next succeeds. It holds only a code (see [Error codes](#error-codes)), never upstream text. |

A reminder's `deadline_utc` is its start minus `minLeadMinutes` (10). After the deadline it is not worth sending.

### Reminder lifecycle

```mermaid
stateDiagram-v2
  [*] --> queued: tick, now ≥ start − 45 min
  queued --> claimed: sender claims (10-minute lease)
  claimed --> sent: ack "sent"
  claimed --> queued: ack "failed", attempts < 3
  claimed --> failed: ack "failed", attempts = 3
  claimed --> claimed: lease expires, attempts < 3, and another run claims it
  claimed --> failed: lease expires, attempts = 3
  queued --> expired: deadline passes
  claimed --> expired: lease expires after the deadline
  expired --> sent: ack "sent" from the claim that was in flight
  sent --> [*]
  failed --> [*]
  expired --> [*]
```

Only the Worker changes a row's status, in two places:

- **Acknowledgement** (`POST /outbox/ack`): the sender reports `sent` or `failed` for each item it claimed; results from any other claim are ignored.
- **Sweep**: runs in every tick and at the start of every claim, in the same transaction as the claim. It expires `queued` rows past their deadline, expires `claimed` rows whose lease has run out after the deadline, and fails `claimed` rows whose lease has run out after the last attempt.

`attempts` counts claims: it rises when a run claims the row, so a run that dies without acknowledging also uses one. Its row stays `claimed` with the old lease, and the next claim takes it over once the lease has run out. A `claimed` row past its deadline is left alone until its lease runs out, so a run that is still sending can report `sent`.

`failed` and `expired` are terminal and count as misses: they appear in `/health` and turn the badge red, and the failed workflow run emails the owner with the text in its job summary.

| Column | Set when |
|---|---|
| `created_at` | The tick queues the reminder. |
| `start_utc`, `deadline_utc` | At queueing: the contest start, and the start minus `minLeadMinutes`. |
| `claimed_at`, `claim_id` | A run claims it; cleared when a failed attempt returns it to `queued`. |
| `attempts` | Each claim. |
| `sent_at`, `message_id` | The `sent` acknowledgement; `message_id` is WhatsApp's message ID. |
| `last_error` | A `failed` acknowledgement, or a lease that runs out after the last attempt. |
| `updated_at` | Every change; for a `failed` or `expired` row, the time it became so. |

A row holds the latest state, not a history of every transition: a reminder that failed once and was then sent shows `sent`, two attempts and the last error. Each run's own timeline is in its GitHub Actions log, and `sent.json` records when the sender delivered each message.

### Configuration

`worker/config/rules.json` (which contests, lead times) and `worker/config/tips.json` (tip rules) are bundled into the Worker at deploy. Both are described in [formatter.md](formatter.md).

### Sender state (branch `state`)

| File | Content |
|---|---|
| `session.db.age` | The WhatsApp login: the whatsmeow SQLite store, encrypted with age. The decryption key is the secret `AGE_IDENTITY`, never stored in a repository, so the file is unreadable without it. Each run decrypts it, connects, and commits the re-encrypted file, because the keys inside change on every connection. |
| `sent.json` | The last 500 reminders this sender delivered: `{key, sentAt, messageId}`. Not secret. |

### Secrets

| Stored in | Name | Use |
|---|---|---|
| Worker | `CLIST_USERNAME`, `CLIST_API_KEY` | clist API |
| Worker | `GH_TOKEN` | Fine-grained token limited to the deployment repository with Actions read and write; dispatches `send.yml`. |
| Worker | `TICK_TOKEN` | `POST /tick` |
| Worker | `ADMIN_TOKEN` | `GET /preview`, `PUT /tips`, `DELETE /tips` |
| Worker and deployment repository | `SENDER_TOKEN` | `/outbox/claim`, `/outbox/ack` |
| Deployment repository | `WORKER_URL` | Base URL of the Worker |
| Deployment repository | `AGE_IDENTITY` | Decrypts and encrypts the session |
| Deployment repository | `ANNOUNCE_JID` | The only chat the sender may post to |
| Deployment repository | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | `deploy.yml`: an API token from Cloudflare's "Edit Cloudflare Workers" template with D1 Edit added, and the account ID |

Every token has one purpose and is compared in constant time. The deployment repository also has the variable `BOT_DEPLOYMENT=true`, which enables `send.yml` and `deploy.yml`; `mitaoe/cp-reminders` has no secrets and no such variable. The Worker's `vars.GH_REPO` in `wrangler.jsonc` names the deployment repository.

## Interfaces

### Worker HTTP API

Bodies are JSON. Authentication is `Authorization: Bearer <token>`.

| Route | Auth | Behaviour |
|---|---|---|
| `GET /health` | none | `{lastTickAt, lastFetchAt, lastFetchSource, lastFetchError, lastWakeError, queued, claimed, failed24h, expired24h}`. The two errors are codes ([Error codes](#error-codes)). Contains no secrets, no message text and no upstream response text. |
| `GET /badge` | none | The health as a [shields.io endpoint badge](https://shields.io/badges/endpoint-badge) (see [Status badge](#status-badge)). |
| `POST /tick` | `TICK_TOKEN` | Runs one tick and returns `{fetched, queued, woke, errors}`. |
| `GET /preview` | `ADMIN_TOKEN` | The next 10 reminders with their send time in IST and rendered text. |
| `PUT /tips` | `ADMIN_TOKEN` | `{href, tip}` sets a one-off tip; an empty `tip` suppresses the tip for that contest. |
| `DELETE /tips` | `ADMIN_TOKEN` | `{href}` removes a one-off tip. |
| `POST /outbox/claim` | `SENDER_TOKEN` | `{claimId}` → `{items: [{key, text, startUtc, deadlineUtc}]}`, at most 5. Atomically moves `queued` rows, and `claimed` rows whose lease has expired, to `claimed` under `claimId`. Rows past their deadline are not returned. |
| `POST /outbox/ack` | `SENDER_TOKEN` | `{claimId, results: [{key, status: "sent" \| "failed", messageId?, error?}]}` applies the lifecycle transitions. Results for rows held by another claim are ignored. |

### Status badge

`GET /badge` returns `{schemaVersion: 1, label: "bot", message, color}` with `Cache-Control: max-age=300`. The states are ordered by what to fix first, and the first matching state wins:

| State | Condition | `message` | `color` |
|---|---|---|---|
| down | no tick in the last 15 minutes, or no tick ever | `no tick for 23m` / `never ticked` | `red` |
| sender unreachable | `lastWakeError` is set | `dispatch failing` | `red` |
| failing | `failed24h + expired24h > 0` | `2 missed in 24h` | `red` |
| degraded | `lastFetchError` is set | `degraded: clist` | `yellow` |
| healthy | otherwise | `ok · tick 3m ago` | `brightgreen` |

A tick that cannot start the sender turns the badge red at once, before any reminder is missed. It stays red until a dispatch succeeds, so a fault that outlasts its reminders stays visible. A single transient GitHub error clears on the next tick, which retries the dispatch. The badge carries no error; `/health` has its code.

### Error codes

`last_fetch_error`, `last_wake_error` and the matching `/health` fields hold a code from a fixed set, so upstream response text, such as GitHub's reason for refusing a token, never becomes public:

| Code | Cause |
|---|---|
| `HTTP <status>` | The upstream service answered with a status outside 2xx. |
| `timeout` | The request did not finish within 8 seconds. |
| `network error` | The request failed before an answer, or with any other error. |
| `invalid response` | The body was not valid JSON, or Codeforces reported a failure. |
| `no contests` | clist or Codeforces returned no upcoming contests. |

`last_fetch_error` names each source that failed, such as `clist: HTTP 502` or `clist: timeout; codeforces: HTTP 503`; `last_wake_error` is the code alone. The full error, including up to 200 characters of the response body for a failed dispatch, is written to the Worker's log, which Workers Logs keeps (`observability` in `wrangler.jsonc`) and `wrangler tail` streams, and is returned by `POST /tick`, which requires `TICK_TOKEN`.

The README shows it through `https://img.shields.io/endpoint?url=<WORKER_URL>/badge&logo=cloudflare&logoColor=white` [9].

### Workflow dispatch

`POST /repos/{owner}/{repo}/actions/workflows/send.yml/dispatches` with `{"ref": "main"}` and no inputs. GitHub answers `200` with the run ID [7]; the Worker treats any 2xx as success. `send.yml` has no `schedule` trigger, so GitHub's 60-day inactivity rule, which applies to scheduled workflows, does not disable it [8].

### Sender CLI

`wa keygen`, `wa pair`, `wa groups` and `wa drain`, described in [sender.md](sender.md).

## Flows

### Tick (Workers cron, every 5 minutes)

1. If the contest snapshot is older than 60 minutes, fetch clist: every upcoming contest of the four platforms, up to 100, which covers everything announced, usually several weeks ahead. If clist fails or returns nothing, fetch the Codeforces API instead, which needs no key: its contests replace the Codeforces ones in the snapshot, and the other platforms' upcoming contests are kept. `last_fetch_error` records the clist failure even when the fallback succeeds. If both fail, the existing snapshot stays.
2. For every contest starting between 45 and 10 minutes from now whose key is not in the outbox yet, render the text and insert it as `queued` with `INSERT … ON CONFLICT(key) DO NOTHING`.
3. Mark `queued` and `claimed` rows past their deadline as `expired`.
4. If rows are waiting (`queued`, or `claimed` with an expired lease) and the last wake was more than 4 minutes ago, dispatch `send.yml`. A 2xx response records `last_wake_at` and removes `last_wake_error`; any other outcome records `last_wake_error`.

The two intervals do different jobs. The snapshot is refreshed hourly because contest schedules change rarely and clist asks for caching. The tick runs every 5 minutes because it decides when a reminder goes out: a reminder is queued at most 5 minutes after it becomes due, and checking the snapshot costs one query.

`POST /tick` runs the same steps, so an external scheduler such as cron-job.org can act as a second trigger. Overlapping ticks are harmless: inserts are idempotent, and an extra wake only produces a run that finds an empty outbox.

### Send run

```mermaid
sequenceDiagram
  participant GH as send.yml
  participant S as sender
  participant W as Worker
  participant WA as WhatsApp
  GH->>S: start (concurrency group wa-session)
  S->>W: POST /outbox/claim {claimId}
  W-->>S: items (possibly none)
  alt no items
    S-->>GH: exit 0 without connecting to WhatsApp
  else items
    S->>S: decrypt session, wait a random 0–90 s
    S->>WA: connect, check the target is Announcements and we are admin
    loop each item not already in sent.json
      S->>WA: send the text to ANNOUNCE_JID
      S->>S: record it in sent.json
    end
    S->>S: disconnect, encrypt and save the session
    S->>W: POST /outbox/ack {claimId, results}
  end
  GH->>GH: force-push branch state (also after a failure)
```

## Failure handling

| Failure | Effect | Handling |
|---|---|---|
| clist down or rate-limited | No fresh contest data | Codeforces fallback; contests already in the snapshot are still reminded. Shown by `/health` and a yellow badge. |
| Cron tick late or skipped | Reminder delayed | A reminder stays sendable from 45 to 10 minutes before the start, so ticks can be up to 35 minutes late. The badge turns red after 15 minutes without a tick. |
| Dispatch fails (GitHub error, revoked or wrong `GH_TOKEN`) | Sender not woken; no run starts, so GitHub sends no email | `last_wake_error` turns the badge red at once and `/health` shows its code, such as `HTTP 401`; the Worker's log and `POST /tick` show GitHub's full answer. `last_wake_at` stays unchanged, so every tick dispatches again until one succeeds. |
| Pending run cancelled by a newer one | None | The outbox keeps the rows; the next run drains them. |
| Two runs at once | Prevented | Concurrency group `wa-session`. One live connection is also what WhatsApp expects of a device. |
| Sent, but the ack is lost | Risk of a duplicate | The next run finds the key in `sent.json` and acks it as sent without resending. If the deadline passes first, the row ends `expired` although the message was delivered; `sent.json` is the record of delivery. |
| WhatsApp session logged out or removed | Nothing can be sent | The run fails, GitHub emails the owner, and the job summary lists the texts. Rows return to `queued` and expire at their deadline. Re-pair with `wa pair`. |
| Contest rescheduled | Start time changes | Before its reminder is queued: the reminder follows the new start. After: the key already exists, so nothing more is sent and the posted time stands. |
| Placeholder contest name | Generic name posted | The snapshot refreshes hourly, so names are usually final by the time a reminder is due. |
| Spare number banned | No reminders | The owner's main number owns the community; pair another number and make it admin. |

## Capacity

| Resource | Use per day | Free limit |
|---|---|---|
| Worker requests | 288 cron ticks, plus sender and badge requests | 100,000 |
| Worker CPU per tick | Parsing about 30 contests and rendering a few messages; fetches and SQL are I/O, not CPU | 10 ms |
| D1 rows written | about 24 snapshot refreshes, a few reminder transitions, 288 tick timestamps | 100,000 |
| D1 rows read | about 2,000 | 5,000,000 |
| clist requests | at most 24 | 10 a minute |
| GitHub Actions | 1–4 send runs of under a minute each, plus a deploy per release | Unlimited minutes on standard runners for public repositories |

## Repository layout

```
worker/              Cloudflare Worker (pnpm workspace package)
  config/            rules.json, tips.json
  migrations/        D1 schema
  src/core/          contest selection and message formatting
  src/               scheduler, outbox API, health, badge
  test/              Worker tests in the Workers runtime with a local D1
  test/core/         rule tests and golden tests against the manual reminder history
  test/fixtures/     frozen clist data and the community's manual reminders, August 2025 to November 2026
sender/              Go module: cmd/wa, internal/{drain,outbox,state,wa}
.github/workflows/   send.yml (drain the outbox), deploy.yml (deploy the Worker), ci.yml (checks)
docs/                architecture, formatter, scheduler, sender, workflow
docs/research/       research behind the design
```

## Alternative host

The outbox API does not depend on GitHub Actions. A sender with a polling loop, running permanently on an always-free VM such as Oracle Cloud's, could call `/outbox/claim` every minute instead of being woken by a dispatch; the Worker, data model and API would stay the same. The sender has no polling mode today.

## References

1. Cloudflare Workers limits: https://developers.cloudflare.com/workers/platform/limits/
2. D1 pricing and limits: https://developers.cloudflare.com/d1/platform/pricing/, https://developers.cloudflare.com/d1/platform/limits/
3. GitHub Actions concurrency: https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/control-the-concurrency-of-workflows-and-jobs
4. D1 `batch()`: https://developers.cloudflare.com/d1/worker-api/d1-database/
5. How KV works: https://developers.cloudflare.com/kv/concepts/how-kv-works/
6. D1 SQL statements: https://developers.cloudflare.com/d1/sql-api/sql-statements/
7. Create a workflow dispatch event: https://docs.github.com/en/rest/actions/workflows#create-a-workflow-dispatch-event
8. Disabling and enabling a workflow: https://docs.github.com/en/actions/managing-workflow-runs-and-deployments/managing-workflow-runs/disabling-and-enabling-a-workflow
9. shields.io endpoint badge: https://shields.io/badges/endpoint-badge
