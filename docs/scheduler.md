# Scheduler (Cloudflare Worker)

The Worker in `worker/` fetches contests, queues reminders in D1, wakes the sender and serves the outbox the sender drains. The design is in [architecture.md](architecture.md); this page covers the code, operations and development.

## Modules

| File | Responsibility |
|---|---|
| `src/index.ts` | Route table and the `fetch` and `scheduled` entry points. |
| `src/router.ts` | Path and method matching; bearer authentication per route. |
| `src/auth.ts` | Bearer parsing and constant-time comparison of SHA-256 digests. |
| `src/core/` | Contest selection and message formatting ([formatter.md](formatter.md)). |
| `src/contests.ts` | Contest snapshot: clist, with the Codeforces API as fallback. |
| `src/tick.ts` | One tick: refresh the snapshot, queue due reminders, expire, wake. |
| `src/reminders.ts` | Claim, acknowledgement, lease and expiry SQL. |
| `src/wake.ts` | `workflow_dispatch` of `send.yml`, debounced. |
| `src/outbox.ts`, `src/admin.ts` | Request validation and handlers for the outbox and admin routes. |
| `src/health.ts`, `src/badge.ts` | Health values and the public `/health` and `/badge` routes. |
| `src/tips.ts`, `src/preview.ts` | One-off tips from D1; the preview text. |
| `src/config.ts` | `config/rules.json` and `config/tips.json`, bundled at deploy. |
| `migrations/0001_init.sql` | D1 schema. |

`POST /tick` and the `*/5 * * * *` cron trigger run the same `runTick` ([tick flow](architecture.md#tick-workers-cron-every-5-minutes), [reminder lifecycle](architecture.md#reminder-lifecycle)).

## HTTP API

The routes and their contracts are in [architecture.md](architecture.md#worker-http-api). In addition:

- Tokens are accepted only in the `Authorization` header, never in the query string.
- Authentication runs before the request body is read.
- A known path with the wrong method answers 405; an unknown path answers 404.
- `href` and `tip` are limited to 500 characters, `claimId` to 100, and an ack to 100 results.
- A one-off tip affects reminders queued after it is set; text already in the outbox does not change.

```sh
BASE=https://cp-reminders.c122.workers.dev

curl "$BASE/health"
curl "$BASE/badge"
curl -X POST -H "Authorization: Bearer $TICK_TOKEN" "$BASE/tick"
curl -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/preview"

curl -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"href":"https://www.codechef.com/START264","tip":"bring snacks"}' "$BASE/tips"
curl -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"href":"https://www.codechef.com/START264","tip":""}' "$BASE/tips"
curl -X DELETE -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"href":"https://www.codechef.com/START264"}' "$BASE/tips"
```

The second `PUT` suppresses the tip for that contest; the `DELETE` removes the one-off tip.

`/health` reports a failed fetch or dispatch only as a code ([error codes](architecture.md#error-codes)). The full error is in the `POST /tick` response and in the Worker's log: Workers Logs in the Cloudflare dashboard, or live with `pnpm exec wrangler tail` in `worker/`.

## D1 behaviour the code depends on

| Behaviour | Used for | Verified by |
|---|---|---|
| `batch()` runs its statements in order as one transaction and returns one result per statement ([D1 Worker API](https://developers.cloudflare.com/d1/worker-api/d1-database/)) | Snapshot save, sweep and claim, tick, ack | Overlapping-claim and overlapping-tick tests |
| `meta.changes` per statement | The `queued` and `applied` counts | Tick and ack tests |
| `UPDATE … RETURNING` and `INSERT … ON CONFLICT DO NOTHING` | Claim, queueing, upserts | `test/claim.test.ts` and `test/tick.test.ts` against the local D1 engine |

## Deployment

Releases are deployed by `deploy.yml` in the deployment repository when the fork is synced ([workflow.md](workflow.md#deployyml-deploy-the-worker)). The first deployment of a new Worker is done by hand, with commands run in `worker/` and the Cloudflare account logged in (`pnpm exec wrangler login`):

1. Create the database with `pnpm exec wrangler d1 create cp-reminders` and put its `database_id` in `wrangler.jsonc`; `vars.GH_REPO` names the deployment repository.
2. Apply the schema: `pnpm run db:migrate:remote`.
3. Deploy with the secrets. `wrangler.jsonc` lists them under `secrets.required`, and `wrangler deploy` fails while any is missing ([secrets](https://developers.cloudflare.com/workers/configuration/secrets/)), so the first deploy passes them in a JSON file kept outside the repository:

   ```sh
   pnpm exec wrangler deploy --secrets-file /path/to/secrets.json
   ```

   The file holds the Worker secrets listed in [architecture.md](architecture.md#secrets). `TICK_TOKEN`, `ADMIN_TOKEN` and `SENDER_TOKEN` are distinct random values (`openssl rand -base64 32`); `SENDER_TOKEN` is also a secret of the deployment repository. Later deploys, by `deploy.yml` or by `pnpm run deploy`, keep the stored secrets, and a single secret changes with `pnpm exec wrangler secret put <NAME>`.

4. Check `/health`, `/preview` and `POST /tick` with the commands above. A new cron trigger can take up to 15 minutes to start firing ([cron triggers](https://developers.cloudflare.com/workers/configuration/cron-triggers/)).

### Optional second trigger

An external scheduler can call `POST /tick` as well, for example a cron-job.org job: URL `$BASE/tick`, every 5 minutes, method POST, header `Authorization: Bearer <TICK_TOKEN>`.

## Development

```sh
pnpm run typecheck          # wrangler types, then tsc --noEmit
pnpm test                   # wrangler types, then Vitest in the Workers runtime with a local D1
pnpm run db:migrate:local   # apply migrations/ to the local D1 used by wrangler dev
pnpm run dev                # wrangler dev --test-scheduled
```

Tests run on [`@cloudflare/vitest-plugin`](https://developers.cloudflare.com/workers/testing/vitest-integration/get-started/write-your-first-test/). `test/setup.ts` applies `migrations/` to the test database. Outbound HTTP goes through an injected fetcher in unit tests and a spy on `globalThis.fetch` in route tests, so no test reaches the network.

The test secrets are defined once, as `TEST_SECRETS` in `vitest.config.ts`. Wrangler reads the keys in `secrets.required` from `.dev.vars`, `.env` or `process.env` and warns about missing ones ([local secrets](https://developers.cloudflare.com/workers/configuration/secrets/#local-development-with-secrets)), so the config copies `TEST_SECRETS` into `process.env`. The same object is passed as `miniflare.bindings`, which take precedence over the Wrangler configuration ([Vitest configuration](https://developers.cloudflare.com/workers/testing/vitest-integration/configuration/)), so a developer's own `.dev.vars` never reaches the tests.
