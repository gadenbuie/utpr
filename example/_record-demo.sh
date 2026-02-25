#!/usr/bin/env bash
# Record the utpr demo GIF.
#
# Runs setup, records demo.tape with vhs, then tears down the demo repo.
# The finished GIF is written to example/demo.gif.
#
# Usage: bash example/_record-demo.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

gum style \
  --border double --padding "1 3" --border-foreground 4 \
  "$(gum style --bold --foreground 4 "utpr demo recorder")"

echo ""

# --- Dependency check ---
for cmd in vhs gh git gum utpr; do
  if ! command -v "$cmd" &>/dev/null; then
    gum log --level error "'$cmd' is not on PATH."
    exit 1
  fi
done

# --- Setup ---
gum log --level info "[ 1/3 ] Running setup..."
bash "$SCRIPT_DIR/01-setup.sh"
echo ""

# --- Record ---
gum log --level info "[ 2/3 ] Recording with vhs..."
cd "$REPO_ROOT"
vhs example/02-demo.tape
echo ""

# --- Teardown ---
gum log --level info "[ 3/3 ] Running teardown..."
bash "$SCRIPT_DIR/03-teardown.sh"
echo ""

gum style \
  --border double --padding "1 3" --border-foreground 2 \
  "$(gum style --bold --foreground 2 "Done!") GIF saved to $(gum style --foreground 4 "example/demo.gif")"
