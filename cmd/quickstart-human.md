# utpr quickstart

utpr is a CLI for GitHub PR workflows, inspired by the `pr_*()` functions in
the R [usethis](https://usethis.r-lib.org/articles/pr-functions.html) package.
It carries you through the whole PR lifecycle: start a branch, open a PR,
keep it current, watch CI, and clean up after the merge.

## The PR lifecycle

### 1. Start a PR branch — `utpr init`

```
utpr init my-feature
```

Creates the PR branch (with your repo's prefix, e.g. `feat/my-feature`),
switches to it, and remembers it for later cleanup.

### 2. Push and open the PR — `utpr push`

```
utpr push
```

Pushes the branch and creates or updates its PR. On the first push you
write the title and description in your terminal (or pass `--edit browser`
to draft them on GitHub instead).

### 3. See where things stand — `utpr status` / `utpr view`

```
utpr status
utpr view
```

`status` is a quick table: PR state, CI checks, reviews, unresolved review
threads, and whether your local branch is in sync with the remote. `view`
shows the full PR with comments.

### 4. Watch CI — `utpr ci`

```
utpr ci
utpr ci --wait
```

Shows GitHub Actions checks for the current branch, with a one-line failure
reason for each failed check. `--wait` blocks until the checks finish, and
`utpr ci logs` digs into the logs of failed jobs.

### 5. Keep current, switch gears

```
utpr merge-main
utpr pause
utpr resume my-feature
```

`merge-main` brings the default branch into your PR branch. `pause` switches
back to the default branch; `resume` picks the PR back up whenever you're
ready.

### 6. Wrap up — `utpr finish`

```
utpr finish
```

After the PR merges: deletes the local branch, prunes the remote branch, and
clears the metadata utpr stashed along the way.

## Going further

- `utpr <command> --help` — detailed usage for any command
- `utpr fetch <pr-number>` — pull down someone else's PR to review or test
  it locally
- `utpr clean` — tidy up merged branches and stale remotes in one pass
- `utpr quickstart --agent` — the same tour, written for AI coding agents
- The [README](https://github.com/gadenbuie/utpr#readme) — installation,
  worktrees, configuration, troubleshooting
