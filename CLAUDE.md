# utpr

A bash CLI for GitHub PR workflows, inspired by the `pr_*()` functions from the R [usethis](https://usethis.r-lib.org/articles/pr-functions.html) package. Dependencies: `git`, `gh`, `jq`, `gum`.

## Key design decisions

- **All user output goes to stderr** via `gum_info/warn/error/success` helpers, keeping stdout clean.
- **`die()` for fatal errors** — no ERR trap. All error exits call `die "message"` explicitly.
- **`git_spin()` for long git ops** — wraps `gum spin`, captures stderr, displays it on failure. Not used for `git merge` (conflicts must be visible).
- **`detect_remote_config()` is cached** — sets globals `REMOTE_CONFIG`, `SOURCE_REMOTE`, `PUSH_REMOTE`, `DEFAULT_BRANCH` once; subsequent calls return early.
- **Remote branch deletion in `cmd_finish`** only targets repos the user can push to — compares `pr_head_repo` against `PUSH_REMOTE` before attempting DELETE.
- **Branch/remote metadata** stored in git config: `branch.<name>.created-by`, `branch.<name>.pr-url`, `branch.<name>.worktree-path`, `remote.<name>.created-by`. Used by `_cleanup_utpr_remote` to safely remove only utpr-created remotes.
- **Worktree support** via `--worktree` flag on `init` and `fetch`. Worktrees are created at `<parent>/<repo>.worktrees/<branch>`. `cmd_forget`/`cmd_finish` automatically clean up worktrees. `cmd_resume` offers navigation to existing worktrees. `cmd_pause` detects worktree context and prints main repo path.
- **`is_in_worktree()` guard** on `cmd_forget`/`cmd_finish` — these commands must run from the main repo (not a worktree) because they delete the branch, which would break the worktree.

## Development notes

- Run `shellcheck utpr` before committing — must pass clean.
- If present, the implementation plan and review history are in `_dev/plan.md`.
