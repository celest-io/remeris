#!/usr/bin/env bash
# SessionStart hook: prepare a Claude Code cloud session. Does nothing on a developer machine.
#
# Makes mise available, selects the "claude" mise environment (mise.claude.toml: gopls and
# friends) and installs the pinned tools.
set -euo pipefail
trap 'echo "setup_remote.sh failed at line $LINENO: $BASH_COMMAND" >&2' ERR

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  echo "Skipping setup: not a remote session"
  exit 0
fi

# Later commands of the session read these.
if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  {
    echo 'export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"'
    echo 'export MISE_ENV="claude"'
  } >> "$CLAUDE_ENV_FILE"
fi
export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"
export MISE_ENV="claude"

if ! command -v mise >/dev/null 2>&1; then
  if ! command -v curl >/dev/null 2>&1 || ! command -v gpg >/dev/null 2>&1; then
    echo "curl and gpg are needed to install mise; mise will not be available" >&2
    exit 0
  fi
  echo "Installing mise"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  # The installer is signed: verify it before running it.
  curl -fsS "https://keys.openpgp.org/vks/v1/by-fingerprint/24853EC9F655CE80B48E6C3A8B81C9D17413A06D" | gpg --import
  curl -fsS https://mise.jdx.dev/install.sh.sig | gpg --decrypt >"$tmp/install.sh"
  sh "$tmp/install.sh"
fi

mise trust
mise install
