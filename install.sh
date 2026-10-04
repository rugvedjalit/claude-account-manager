#!/usr/bin/env bash
# Installs (or removes) the Claude Account Manager binary for the current user on Linux or macOS.
#
#   ./install.sh               install to ~/.local/bin (same place Claude Code's native installer uses)
#   ./install.sh --uninstall   remove the binary (saved accounts are kept)
#   ./install.sh --uninstall --purge-accounts
#
# No root needed. Nothing in the Claude Code installation or config is modified.
set -euo pipefail

BIN_DIR="${CLAUDE_ACCOUNT_BIN:-$HOME/.local/bin}"
REPO="rugvedjalit/claude-account-manager"
# When piped from curl there is no script file, so there is no local dist/ folder either.
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
else
  HERE=""
fi

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) echo "Unsupported OS: $(uname -s). On Windows use install.ps1." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [[ "$os" == darwin ]]; then
  STORE="$HOME/Library/Application Support/claude-account"
else
  STORE="${XDG_CONFIG_HOME:-$HOME/.config}/claude-account"
fi

if [[ "${1:-}" == "--uninstall" ]]; then
  rm -f "$BIN_DIR/claude-account" && echo "removed $BIN_DIR/claude-account"
  if [[ "${2:-}" == "--purge-accounts" ]]; then
    if [[ "$os" == darwin ]]; then
      for i in 1 2 3 4 5 6 7 8 9; do security delete-generic-password -a "slot-$i" -s claude-account-manager >/dev/null 2>&1 || true; done
    elif command -v secret-tool >/dev/null 2>&1; then
      for i in 1 2 3 4 5 6 7 8 9; do secret-tool clear app claude-account-manager slot "$i" >/dev/null 2>&1 || true; done
    fi
    rm -rf "$STORE" && echo "deleted $STORE"
  else
    echo "saved accounts kept in: $STORE (re-run with --uninstall --purge-accounts to delete them)"
  fi
  echo "Done. Claude Code itself and its current login are untouched."
  exit 0
fi

echo "Installing Claude Account Manager ($os-$arch)..."

SRC="$HERE/dist/$os-$arch/claude-account"
if [[ -z "$HERE" || ! -f "$SRC" ]]; then
  # No local build: download the binary for this platform from the latest GitHub release.
  url="https://github.com/$REPO/releases/latest/download/claude-account-$os-$arch"
  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' EXIT
  echo "  downloading $url"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmp" || { echo "Download failed. Check your connection or build from source (./build.sh)." >&2; exit 1; }
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$tmp" || { echo "Download failed. Check your connection or build from source (./build.sh)." >&2; exit 1; }
  else
    echo "Need curl or wget to download the binary (or build from source with ./build.sh)." >&2
    exit 1
  fi
  SRC="$tmp"
fi
if command -v claude >/dev/null 2>&1; then
  echo "  Claude Code: $(claude --version 2>/dev/null | head -1) ($(command -v claude))"
else
  echo "  WARNING: 'claude' not found on PATH. Install Claude Code first: https://code.claude.com/docs/en/setup" >&2
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$SRC" "$BIN_DIR/claude-account"
if [[ "$os" == darwin ]]; then
  xattr -d com.apple.quarantine "$BIN_DIR/claude-account" 2>/dev/null || true
fi
echo "  installed $BIN_DIR/claude-account"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo
    echo "  $BIN_DIR is not on your PATH. Add this line to ~/.bashrc or ~/.zshrc:"
    echo "      export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac

echo
echo "Installed. Next:"
echo "    claude-account setup      # one-time: save Account 1, add Account 2"
echo "    claude-account status"
echo "    claude-account switch"
