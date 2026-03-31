#!/usr/bin/env bash
set -euo pipefail

# cowork install script
# Builds the binary and optionally registers the skill with OpenClaw.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${COWORK_INSTALL_DIR:-$HOME/bin}"
BINARY="$INSTALL_DIR/cowork"

echo "==> Building cowork..."

if ! command -v go &>/dev/null; then
  echo "Error: Go is not installed or not in PATH."
  echo "Install Go 1.20+ from https://go.dev/dl/ and try again."
  exit 1
fi

mkdir -p "$INSTALL_DIR"
(cd "$SCRIPT_DIR" && go build -o "$BINARY" .)
echo "    Binary installed to $BINARY"

# Verify
"$BINARY" version

echo ""
echo "==> Done. To register cowork as an OpenClaw skill:"
echo ""
echo "    openclaw skills add $SCRIPT_DIR"
echo ""
echo "Then follow the setup instructions in SKILL.md."
