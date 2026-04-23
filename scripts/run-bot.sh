#!/usr/bin/env bash
# run-bot — launch a nito-bot container against the local ./nito-bot-data
# directory. Interactive by default (so the first-run wizard + verify prompt
# can read stdin); pass DETACH=1 to run in the background once bootstrapped.
set -euo pipefail

IMAGE="${IMAGE:-nito-bot:latest}"
DATA_DIR="${DATA_DIR:-$PWD/nito-bot-data}"
NAME="${NAME:-nito-bot}"

mkdir -p "$DATA_DIR"
# distroless/nonroot runs as uid 65532; chown so the bot can write state
# + keys into the mounted volume. Running this every invocation is cheap
# and self-healing if a user rm'd + recreated the dir.
if [ "$(id -u)" = "0" ]; then
  chown -R 65532:65532 "$DATA_DIR"
else
  # Non-root host user can't chown to 65532 on Linux. Loosen perms so
  # the container uid can write; security trade-off is small because
  # this dir is a per-bot secret directory anyway.
  chmod -R u+rwX,g+rwX "$DATA_DIR" || true
fi

# If the container already exists, attach/start it instead of creating a new
# one. This is the normal case after the first run: the wizard has completed,
# keys are on disk, and the user just wants to reconnect.
# Use docker ps rather than docker inspect to avoid spurious stderr on miss.
if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$NAME"; then
  if [ "${DETACH:-0}" = "1" ]; then
    exec docker start "$NAME"
  else
    exec docker start -ai "$NAME"
  fi
fi

# --rm and --restart are mutually exclusive. Interactive runs are one-shot
# (wizard / verify / debug) so --rm fits; detached runs are the always-on
# production mode so --restart fits. Never both.
args=(run --name "$NAME" -v "$DATA_DIR:/data")

if [ "${DETACH:-0}" = "1" ]; then
  args+=(-d --restart=unless-stopped)
else
  args+=(-it --rm)
fi

# Pass through NITO_BOT_PASSWORD if the user set it in their shell — skips
# the wizard password prompt during docker-run.
if [ -n "${NITO_BOT_PASSWORD:-}" ]; then
  args+=(-e "NITO_BOT_PASSWORD=$NITO_BOT_PASSWORD")
fi

args+=("$IMAGE")

exec docker "${args[@]}"
