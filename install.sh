#!/usr/bin/env bash
# utpr installer
#
# Installs utpr and all prerequisites (git, gh, jq, gum) for the
# current platform. Supported: macOS (Homebrew), Linux (Debian/Ubuntu,
# Fedora/RHEL), and WSL.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gadenbuie/utpr/main/install.sh | bash

set -euo pipefail

UTPR_VERSION="${UTPR_VERSION:-main}"
UTPR_URL="https://raw.githubusercontent.com/gadenbuie/utpr/${UTPR_VERSION}/utpr"
INSTALL_DIR="${UTPR_INSTALL_DIR:-$HOME/.local/bin}"

# --- Platform detection ---
OS="$(uname -s)"
IS_WSL=false
if [[ "$OS" == "Linux" ]] && grep -qi microsoft /proc/version 2>/dev/null; then
  IS_WSL=true
fi

# -----------------------------------------------------------------------
# Phase 1: Bootstrap gum using plain echo — gum is not available yet.
# -----------------------------------------------------------------------

_bootstrap_gum() {
  if command -v gum &>/dev/null; then
    return 0
  fi

  echo "==> Installing gum..."

  case "$OS" in
    Darwin)
      if ! command -v brew &>/dev/null; then
        echo "Error: Homebrew is required on macOS." >&2
        echo "Install it from https://brew.sh and re-run this script." >&2
        exit 1
      fi
      brew install gum
      ;;
    Linux)
      if command -v apt-get &>/dev/null; then
        echo "==> Adding Charm apt repository..."
        sudo mkdir -p /etc/apt/keyrings
        curl -fsSL https://repo.charm.sh/apt/gpg.key \
          | sudo gpg --yes --dearmor -o /etc/apt/keyrings/charm.gpg
        echo "deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" \
          | sudo tee /etc/apt/sources.list.d/charm.list > /dev/null
        sudo apt-get update -qq
        sudo apt-get install -y gum
      elif command -v dnf &>/dev/null; then
        echo "==> Adding Charm yum repository..."
        printf '[charm]\nname=Charm\nbaseurl=https://repo.charm.sh/yum/\nenabled=1\ngpgcheck=1\ngpgkey=https://repo.charm.sh/yum/gpg.key\n' \
          | sudo tee /etc/yum.repos.d/charm.repo > /dev/null
        sudo dnf install -y gum
      else
        echo "Error: No supported package manager found (brew, apt-get, or dnf required)." >&2
        exit 1
      fi
      ;;
    *)
      echo "Error: Unsupported OS: $OS" >&2
      exit 1
      ;;
  esac
}

_bootstrap_gum

# -----------------------------------------------------------------------
# Phase 2: gum is available — use it for all remaining output.
# -----------------------------------------------------------------------

_info()    { gum log --level info  "$@"; }
_warn()    { gum log --level warn  "$@"; }
_error()   { gum log --level error "$@"; exit 1; }

gum style \
  --border double --padding "1 3" --border-foreground 4 \
  "$(gum style --bold --foreground 4 "utpr installer")"
echo ""

# --- Install remaining prerequisites ---

_install_deps_macos() {
  local missing=()
  for dep in git gh jq; do
    command -v "$dep" &>/dev/null || missing+=("$dep")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    _info "Installing ${missing[*]} via Homebrew..."
    brew install "${missing[@]}"
  else
    _info "Prerequisites already installed."
  fi
}

_install_deps_apt() {
  local missing_apt=()
  for dep in git jq; do
    command -v "$dep" &>/dev/null || missing_apt+=("$dep")
  done
  if [[ ${#missing_apt[@]} -gt 0 ]]; then
    _info "Installing ${missing_apt[*]}..."
    sudo apt-get install -y "${missing_apt[@]}"
  fi

  if ! command -v gh &>/dev/null; then
    _info "Adding GitHub CLI apt repository..."
    sudo mkdir -p -m 755 /etc/apt/keyrings
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null
    sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
    sudo apt-get update -qq
    _info "Installing gh..."
    sudo apt-get install -y gh
  fi

  if [[ "$IS_WSL" == "true" ]] && ! command -v wslview &>/dev/null; then
    _info "Installing wslu for browser integration..."
    sudo apt-get install -y wslu
  fi
}

_install_deps_dnf() {
  local missing=()
  for dep in git gh jq; do
    command -v "$dep" &>/dev/null || missing+=("$dep")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    _info "Installing ${missing[*]}..."
    sudo dnf install -y "${missing[@]}"
  else
    _info "Prerequisites already installed."
  fi
}

case "$OS" in
  Darwin) _install_deps_macos ;;
  Linux)
    if command -v apt-get &>/dev/null; then
      _install_deps_apt
    elif command -v dnf &>/dev/null; then
      _install_deps_dnf
    else
      _error "No supported package manager found (apt-get or dnf required)."
    fi
    ;;
esac

# --- Install utpr ---
_info "Installing utpr to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR" || _error "Cannot create $INSTALL_DIR. Check permissions or set UTPR_INSTALL_DIR."
gum spin --show-error --title "Downloading utpr..." -- \
  curl -fsSL "$UTPR_URL" -o "$INSTALL_DIR/utpr" \
  || _error "Failed to download utpr from $UTPR_URL"
if ! head -1 "$INSTALL_DIR/utpr" | grep -q '^#!/usr/bin/env bash'; then
  rm -f "$INSTALL_DIR/utpr"
  _error "Downloaded file doesn't appear to be a valid utpr script. Aborting."
fi
chmod +x "$INSTALL_DIR/utpr"
_info "Installed: $INSTALL_DIR/utpr"

# --- Check PATH ---
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  _warn "$INSTALL_DIR is not in your PATH."

  shell_rc="your shell's rc file"
  case "${SHELL:-}" in
    */zsh)  shell_rc="$HOME/.zshrc" ;;
    */bash) shell_rc="$HOME/.bashrc" ;;
  esac

  gum style --border rounded --padding "0 1" --border-foreground 3 \
    "Add this line to $shell_rc, then open a new terminal:" \
    "" \
    "$(gum style --foreground 6 "  export PATH=\"$INSTALL_DIR:\$PATH\"")"
  echo ""
fi

# --- Check gh auth ---
if ! gh auth status &>/dev/null; then
  _warn "GitHub CLI is not authenticated. Run: gh auth login"
  echo ""
fi

# --- Done ---
gum style \
  --border rounded --padding "1 2" --border-foreground 2 \
  "$(gum style --bold --foreground 2 "Installation complete!")" \
  "" \
  "  Run $(gum style --foreground 6 'utpr --help') to get started."
