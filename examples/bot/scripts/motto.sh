#!/bin/sh
# motto.sh — example using a per-command env var.
#
# bot.yml routes the bot's MOTTO env var into this command's container
# via `env: ["MOTTO"]`. Other commands' containers don't get MOTTO, so
# a buggy or hostile script under !echo / !hello cannot read it.

if [ -z "$MOTTO" ]; then
  printf '{"reply":"motto is not configured"}'
  exit 0
fi
printf '{"reply":"motto: %s"}' "$MOTTO"
