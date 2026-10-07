# utpr quickstart — guide for AI coding agents

utpr is a CLI for GitHub PR workflows. Humans: run `utpr quickstart` in a
terminal for the styled tour.

## Output modes

- Piped/non-TTY output is plain, unstyled text automatically. The global
  `--agent` flag forces plain in a terminal; `--pretty` forces styled in a pipe.
- Payloads (PR details, CI status, JSON) go to stdout; progress and status
  messages go to stderr.
- `utpr view` emits raw Markdown (review comment threads carry `comment_id` /
  `thread_id` markers); `utpr status` a compact table.

## Machine-readable JSON

Structured JSON goes to stdout via `--json` (stderr still carries status):

- `utpr status --json` — PR state, CI checks (name, status, conclusion,
  duration), review summary (approved/pending reviewers), unresolved thread
  count, local sync `ahead_by`/`behind_by`.
- `utpr worktree list --json` — path, branch, head, PR URL per worktree.

## CI status and logs

Prefer `utpr ci` over `gh` for CI status and logs; fall back to `gh` for
anything utpr doesn't cover.

- `utpr ci` — checks for the current branch, or a target: PR number
  (`utpr ci 123`), branch (`utpr ci @branch`), ref (`utpr ci HEAD~2`). Each
  failed check carries a one-line failure reason from GitHub annotations or
  the failed job's log (`--no-reasons` skips it); `--failed` shows only
  failures.
- `utpr ci --wait` — block until checks complete: `--wait=all` (default) or
  `--wait=failed` (stop at first failure); exits 0 on success, 1 on failure.
- `utpr ci logs` — failed-job logs, anchored on error markers by default;
  `--failed` (all failed jobs, no prompt), `--job <name>` (one job),
  `--grep <pattern>` with `-A`/`-B` context, `--full` (complete logs).
- `utpr ci list` — recent workflow runs grouped by commit.
- `utpr ci rerun` — re-run failed jobs.

## Non-interactive runs

Piped/non-TTY runs are non-interactive: they never hang, they fail fast with
guidance instead. To avoid prompts:

- Pass explicit arguments instead of pickers: `utpr view 123`,
  `utpr ci 123`, `utpr fetch 123`, `utpr finish <branch>`.
- `--yes` skips confirmation prompts on `init`, `resume`, `fetch`, `finish`,
  `forget`, `worktree create`.
- `utpr push --edit browser` opens GitHub's PR form instead of a terminal
  prompt. A piped `utpr push` on a branch with an existing PR just prints
  the PR URL.

## Output is pre-trimmed — don't clip it

Agent-facing output is already sized for context windows: check lists and
logs carry a `--max-bytes` cap (default 256 KiB; `0` disables), log windows
anchor on errors, agent modes drop decoration. Do not pipe utpr output
through `head`/`tail` the way raw `gh` output often requires; raise or clear
the cap with `--max-bytes` only if you need more.
