# utpr

A command-line tool for pull request workflows, inspired by the
[`pr_*()` helper functions][usethis-pr] from the R [usethis] package.

<p align="center"><img src="example/demo.gif" alt="utpr demo" /></p>

## Why utpr?

Working with pull requests involves a surprising amount of repetitive,
error-prone Git and GitHub ceremony: creating branches, configuring
remotes, fetching contributor forks, pushing and opening PRs, cleaning up
after merges. The [usethis] R package solved this beautifully with its
[`pr_*()` functions][usethis-pr], which abstract the entire PR lifecycle
into simple, memorable commands.

**utpr brings that same workflow to any terminal** — no R required.
It uses `git`, `gh`, and `gum` to provide a polished, interactive
experience for the full pull request round-trip:

- **Start work** with `utpr init`, which creates a branch from a
  freshly-pulled default branch.
- **Switch context** freely with `utpr pause` and `utpr resume`.
- **Push and create PRs** with `utpr push`, which handles first-push
  setup and subsequent updates.
- **Review others' PRs** with `utpr fetch`, which configures remotes
  and tracking branches automatically — even for fork-based PRs.
- **Stay up to date** with `utpr pull` and `utpr merge-main`.
- **Clean up** with `utpr finish` after a merge, or `utpr forget`
  to abandon work.
- **Isolate AI coding agents** with `--worktree` on `utpr init` and
  `utpr fetch`: each agent gets its own Git worktree, pre-wired with
  your project's shared config, and `utpr finish` tears it down cleanly
  when the PR is merged.

utpr automatically detects whether you own the repo or are working from
a fork, and configures remotes accordingly. It tracks which branches and
remotes it created, so cleanup is safe and thorough.

For more background on the workflow that inspired utpr, see
[Pull Request Flow with usethis][blog-pr-flow] and the
[usethis pull request helpers documentation][usethis-pr].

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/gadenbuie/utpr/main/install.sh | bash
```

This installs utpr and all prerequisites to `~/.local/bin`. Supported
platforms: **macOS** (via Homebrew), **Linux** (Debian/Ubuntu and
Fedora/RHEL), and **WSL**.

After installation, authenticate the GitHub CLI if you haven't already:

```bash
gh auth login
```

### Manual installation

utpr requires the following tools:

| Tool | Purpose |
|------|---------|
| [git](https://git-scm.com) | Version control |
| [gh](https://cli.github.com) | GitHub CLI (must be authenticated) |
| [jq](https://jqlang.github.io/jq/) | JSON processing |
| [gum](https://github.com/charmbracelet/gum) | Interactive terminal UI |

Once prerequisites are installed, download utpr:

```bash
mkdir -p ~/.local/bin
curl -fsSL https://raw.githubusercontent.com/gadenbuie/utpr/main/utpr -o ~/.local/bin/utpr
chmod +x ~/.local/bin/utpr
```

Make sure `~/.local/bin` is in your `PATH` (add to `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Usage

```
utpr <command> [options]
```

| Command | Description |
|---------|-------------|
| `utpr init <branch> [--worktree]` | Create a new PR branch, optionally in a worktree |
| `utpr pause` | Switch back to the default branch |
| `utpr resume [<branch>]` | Resume work on a PR branch |
| `utpr fetch [<pr>] [--worktree]` | Fetch a PR from GitHub, optionally into a worktree |
| `utpr push [--edit=...]` | Push branch and create/update PR |
| `utpr pull` | Pull latest changes |
| `utpr merge-main` | Merge default branch into current branch |
| `utpr forget` | Abandon local PR branch |
| `utpr finish [<pr>]` | Clean up after a merged PR |
| `utpr view [<pr>]` | View PR in browser |

Run `utpr <command> --help` for detailed usage of any command.

### Typical workflow

```bash
# Start a new feature
utpr init my-feature

# ... write code, commit changes ...

# Push and create a PR (interactive terminal prompts)
utpr push

# ... address review feedback ...
utpr push

# Switch to something else
utpr pause

# Come back later
utpr resume my-feature

# Stay current with the default branch
utpr merge-main

# After the PR is merged
utpr finish
```

### Reviewing a PR

```bash
# Fetch a contributor's PR (interactive selection)
utpr fetch

# Or by number
utpr fetch 42

# View it in the browser
utpr view

# Done reviewing
utpr finish
```

### Working with worktrees

The `--worktree` flag on `utpr init` and `utpr fetch` creates a
[Git worktree][git-worktree] instead of checking out the branch in
your main repo. Each worktree is a fully independent working directory,
so you can have multiple branches active simultaneously without
disturbing your main checkout.

This is especially useful for **AI coding agents**: give each agent
its own isolated workspace, pre-configured with your project's shared
settings.

```bash
# Start a new branch in its own worktree
utpr init feat/add-auth --worktree

# Fetch a contributor's PR into a worktree for parallel review
utpr fetch 42 --worktree
```

Worktrees are created at `<parent>/<repo>.worktrees/<branch>/`.
utpr automatically:

- Symlinks shared directories (`.claude`, `_dev`) into each worktree
- Runs project setup when detected (`npm install`, `uv sync`, `make setup`)
- Opens the worktree in your current editor

`utpr finish` and `utpr forget` clean up worktrees automatically.
`utpr resume` detects when a branch has a worktree and offers to
navigate there instead of switching in the main repo.

The symlinked directories are controlled by the `UTPR_SYMLINK_DIRS`
environment variable (default: `_dev,.claude`).

## Acknowledgments

utpr is a standalone reimplementation of the
[pull request helpers][usethis-pr] from [usethis], an R package by
[Hadley Wickham][hadley], [Jennifer Bryan][jennybc], [Malcolm
Barrett][malcolm], and the [tidyverse team][tidyverse]. The
usethis PR functions made collaborative GitHub workflows delightful
for R developers — utpr aims to bring that same experience to any
project, in any language.

For a detailed walkthrough of the workflow, see
[Pull Request Flow with usethis][blog-pr-flow] by [Garrick
Aden-Buie][garrick].

## License

MIT

[git-worktree]: https://git-scm.com/docs/git-worktree
[usethis]: https://usethis.r-lib.org
[usethis-pr]: https://usethis.r-lib.org/articles/pr-functions.html
[blog-pr-flow]: https://www.garrickadenbuie.com/blog/pull-request-flow-usethis/
[hadley]: https://github.com/hadley
[jennybc]: https://github.com/jennybc
[malcolm]: https://github.com/malcolmbarrett
[tidyverse]: https://github.com/tidyverse
[garrick]: https://github.com/gadenbuie
