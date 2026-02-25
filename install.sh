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

PKG_MANAGER="unknown"
case "$OS" in
  Darwin)
    PKG_MANAGER="brew"
    ;;
  Linux)
    if command -v apt-get &>/dev/null; then
      PKG_MANAGER="apt-get"
    elif command -v dnf &>/dev/null; then
      PKG_MANAGER="dnf"
    fi
    ;;
esac

_die() {
  local msg="$1"
  if command -v gum &>/dev/null; then
    gum log --level error "$msg"
  else
    echo "Error: $msg" >&2
  fi
  exit 1
}

_as_root() {
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    "$@"
  elif command -v sudo &>/dev/null; then
    sudo "$@"
  else
    _die "This installer needs root privileges for this operation, but sudo is not available."
  fi
}

_require_cmd() {
  local cmd="$1"
  local context="${2:-this installer}"
  command -v "$cmd" &>/dev/null || {
    _die "Required command '$cmd' was not found (${context})."
  }
}

_require_cmd curl "download dependencies and utpr"

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
        _die "Homebrew is required on macOS. Install it from https://brew.sh and re-run this script."
      fi
      brew install gum
      ;;
    Linux)
      if command -v apt-get &>/dev/null; then
        _require_cmd gpg "install gum on apt-based Linux"
        echo "==> Adding Charm apt repository..."
        _as_root mkdir -p /etc/apt/keyrings
        curl -fsSL https://repo.charm.sh/apt/gpg.key \
          | _as_root gpg --yes --dearmor -o /etc/apt/keyrings/charm.gpg
        echo "deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" \
          | _as_root tee /etc/apt/sources.list.d/charm.list > /dev/null
        _as_root apt-get update -qq
        _as_root apt-get install -y gum
      elif command -v dnf &>/dev/null; then
        echo "==> Adding Charm yum repository..."
        printf '[charm]\nname=Charm\nbaseurl=https://repo.charm.sh/yum/\nenabled=1\ngpgcheck=1\ngpgkey=https://repo.charm.sh/yum/gpg.key\n' \
          | _as_root tee /etc/yum.repos.d/charm.repo > /dev/null
        _as_root dnf install -y gum
      else
        _die "No supported package manager found (brew, apt-get, or dnf required)."
      fi
      ;;
    *)
      _die "Unsupported OS: $OS"
      ;;
  esac
}

_bootstrap_gum

# -----------------------------------------------------------------------
# Phase 2: gum is available — use it for all remaining output.
# -----------------------------------------------------------------------

_info()    { gum log --level info  "$@"; }
_warn()    { gum log --level warn  "$@"; }
_error()   { _die "$*"; }

gum style \
  --border double --padding "1 3" --border-foreground 4 \
  "$(gum style --bold --foreground 4 "utpr installer")"
echo ""
_info "Preflight: os=$OS package_manager=$PKG_MANAGER install_dir=$INSTALL_DIR version=$UTPR_VERSION wsl=$IS_WSL"

# --- Install remaining prerequisites ---

_install_deps_macos() {
  local missing=()
  for dep in git gh jq; do
    command -v "$dep" &>/dev/null || missing+=("$dep")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    command -v brew &>/dev/null || _error "Homebrew is required to install missing prerequisites on macOS."
    _info "Installing ${missing[*]} via Homebrew..."
    brew install "${missing[@]}"
  else
    _info "Prerequisites already installed."
  fi
}

_install_deps_apt() {
  local missing_apt=()
  local need_gh=false
  local need_wslu=false
  local refresh_apt=false
  local gh_repo_path="/etc/apt/sources.list.d/github-cli.list"
  local gh_keyring_path="/etc/apt/keyrings/githubcli-archive-keyring.gpg"

  for dep in git jq; do
    command -v "$dep" &>/dev/null || missing_apt+=("$dep")
  done
  if [[ ${#missing_apt[@]} -gt 0 ]]; then
    refresh_apt=true
  fi

  if ! command -v gh &>/dev/null; then
    need_gh=true
    refresh_apt=true
  fi

  if [[ "$IS_WSL" == "true" ]] && ! command -v wslview &>/dev/null; then
    need_wslu=true
    refresh_apt=true
  fi

  if [[ "$need_gh" == "true" ]]; then
    _require_cmd dpkg "configure GitHub CLI apt repository"
    _as_root mkdir -p -m 755 /etc/apt/keyrings

    if [[ ! -f "$gh_keyring_path" ]]; then
      _info "Adding GitHub CLI apt keyring..."
      curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | _as_root tee "$gh_keyring_path" > /dev/null
      _as_root chmod go+r "$gh_keyring_path"
    fi

    if [[ ! -f "$gh_repo_path" ]] || ! grep -q 'https://cli.github.com/packages stable main' "$gh_repo_path"; then
      _info "Adding GitHub CLI apt repository..."
      echo "deb [arch=$(dpkg --print-architecture) signed-by=$gh_keyring_path] https://cli.github.com/packages stable main" \
        | _as_root tee "$gh_repo_path" > /dev/null
    fi
  fi

  if [[ "$refresh_apt" == "false" ]]; then
    _info "Prerequisites already installed."
    return 0
  fi

  _info "Updating apt package index..."
  _as_root apt-get update -qq

  if [[ ${#missing_apt[@]} -gt 0 ]]; then
    _info "Installing ${missing_apt[*]}..."
    _as_root apt-get install -y "${missing_apt[@]}"
  fi

  if [[ "$need_gh" == "true" ]]; then
    _info "Installing gh..."
    _as_root apt-get install -y gh
  fi

  if [[ "$need_wslu" == "true" ]]; then
    _info "Installing wslu for browser integration..."
    _as_root apt-get install -y wslu
  fi
}

_install_deps_dnf() {
  local missing=()
  for dep in git gh jq; do
    command -v "$dep" &>/dev/null || missing+=("$dep")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    _info "Installing ${missing[*]}..."
    _as_root dnf install -y "${missing[@]}"
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
  *)
    _error "Unsupported OS: $OS"
    ;;
esac

# --- Install utpr ---
_info "Installing utpr to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR" || _error "Cannot create $INSTALL_DIR. Check permissions or set UTPR_INSTALL_DIR."
_require_cmd mktemp "stage utpr download safely"
tmp_utpr=""
_cleanup_tmp_utpr() {
  if [[ -n "${tmp_utpr:-}" ]]; then
    rm -f "$tmp_utpr"
  fi
}
trap _cleanup_tmp_utpr EXIT INT TERM
tmp_utpr="$(mktemp "$INSTALL_DIR/.utpr.XXXXXX")" || _error "Failed to create temporary file in $INSTALL_DIR"
gum spin --show-error --title "Downloading utpr..." -- \
  curl -fsSL "$UTPR_URL" -o "$tmp_utpr" \
  || _error "Failed to download utpr from $UTPR_URL"
if ! head -1 "$tmp_utpr" | grep -q '^#!/usr/bin/env bash'; then
  _error "Downloaded file doesn't appear to be a valid utpr script. Aborting."
fi
chmod +x "$tmp_utpr"
mv "$tmp_utpr" "$INSTALL_DIR/utpr" || _error "Failed to install utpr to $INSTALL_DIR/utpr"
tmp_utpr=""
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
