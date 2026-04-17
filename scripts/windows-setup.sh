#!/usr/bin/env bash
# windows-setup.sh — run this once from the MSYS2 UCRT64 shell to set up the
# nito build environment on Windows.
#
# Usage:
#   1. Download and install MSYS2 from https://www.msys2.org
#   2. Open the "MSYS2 UCRT64" shortcut (not MSYS, not MinGW — UCRT64)
#   3. Clone the repo and run this script:
#        git clone https://github.com/srschreiber/nito-client.git
#        cd nito-client
#        bash scripts/windows-setup.sh

set -e

# Verify we're in the right shell.
if [[ "$MSYSTEM" != "UCRT64" ]]; then
  echo "ERROR: This script must be run from the MSYS2 UCRT64 shell."
  echo "       Open the 'MSYS2 UCRT64' shortcut and try again."
  exit 1
fi

echo "==> Updating package database..."
pacman -Sy --noconfirm

echo "==> Installing build dependencies..."
pacman -S --needed --noconfirm \
  mingw-w64-ucrt-x86_64-gcc \
  mingw-w64-ucrt-x86_64-go \
  mingw-w64-ucrt-x86_64-make \
  mingw-w64-ucrt-x86_64-pkgconf \
  mingw-w64-ucrt-x86_64-opus \
  mingw-w64-ucrt-x86_64-rnnoise \
  mingw-w64-ucrt-x86_64-freetype \
  mingw-w64-ucrt-x86_64-mesa

echo "==> Configuring Go environment..."
source /ucrt64/etc/profile.d/go.sh
export GOROOT=/ucrt64/lib/go
export PATH=/ucrt64/bin:$GOROOT/bin:$PATH

go env -w CGO_ENABLED=1
go env -w GOROOT=/ucrt64/lib/go

echo "==> Adding Go environment to ~/.bashrc..."
PROFILE_BLOCK='
# nito: MSYS2 UCRT64 Go environment
source /ucrt64/etc/profile.d/go.sh
export GOROOT=/ucrt64/lib/go
export PATH=/ucrt64/bin:$GOROOT/bin:$PATH
'

if ! grep -q "nito: MSYS2 UCRT64 Go environment" ~/.bashrc 2>/dev/null; then
  echo "$PROFILE_BLOCK" >> ~/.bashrc
  echo "    Added to ~/.bashrc."
else
  echo "    Already present in ~/.bashrc, skipping."
fi

echo ""
echo "Setup complete. Run the app with:"
echo "    make run-ui"
echo ""
echo "If this is a fresh shell, run 'source ~/.bashrc' first."
