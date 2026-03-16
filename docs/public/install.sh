#!/usr/bin/env bash
set -euo pipefail

REPO="hunydev/rc"
INSTALL_DIR="${RC_INSTALL_DIR:-}"
BINARY_NAME="rc"

# ─── Helpers ───

info()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
warn()  { printf '\033[1;33m%s\033[0m\n' "$*"; }
error() { printf '\033[0;31m%s\033[0m\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || error "Required command not found: $1"
}

# ─── Detect platform ───

detect_os() {
  local os
  os="$(uname -s)"
  case "$os" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       error "Unsupported OS: $os" ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *)              error "Unsupported architecture: $arch" ;;
  esac
}

# ─── Resolve install directory ───

resolve_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    echo "$INSTALL_DIR"
    return
  fi

  # Prefer ~/.local/bin if it exists or is in PATH
  local local_bin="$HOME/.local/bin"
  if [ -d "$local_bin" ] || echo "$PATH" | grep -q "$local_bin"; then
    echo "$local_bin"
    return
  fi

  # Fall back to /usr/local/bin (needs sudo)
  echo "/usr/local/bin"
}

# ─── Fetch latest release tag ───

get_latest_version() {
  need curl
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  local tag
  tag="$(curl -fsSL "$url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
  [ -n "$tag" ] || error "Failed to determine latest release"
  echo "$tag"
}

# ─── Download & install ───

download_and_install() {
  local version="$1"
  local os="$2"
  local arch="$3"
  local dir="$4"

  local archive="rc_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/${version}/${archive}"
  local tmp
  tmp="$(mktemp -d)"

  info "Downloading rc ${version} (${os}/${arch})..."
  curl -fsSL -o "${tmp}/${archive}" "$url" || error "Download failed: $url"

  tar xzf "${tmp}/${archive}" -C "$tmp" || error "Extraction failed"
  chmod +x "${tmp}/${BINARY_NAME}"

  # Install to target directory
  mkdir -p "$dir"
  if [ -w "$dir" ]; then
    mv "${tmp}/${BINARY_NAME}" "${dir}/${BINARY_NAME}"
  else
    info "Installing to ${dir} (requires sudo)..."
    sudo mv "${tmp}/${BINARY_NAME}" "${dir}/${BINARY_NAME}"
  fi

  rm -rf "$tmp"
}

# ─── Main ───

main() {
  need curl
  need uname

  local os arch version dir

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(get_latest_version)"
  dir="$(resolve_install_dir)"

  info "  Platform:  ${os}/${arch}"
  info "  Version:   ${version}"
  info "  Directory: ${dir}"
  echo ""

  download_and_install "$version" "$os" "$arch" "$dir"

  # Verify
  if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    info "rc installed successfully!"
    info "Run 'rc' to get started."
  else
    warn "rc was installed to ${dir}/${BINARY_NAME}"
    warn "Make sure ${dir} is in your PATH."
    echo ""
    echo "  export PATH=\"${dir}:\$PATH\""
    echo ""
  fi
}

main "$@"
