# GitHub Actions workflows

`.github/workflows/` holds three workflows. Every action is pinned to a full commit SHA, with its release in a trailing comment.

## Where they run

The same files exist in `mitaoe/cp-reminders` and in its fork, the deployment repository ([README](../README.md#where-it-runs)).

| File | Workflow | Jobs (id: name) | Runs in | Condition |
|---|---|---|---|---|
| `ci.yml` | CI | `worker`: Worker Type Check and Tests; `sender`: Sender Vet and Tests | Every repository | Always |
| `send.yml` | Send Reminders | `drain`: Drain Outbox; `persist`: Persist State | The deployment repository only | The repository variable `BOT_DEPLOYMENT` is `true` |
| `deploy.yml` | Deploy Worker | `check`: Type Check and Tests; `deploy`: Deploy to Cloudflare | The deployment repository only | The repository variable `BOT_DEPLOYMENT` is `true` |

The Worker dispatches `send.yml` and the README's CI badge points at `ci.yml`, both by file name; renaming a workflow changes only the badge's label.

Every job of the two bot workflows checks the variable, so in any other repository, including `mitaoe/cp-reminders` and forks made by contributors, those jobs are skipped rather than failing for want of secrets. Which repository runs the bot is therefore a repository setting, not a name in the code. Workflows in a new fork run only after Actions is enabled in its Actions tab ([forks](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)).

## send.yml: drain the outbox

The Worker starts `send.yml` through `workflow_dispatch` with no inputs ([architecture.md](architecture.md#workflow-dispatch)); it has no other trigger. Because every run drains the whole outbox, an extra or a missing wake-up does no harm.

It has two jobs, so that the token that can write to the repository never shares a runner with the sender and its dependencies:

| Job | Permissions | Runs |
|---|---|---|
| `drain` (Drain Outbox) | `contents: read` | The sender, with its secrets. |
| `persist` (Persist State), `needs: drain` | `contents: write` | No repository code: a pinned artifact action and `git`, pushing only `refs/heads/state`. |

What `drain` hands to `persist` is untrusted: the artifact's contents and the outputs `changed` and `state-sha`. `persist` uses only the bytes of two regular files from the artifact and a value of `state-sha` that is empty or a commit ID; the artifact never becomes part of a git directory, and git runs without system or global configuration and without hooks. Code in `drain` can therefore choose what branch `state` holds, which it can already do with `AGE_IDENTITY`, but cannot obtain a token that writes `main`. It can also write Actions cache entries in the `main` scope, which `drain`, both jobs of `ci.yml` and both jobs of `deploy.yml` restore; `pnpm/setup` restores its lockfile-verification cache even in the `deploy` job, which does not use the pnpm store cache.

| Aspect | Setting | Reason |
|---|---|---|
| Concurrency | `group: wa-session`, `cancel-in-progress: false` | WhatsApp allows one live connection per session and the keys rotate on every connection, so runs are serialised, both jobs included. A running workflow is never cancelled, and at most one run waits as pending; a newer pending run replaces it ([concurrency](https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/control-the-concurrency-of-workflows-and-jobs)). |
| Permissions | `{}` for the workflow, then per job as above | Each job gets only what it uses. |
| Timeout | `drain` 6 minutes, `persist` 5 minutes | The sender's own limit is 4 minutes, including up to 90 s of jitter. |
| Checkout | `main` with `persist-credentials: false` | No token remains in `.git/config` while the sender runs. |
| Secrets | Passed to the drain step through `env:` | Expressions appear only in `env:` and `with:`, never inside a script. The secrets are listed in [architecture.md](architecture.md#secrets). |

`drain` steps:

1. **Fetch state branch.** Initialises `./state` as a repository on branch `state` and fetches branch `state` with an `http.extraheader` built from `github.token` for that command only. When the branch does not exist yet, `./state` stays an empty orphan branch. The fetched commit is the job output `state-sha`.
2. **Set up Go** from `sender/go.mod`, with the module cache keyed on `sender/go.sum`.
3. **Drain the outbox.** `go run ./cmd/wa drain --state "$GITHUB_WORKSPACE/state"` in `sender/`, which is its own module (`cp-reminders/sender`). On failure the sender writes the unsent texts to the job summary, and GitHub emails the owner about the failed run.
4. **Detect state changes** (`if: always()`, because the keys rotate even when the drain fails): the job output `changed` is `true` when `session.db.age` or `sent.json` differs from the fetched commit.
5. **Upload state** when changed: exactly those two files, as artifact `state` kept for one day. The artifact holds the same encrypted session that branch `state` publishes.

`persist` runs after `drain`, whatever its result, when `BOT_DEPLOYMENT` is `true` and `changed` is `true`:

1. **Download state** into `$RUNNER_TEMP/artifact`.
2. **Push state branch.** Checks that `state-sha` is empty or a commit ID; creates a new repository in `$RUNNER_TEMP/repo` before reading the artifact; copies `session.db.age` and `sent.json` into it if they are regular files, and fails on a symlink, a directory or any other kind of file; commits them as a single commit by `github-actions[bot]`; and runs `git push --force-with-lease="refs/heads/state:$STATE_SHA" origin HEAD:refs/heads/state`. The allowlist keeps a plaintext `session.db` out of the repository, the single commit keeps the repository from growing, and the lease rejects the push if the branch moved since the fetch; an empty `state-sha` requires that the branch does not exist.

If `persist` fails, its artifact is kept for a day: open the failed `persist` job and choose "Re-run this job", which reuses `drain`'s outputs and artifact. "Re-run failed jobs" or "Re-run all jobs" would run `drain` again, whenever it failed too, with the session from before the run, whose keys have rotated since.

## deploy.yml: deploy the Worker

Runs on a push to `main` that changes `worker/`, the workspace files (`package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`) or `deploy.yml` itself, which in the deployment repository means a sync of the fork. It can also be started by hand (`workflow_dispatch`).

| Aspect | Setting | Reason |
|---|---|---|
| Concurrency | `group: deploy-worker`, `cancel-in-progress: false` | Deploys run one at a time and in order. |
| Permissions | `contents: read` | It only reads the code. |
| Jobs | `check`, then `deploy` (`needs: check`), each on its own runner | The typecheck and the tests, which run the test framework and the repository's test code, never run on the runner that receives the Cloudflare token. |
| Secrets | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` in the `env:` of the migration and deploy steps of `deploy` | Wrangler reads both from the environment ([system environment variables](https://developers.cloudflare.com/workers/wrangler/system-environment-variables/)). Wrangler and the packages it loads necessarily run with the token. |
| Cache | `check` restores and saves the pnpm store; `deploy` does not use it and installs from the registry | Nothing written by a job that ran test code reaches the runner that receives the token, and every package `deploy` installs is checked against the lockfile's hashes. |

Jobs, in `worker/`:

1. `check`: typecheck and test, so a change that fails the checks is never deployed.
2. `deploy`: apply pending D1 migrations (`pnpm run db:migrate:remote`), then deploy with `pnpm run deploy --message "$GITHUB_SHA"`, which keeps the Worker's stored secrets.

## ci.yml: checks

Runs on every push and pull request, with `contents: read` and a concurrency group that cancels older runs for the same ref. It uses no secrets, so it runs the same for pull requests from forks.

| Job | Steps |
|---|---|
| `worker` | [`pnpm/setup`](https://github.com/pnpm/setup) installs Node 24, the pnpm version in the root `packageManager` field, and the workspace; then `pnpm --filter cp-reminders-worker typecheck` and `test`. The tests include the golden tests in `worker/test/core/`. |
| `sender` | `go vet ./...` and `go test ./...` in `sender/`. |

The root `package.json` contains only `name`, `private` and `packageManager`: `pnpm/setup` takes the pnpm version from it and runs `pnpm install` when a `package.json` is present.
