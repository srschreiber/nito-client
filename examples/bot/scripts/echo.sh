#!/bin/sh
# echo.sh — repeats whatever args followed `!echo` back into the room.
# Demonstrates NITO_ARGS without an args_regex.

printf '{"reply":"%s said: %s"}' "$REQUESTER" "$NITO_ARGS"
