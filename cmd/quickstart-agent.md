# utpr quickstart — guide for AI coding agents

utpr is a CLI for GitHub PR workflows. This guide covers the parts of utpr
built for agent consumption. Humans: run `utpr quickstart` in a terminal for
the styled tour.

## Output modes: plain vs styled

Every utpr command is TTY-aware:

- Piped / non-TTY output is plain, unstyled text automatically. The global
  `--agent` flag forces plain output in a terminal; `--pretty` forces styled
  output in a pipe.
- Command payloads (PR details, CI status, JSON) go to **stdout**. Progress
  and status messages go to stderr, so stdout stays clean for parsing and
  piping.
- Read commands trim themselves for agent use: `utpr view` emits raw
  Markdown (review comment threads carry machine-readable `comment_id` /
  `thread_id` markers), `utpr status` a compact table, and
  `utpr status --json` structured JSON.

## CI status and logs

- `utpr ci` — GitHub Actions checks for the current branch, or pass an
  explicit target: PR number (`utpr ci 123`), branch (`utpr ci @branch`), or
  ref (`utpr ci HEAD~2`). Each failed check carries a one-line failure
  reason taken from GitHub annotations or the failed job's log
  (`--no-reasons` skips it); `--failed` shows only the failures.
- `utpr ci --wait` — block until checks complete: `--wait=all` (default)
  or `--wait=failed` (stop at the first failure). Exits 0 on success and 1
  on failure, so it works as a scripted gate.
- `utpr ci logs` — logs for failed jobs, anchored on error markers by
  default. `--failed` shows all failed jobs without a prompt, `--job <name>`
  picks one job, `--grep <pattern>` filters lines with `-A`/`-B` context,
  and `--full` prints complete logs.
- `utpr ci list` — recent workflow runs grouped by commit.
- `utpr ci rerun` — re-run failed jobs.

## Avoiding interactive prompts

In a terminal, several commands prompt when run without arguments. Piped /
non-TTY runs never hang: they fail fast with guidance instead. To stay fully
non-interactive:

- Pass explicit arguments instead of relying on pickers:
  `utpr view 123`, `utpr ci 123`, `utpr fetch 123`,
  `utpr finish <branch>`.
- Mutating commands take `--yes` to skip confirmation prompts: `init`,
  `resume`, `fetch`, `finish`, `forget`, and `worktree create`.
- `utpr push --edit browser` opens GitHub's PR form instead of a terminal
  prompt. A piped `utpr push` for a branch that already has a PR just
  prints the PR URL.

## Output is pre-trimmed — don't clip it

Agent-facing output is already sized for context windows: check lists and
logs carry a `--max-bytes` cap (256 KiB by default), log windows are
anchored on errors, and agent modes drop decoration. There is no need to
pipe utpr output through `head` or `tail` the way raw `gh` output often
requires. If you really need more, raise or clear the cap with
`--max-bytes` (`0` disables it).
