#!/usr/bin/env bash
# generate-bot-image — build the nito-bot container image from source.
#
# Runs `docker build` with the repo root as build context so the Go build
# can see go.mod / go.sum / the full package tree. Defaults to
# `nito-bot:latest`; pass IMAGE=foo/bar:v1 to override.
set -euo pipefail

IMAGE="${IMAGE:-nito-bot:latest}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "[generate-bot-image] building $IMAGE"
docker build \
  --tag "$IMAGE" \
  --file botcli/Dockerfile \
  .

cat <<MSG

[generate-bot-image] done.

Next: run the bot. First run is interactive (wizard + verify prompt):

  ./scripts/run-bot.sh

Later runs can detach (bot is always-online once bootstrapped):

  IMAGE=$IMAGE DETACH=1 ./scripts/run-bot.sh

Ship to the cloud: docker save $IMAGE | gzip > nito-bot.tar.gz
MSG
