# cp-reminders

[![CI](https://github.com/mitaoe/cp-reminders/actions/workflows/ci.yml/badge.svg)](https://github.com/mitaoe/cp-reminders/actions/workflows/ci.yml)
[![bot](https://img.shields.io/endpoint?url=https%3A%2F%2Fcp-reminders.c122.workers.dev%2Fbadge&logo=cloudflare&logoColor=959da5)](https://cp-reminders.c122.workers.dev/health)

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
- A **GitHub Actions** job runs the **Go sender**, a WhatsApp linked device on a dedicated number, which posts the queued reminders to the Announcements group and nowhere else. It posts only texts in the agreed reminder format, at most 8 in any 24 hours.
- Everything runs on free tiers. The badges above show the checks and the bot's live health: red after 15 minutes without a tick, when the sender cannot be started, or after a missed reminder; yellow when clist is failing and the Codeforces fallback is in use.

## Repository

| Path | Contents |
|---|---|
| `worker/` | Cloudflare Worker: scheduling, contest selection and formatting, outbox API, health and badge. |
| `worker/config/` | Which contests get reminders, lead times and tips. |
| `sender/` | Go WhatsApp sender. |
| `testdata/` | Test data shared by the Worker and the sender: the rendered reminders and free-text examples ([reminder-format.md](docs/reminder-format.md#shared-test-data)). |
| `.github/workflows/` | `send.yml` drains the outbox; `deploy.yml` deploys the Worker; `ci.yml` runs the checks. |
| `docs/` | Design and operation. |
| `docs/pfp/` | The sender's WhatsApp profile picture: `crewmates.svg` is the source, `crewmates.png` the 640×640 image set on the account. |

### Where it runs

- **[mitaoe/cp-reminders](https://github.com/mitaoe/cp-reminders)** holds the code. It has no secrets, and the bot never runs from it. Its `main` changes only through pull requests whose CI checks pass; force pushes and deletion are blocked. `.github/CODEOWNERS` requests a review from @adityakotkar47 on every pull request opened by another account, except while it is a draft. GitHub never requests a review from a pull request's own author, and no approval is required, so @adityakotkar47 merges his own pull requests once CI passes.
- **The deployment repository**, [adityakotkar47/cp-reminders](https://github.com/adityakotkar47/cp-reminders), is a fork of it and runs the bot. It alone has the bot's secrets, the repository variable `BOT_DEPLOYMENT=true` that enables `send.yml` and `deploy.yml`, and branch `state`.

A release is an update of the fork's `main`, and a change goes live with it. Every workflow on the fork can read the bot's secrets. `deploy.yml` runs when the release touches `worker/`, `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml` or `deploy.yml` itself ([workflow.md](docs/workflow.md#deployyml-deploy-the-worker)): its tests run without secrets, then pnpm, Wrangler and the locked dependencies run with the Cloudflare token, apply `worker/migrations` to the production D1 database and deploy a Worker that holds its own secrets. The next send run uses the new sender. A release therefore reviews the whole difference and pushes exactly the reviewed commit, with the fork as `origin` and `mitaoe/cp-reminders` as `upstream`:

1. `git fetch origin && git fetch upstream`, then record the commit to release with `git rev-parse upstream/main`.
2. Review `git diff origin/main <that commit>`, or the [compare view](https://github.com/adityakotkar47/cp-reminders/compare/main...mitaoe:cp-reminders:main) with collapsed files such as `pnpm-lock.yaml` expanded; when reviewing in the compare view, the commit to release is the last one it lists.
3. `git push origin <that commit>:main`, a fast-forward that the fork's ruleset allows. Pushing a change to `.github/workflows/` needs a credential with the `workflow` scope.

**Sync fork** on GitHub releases whatever is on `mitaoe/cp-reminders` `main` at the moment it is clicked, so with it the compare view is checked again immediately before clicking.

Branch `state` of the deployment repository holds the sender's WhatsApp login, encrypted with age (`session.db.age`; the key is the secret `AGE_IDENTITY`), and its delivery log (`sent.json`). Each run replaces it with a single new commit. A release leaves it untouched; it must not be merged or deleted, since deleting it logs the bot out.

## Documentation

- [Architecture](docs/architecture.md): design, data model, interfaces and failure handling.
- [Reminder format](docs/reminder-format.md): the exact text the bot may post, shared by the formatter and the sender.
- [Formatter](docs/formatter.md): how contests become reminders, contest selection and tips.
- [Scheduler](docs/scheduler.md): the Worker, its API, deployment and development.
- [Sender](docs/sender.md): commands, content checks, pairing and troubleshooting.
- [Workflows](docs/workflow.md): `send.yml`, `deploy.yml` and `ci.yml`.
- [Research](docs/research/report.md): the findings behind the design.

## Operating

| Task | How |
|---|---|
| See upcoming reminders | `GET /preview` ([scheduler.md](docs/scheduler.md#http-api)) |
| Add or suppress a tip for one contest | `PUT /tips`; the tip must be one line without links, domains, IP addresses or phone numbers ([scheduler.md](docs/scheduler.md#http-api)) |
| Release a change | Merge it in `mitaoe/cp-reminders`, then release it to the deployment repository ([Where it runs](#where-it-runs)). |
| Change which contests are posted, or the standard tips | Edit `worker/config/`, regenerate `testdata/reminders.json` with `pnpm --filter cp-reminders-worker run contract`, and release it ([formatter.md](docs/formatter.md)) |
| A reminder failed | GitHub emails the failed `send` run; its summary holds the text to post by hand. |
| The sender refused a text | Do not post it by hand; see [sender.md](docs/sender.md#troubleshooting). |
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
