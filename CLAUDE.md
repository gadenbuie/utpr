# utpr

A Go CLI for GitHub PR workflows, inspired by the `pr_*()` functions from the R [usethis](https://usethis.r-lib.org/articles/pr-functions.html) package. External dependencies: `git`, `gh`.

## Architecture

- **cobra** for command routing (`cmd/root.go` registers all subcommands).
- **`cmd/`** — one file per subcommand plus `helpers.go` for shared challenge/pull helpers and `worktree.go` for worktree setup logic.
- **`internal/`** — private packages: `git` (git operations), `gh` (GitHub API via go-gh), `remote` (remote detection/caching), `ui` (output, prompts, spinner), `editor` (editor detection/open).
- **charmbracelet** libraries for UI: `lipgloss` (styling), `huh` (prompts/spinner), `glamour` (markdown rendering).
- **Version info** injected at build time via ldflags into `main.go` → `cmd.SetVersionInfo()`.

## Key design decisions

- **All user output goes to stderr** via `ui.Info/Warn/Error/Success` helpers (`internal/ui/output.go`), keeping stdout clean for piping.
- **`ui.Die()`/`ui.Dief()` for fatal errors** — prints to stderr and returns an `error`; cobra handles the exit. Errors propagate via Go's standard return-error pattern.
- **`ui.Spin()` for long operations** — wraps charmbracelet `huh/spinner`. Not used for `git merge`, which runs via `git.RunInteractive()` so conflicts are visible to the user.
- **`remote.Detect()` is cached** — returns a `*remote.Config` struct with `Layout`, `SourceRemote`, `PushRemote`, `DefaultBranch`; subsequent calls return the cached result.
- **Remote branch deletion in `cmd_finish`** only targets repos the user can push to — compares `pr.Head.Repo.FullName` against the push remote's repo before attempting DELETE.
- **Branch/remote metadata** stored in git config: `branch.<name>.created-by`, `branch.<name>.pr-url`, `remote.<name>.created-by`. Used by `remote.CleanupUtprRemotes()` to safely remove only utpr-created remotes.
- **Worktree support** via `--worktree` flag on `init` and `fetch`. Worktrees are created at `<parent>/<repo>.worktrees/<branch>`. `forget`/`finish` automatically clean up worktrees. `resume` offers navigation to existing worktrees. `pause` detects worktree context and prints main repo path.
- **`git.IsInWorktree()` guard** on `forget`/`finish` — these commands must run from the main repo (not a worktree) because they delete the branch, which would break the worktree.
- **Worktree path discovery** uses `git.GetBranchWorktreePath()` (live `git worktree list --porcelain` query) as the single source of truth — no config key duplication.
- **Environment variables:** `UTPR_EDITOR` overrides auto-detected editor for worktree open. `UTPR_SYMLINK_DIRS` (comma-separated) controls which files/directories are symlinked into worktrees; items are offered only if they exist in the main repo but not in the worktree (not tracked by git). Gitignored files are included naturally. Default covers: `_dev,.claude,.env,.env.local,.Renviron,.Rprofile,.agents,.secrets,secrets,.htpasswd,.vscode,.vscode/settings.json`.

## Development notes

- Run `go vet ./...` and `go test ./...` before committing — both must pass clean.
- If present, the implementation plan and review history are in `_dev/plan.md`.

## Cutting a release

1. Push `main`.
2. Draft release notes from commits since last tag (`git log $(git describe --tags --abbrev=0)..HEAD --oneline`): one summary line + bullets for user-facing changes; skip chore/internal commits.
3. Create an annotated tag:
   ```
   git tag -a vX.Y.Z -m "Summary line

   - Change one
   - Change two"
   ```
4. `git push origin vX.Y.Z`

The workflow builds cross-platform binaries and publishes a GitHub release. The tag message becomes the release body; without one, only the Full Changelog link is shown.
