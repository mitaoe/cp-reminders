# cp-reminders

[![CI](https://github.com/mitaoe/cp-reminders/actions/workflows/ci.yml/badge.svg)](https://github.com/mitaoe/cp-reminders/actions/workflows/ci.yml)
[![bot](https://img.shields.io/endpoint?url=https%3A%2F%2Fcp-reminders.c122.workers.dev%2Fbadge&logo=cloudflare&logoColor=white)](https://cp-reminders.c122.workers.dev/health)

Posts a reminder for every Codeforces, CodeChef, LeetCode and AtCoder contest into the Announcements group of the CP community's WhatsApp Community, 45 minutes before it starts.

```
🚨 Contest Reminder 🚨

📍 Platform: LeetCode
🏆 Contest Name: Weekly Contest 527
🕒 Timing: 8:00 AM
⏳ Duration: 1:30 hours
🔗 Contest Link: https://leetcode.com/contest/weekly-contest-527/

📌 Tip: morning contests build discipline
```

## How it works

- A **Cloudflare Worker** reads upcoming contests from [clist.by](https://clist.by) every hour, queues each reminder in D1 when it becomes due, and wakes the sender.
- A **GitHub Actions** job runs the **Go sender**, a WhatsApp linked device on a dedicated number, which posts the queued reminders to the Announcements group and nowhere else.
- Everything runs on free tiers. The badges above show the checks and the bot's live health: red after 15 minutes without a tick, when the sender cannot be started, or after a missed reminder; yellow when clist is failing and the Codeforces fallback is in use.

## Repository

| Path | Contents |
|---|---|
| `worker/` | Cloudflare Worker: scheduling, contest selection and formatting, outbox API, health and badge. |
| `worker/config/` | Which contests get reminders, lead times and tips. |
| `sender/` | Go WhatsApp sender. |
| `.github/workflows/` | `send.yml` drains the outbox; `deploy.yml` deploys the Worker; `ci.yml` runs the checks. |
| `docs/` | Design and operation. |
| `docs/pfp/` | The sender's WhatsApp profile picture: `crewmates.svg` is the source, `crewmates.png` the 640×640 image set on the account. |

### Where it runs

- **[mitaoe/cp-reminders](https://github.com/mitaoe/cp-reminders)** holds the code. Changes arrive as pull requests, and CI checks them. It has no secrets, and the bot never runs from it.
- **The deployment repository**, [adityakotkar47/cp-reminders](https://github.com/adityakotkar47/cp-reminders), is a fork of it and runs the bot. It alone has the bot's secrets, the repository variable `BOT_DEPLOYMENT=true` that enables `send.yml` and `deploy.yml`, and branch `state`.

A merged change goes live when the fork is synced (**Sync fork** on GitHub): `deploy.yml` deploys the Worker when `worker/` changed, and the next send run uses the new sender. Whatever is synced runs with the bot's secrets, so changes to `.github/workflows/` and `sender/` are reviewed before syncing.

Branch `state` of the deployment repository holds the sender's WhatsApp login, encrypted with age (`session.db.age`; the key is the secret `AGE_IDENTITY`), and its delivery log (`sent.json`). Each run replaces it with a single new commit. Syncing leaves it untouched; it must not be merged or deleted, since deleting it logs the bot out.

## Documentation

- [Architecture](docs/architecture.md): design, data model, interfaces and failure handling.
- [Formatter](docs/formatter.md): message format, contest selection and tips.
- [Scheduler](docs/scheduler.md): the Worker, its API, deployment and development.
- [Sender](docs/sender.md): commands, pairing and troubleshooting.
- [Workflows](docs/workflow.md): `send.yml` and `ci.yml`.
- [Research](docs/research/report.md): the findings behind the design.

## Operating

| Task | How |
|---|---|
| See upcoming reminders | `GET /preview` ([scheduler.md](docs/scheduler.md#http-api)) |
| Add or suppress a tip for one contest | `PUT /tips` ([scheduler.md](docs/scheduler.md#http-api)) |
| Release a change | Merge it in `mitaoe/cp-reminders`, then sync the deployment repository ([Where it runs](#where-it-runs)). |
| Change which contests are posted, or the standard tips | Edit `worker/config/` and release it ([formatter.md](docs/formatter.md)) |
| A reminder failed | GitHub emails the failed `send` run; its summary holds the text to post by hand. |
| The sender is logged out | Pair again ([sender.md](docs/sender.md#pairing)) |
| Keep the session alive | Open WhatsApp on the sender's phone at least every 14 days. |
| Post to a different community | Add the sender's number to the community and make it a community admin. Find the community's Announcements JID with `wa groups` ([sender.md](docs/sender.md#announcements-jid)), then set it on the deployment repository: `gh secret set ANNOUNCE_JID --repo adityakotkar47/cp-reminders`. The next send run uses it. |
| Rotate a Worker token or the GitHub token | Generate a new value and set it with `pnpm exec wrangler secret put <NAME>` in `worker/`. `SENDER_TOKEN` is also a secret of the deployment repository, so set the same value with `gh secret set SENDER_TOKEN --repo adityakotkar47/cp-reminders`. `GH_TOKEN` is a fine-grained token limited to the deployment repository with Actions read and write ([architecture.md](docs/architecture.md#secrets)). |
| Add a second trigger | Optional: an external scheduler calling `POST /tick` every 5 minutes ([scheduler.md](docs/scheduler.md#optional-second-trigger)). |

## Checks

```sh
pnpm install
pnpm --filter cp-reminders-worker typecheck
pnpm --filter cp-reminders-worker test
go -C sender vet ./...
go -C sender test ./...
```
