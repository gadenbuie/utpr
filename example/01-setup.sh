#!/usr/bin/env bash
# Setup script for the utpr demo.
#
# Creates a GitHub repository seeded with initial content and one pre-merged
# PR so that `utpr finish 1` has a real merged PR to clean up in the recording.
#
# Writes /tmp/utpr-demo.env on success.
#
# Usage: bash example/01-setup.sh

set -euo pipefail

REPO_NAME="utpr-demo"
DEMO_DIR="/tmp/utpr-demo"
ENV_FILE="/tmp/utpr-demo.env"

info()    { gum log --level info    "$@"; }
warn()    { gum log --level warn    "$@"; }
success() { gum log --level info    "$@"; }

# --- GitHub identity ---
info "Getting GitHub username..."
GH_USER=$(gh api user --jq '.login')
info "GitHub user: $GH_USER"

# --- Clean up any prior state ---
if gh repo view "$GH_USER/$REPO_NAME" &>/dev/null; then
  warn "Removing existing '$REPO_NAME' repo..."
  gum spin --title "Deleting $GH_USER/$REPO_NAME..." -- \
    gh repo delete "$GH_USER/$REPO_NAME" --yes
fi

if [[ -d "$DEMO_DIR" ]]; then
  warn "Removing existing demo directory..."
  rm -rf "$DEMO_DIR"
fi

# --- Create the GitHub repository ---
info "Creating '$GH_USER/$REPO_NAME' on GitHub..."
gum spin --title "Creating repo..." -- \
  gh repo create "$GH_USER/$REPO_NAME" \
    --public \
    --description "Demo repository for utpr — a GitHub PR workflow CLI"

info "Cloning into $DEMO_DIR..."
gum spin --title "Cloning..." -- \
  gh repo clone "$GH_USER/$REPO_NAME" "$DEMO_DIR"
cd "$DEMO_DIR"

# Carry forward any local git identity
git config user.email "$(git config --global user.email 2>/dev/null || echo '')"
git config user.name  "$(git config --global user.name  2>/dev/null || echo '')"

# --- Seed main with a README ---
info "Creating initial content on main..."
cat > README.md << 'HEREDOC'
# utpr Demo Project

A sandbox repository for exploring the `utpr` GitHub PR workflow CLI.

## About

`utpr` is a bash tool for managing pull request workflows from the terminal,
inspired by the `pr_*()` functions from the R usethis package.

## Usage

```bash
utpr init <branch>   # start a new PR branch
utpr push            # push branch and open a pull request
utpr pause           # return to the main branch
utpr resume          # switch back to a PR branch
utpr finish          # clean up after a PR is merged
```
HEREDOC

git add README.md
git commit -m "docs: add README" --quiet
gum spin --title "Pushing main..." -- git push -u origin main

# --- Create and merge a PR (gives the demo a real merged PR to finish) ---
info "Creating PR #1 (feat/add-contributing)..."
git switch -c feat/add-contributing --quiet

cat > CONTRIBUTING.md << 'HEREDOC'
# Contributing

Thank you for your interest in contributing to this project!

## How to contribute

1. Fork the repository
2. Create a feature branch: `utpr init feat/my-feature`
3. Make your changes and commit them
4. Push and open a PR: `utpr push`
5. Clean up after merge: `utpr finish`
HEREDOC

git add CONTRIBUTING.md
git commit -m "docs: add contributing guide" --quiet
gum spin --title "Pushing feat/add-contributing..." -- git push -u origin feat/add-contributing

gum spin --title "Creating PR..." -- \
  gh pr create \
    --title "docs: add contributing guide" \
    --body "Adds a CONTRIBUTING.md to guide new contributors." \
    --base main

gum spin --title "Merging PR #1..." -- gh pr merge --squash

# Return to main and pull the squash commit
git switch main --quiet
gum spin --title "Pulling main..." -- git pull origin main

# Delete the local branch so it doesn't appear in `utpr resume`'s picker,
# but leave the remote branch for `utpr finish 1` to clean up.
git branch -D feat/add-contributing

# --- Write the demo environment file ---
cat > "$ENV_FILE" << HEREDOC
DEMO_DIR="$DEMO_DIR"
GH_USER="$GH_USER"
REPO_NAME="$REPO_NAME"
HEREDOC

gum style \
  --border rounded --padding "1 2" --border-foreground 2 \
  "$(gum style --bold --foreground 2 "Setup complete!")" \
  "" \
  "  Repo:      https://github.com/$GH_USER/$REPO_NAME" \
  "  Local dir: $DEMO_DIR" \
  "  Env file:  $ENV_FILE"
