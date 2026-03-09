#!/bin/bash
# Install script for utpr
# Usage: curl -fsSL https://raw.githubusercontent.com/gadenbuie/utpr/main/scripts/install.sh | bash
set -e

REPO="gadenbuie/utpr"

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    CYGWIN*|MINGW*|MSYS*) echo "windows" ;;
    *) echo "unsupported" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "unsupported" ;;
  esac
}

get_latest_version() {
  if command -v gh >/dev/null 2>&1; then
    gh release view --repo "${REPO}" --json tagName --jq '.tagName' 2>/dev/null && return
  fi
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'
}

find_install_dir() {
  if [ -w /usr/local/bin ]; then
    echo "/usr/local/bin"
  elif [ -d "${HOME}/.local/bin" ] || mkdir -p "${HOME}/.local/bin" 2>/dev/null; then
    echo "${HOME}/.local/bin"
  else
    return 1
  fi
}

install_from_release() {
  local os="$1" arch="$2" install_dir="$3" version="$4"

  local archive="utpr-${os}-${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/${version}/${archive}"

  echo "Downloading utpr ${version} for ${os}/${arch}..."

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' EXIT

  if ! curl -fsSL -o "${tmpdir}/${archive}" "${url}"; then
    return 1
  fi

  tar -xzf "${tmpdir}/${archive}" -C "${tmpdir}"

  local ext=""
  if [ "${os}" = "windows" ]; then
    ext=".exe"
  fi

  install -m 755 "${tmpdir}/utpr-${os}-${arch}/utpr${ext}" "${install_dir}/utpr${ext}"
  echo "Installed utpr to ${install_dir}/utpr${ext}"
}

install_from_go() {
  if ! command -v go >/dev/null 2>&1; then
    return 1
  fi
  echo "Installing utpr via go install..."
  go install "github.com/${REPO}@latest"
  echo "Installed utpr to $(go env GOPATH)/bin/utpr"
}

main() {
  local os arch install_dir version

  os="$(detect_os)"
  arch="$(detect_arch)"

  if [ "${os}" = "unsupported" ]; then
    echo "Error: unsupported operating system: $(uname -s)" >&2
    exit 1
  fi
  if [ "${arch}" = "unsupported" ]; then
    echo "Error: unsupported architecture: $(uname -m)" >&2
    exit 1
  fi

  install_dir="$(find_install_dir)" || {
    echo "Error: could not find a writable install directory" >&2
    echo "Try: sudo mkdir -p /usr/local/bin && sudo chown \$(whoami) /usr/local/bin" >&2
    exit 1
  }

  version="$(get_latest_version)" || {
    echo "Error: could not determine latest version" >&2
    exit 1
  }

  if install_from_release "${os}" "${arch}" "${install_dir}" "${version}"; then
    :
  elif install_from_go; then
    :
  else
    echo "Error: installation failed" >&2
    echo "Install Go (https://go.dev) and run: go install github.com/${REPO}@latest" >&2
    exit 1
  fi

  # Check if install directory is in PATH
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *)
      echo ""
      echo "Note: ${install_dir} is not in your PATH."
      echo "Add it to your shell profile:"
      echo "  echo 'export PATH=\"${install_dir}:\$PATH\"' >> ~/.zshrc"
      ;;
  esac
}

main
