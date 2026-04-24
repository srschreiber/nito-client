#!/bin/sh
# hello.sh — example nito-bot command script.
#
# The bot passes these env vars to every script:
#   NITO_COMMAND   matched command name (e.g. "hello")
#   NITO_ARGS      raw text after the command token (may be empty)
#   REQUESTER      the in-room sender's username
#   NITO_ARG_<N>   one per entry in `arg_names` when `args_regex` matches
#
# The script must print one JSON object to stdout: {"reply": "..."}.
# An empty reply means the bot stays silent for this message.

printf '{"reply":"hello, @%s"}' "$REQUESTER"
