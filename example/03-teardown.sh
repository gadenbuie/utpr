#!/usr/bin/env bash
# Teardown script for the utpr demo.
#
# Deletes the demo GitHub repository and removes the local clone.
#
# Usage: bash example/03-teardown.sh

set -euo pipefail

ENV_FILE="/tmp/utpr-demo.env"

info()    { gum log --level info "$@"; }
warn()    { gum log --level warn "$@"; }

if [[ ! -f "$ENV_FILE" ]]; then
  gum log --level error "$ENV_FILE not found — was 01-setup.sh run?"
  exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

if gh repo view "$GH_USER/$REPO_NAME" &>/dev/null; then
  warn "Deleting GitHub repo '$GH_USER/$REPO_NAME'..."
  gum spin --title "Deleting repo..." -- \
    gh repo delete "$GH_USER/$REPO_NAME" --yes
  info "Repo deleted."
else
  info "Repo '$GH_USER/$REPO_NAME' not found — already deleted?"
fi

if [[ -d "$DEMO_DIR" ]]; then
  warn "Removing local clone at $DEMO_DIR..."
  rm -rf "$DEMO_DIR"
  info "Local clone removed."
else
  info "Directory $DEMO_DIR not found — already removed?"
fi

info "Removing env file $ENV_FILE..."
rm -f "$ENV_FILE"

gum style \
  --border rounded --padding "1 2" --border-foreground 3 \
  "$(gum style --bold "Teardown complete.")"
