#!/usr/bin/env bash
# mac-setup.sh — run this once to set up the nito build environment on macOS.
#
# Prerequisites: Xcode Command Line Tools (xcode-select --install)
#
# Usage:
#   git clone https://github.com/srschreiber/nito-client.git
#   cd nito-client
#   bash scripts/mac-setup.sh

set -e

if [[ "$(uname)" != "Darwin" ]]; then
  echo "ERROR: This script is for macOS only."
  exit 1
fi

# Ensure Homebrew is available.
if ! command -v brew &>/dev/null; then
  echo "ERROR: Homebrew not found. Install it from https://brew.sh and re-run."
  exit 1
fi

echo "==> Updating Homebrew..."
brew update

echo "==> Installing build dependencies..."
brew install go opus rnnoise pkg-config

echo "==> Verifying CGo is enabled..."
go env -w CGO_ENABLED=1

echo ""
echo "Setup complete. Build and run the app with:"
echo "    make run-ui"
echo ""
echo "Or build only:"
echo "    cd ui && go build -o nito ."
