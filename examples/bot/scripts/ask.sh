#!/bin/sh
# ask.sh — !ask command. Calls Gemini with:
#   - a system prompt loaded from /scripts/system_prompt.txt (so the
#     model knows it's a nito bot and where to point users for docs)
#   - the requester's prior conversation history (last 10 exchanges)
#     persisted across container restarts at /state/conv-<hash>.json
#
# Per-user history isolation: the file is keyed by sha256(REQUESTER),
# so Alice's chat never bleeds into Bob's. /state is a per-command
# bind mount, so /state/conv-* is also invisible to other commands'
# scripts (!hello can't read what someone asked the LLM).

set -eu

if [ -z "${GEMINI_API_KEY:-}" ]; then
  printf '{"reply":"!ask is not configured (GEMINI_API_KEY missing)"}'
  exit 0
fi
if [ -z "${NITO_ARGS:-}" ]; then
  printf '{"reply":"usage: !ask <question>"}'
  exit 0
fi

state_dir=/state
mkdir -p "$state_dir"

# sha256 the requester so a username with weird chars (or a literal
# `..` someone tried to inject) can't escape /state. 32 hex chars is
# plenty of collision resistance for a per-bot scope.
user_hash=$(printf '%s' "$REQUESTER" | sha256sum | cut -d' ' -f1 | cut -c1-32)
state_file="$state_dir/conv-$user_hash.json"

# Load existing history. Falls back to [] on missing/corrupt file —
# we don't want a single bad write to brick the bot.
if [ -f "$state_file" ]; then
  history=$(jq -c '.' "$state_file" 2>/dev/null || printf '[]')
else
  history='[]'
fi

# Build the request: system_instruction + (history + new user turn).
# We tag every user message with [from <username>] inside the text
# so even if Gemini ignores the system-prompt hint, the model sees
# whose voice each turn belongs to.
sys_prompt=$(cat /scripts/system_prompt.txt)
sys_json=$(printf '%s' "$sys_prompt" | jq -Rs '.')
tagged_q=$(printf '[from %s] %s' "$REQUESTER" "$NITO_ARGS")
question_json=$(printf '%s' "$tagged_q" | jq -Rs '.')

body=$(jq -cn \
  --argjson hist "$history" \
  --argjson q "$question_json" \
  --argjson sys "$sys_json" \
  '{
    system_instruction: {parts: [{text: $sys}]},
    contents: ($hist + [{role: "user", parts: [{text: $q}]}]),
    tools: [{google_search: {}}]
  }')

resp=$(curl -fsS \
  -H "Content-Type: application/json" \
  -X POST \
  -d "$body" \
  "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=${GEMINI_API_KEY}" 2>/dev/null) || {
  # Generic failure — never echo $resp because curl can include the
  # URL (with the API key in the query string) in its output.
  printf '{"reply":"gemini call failed"}'
  exit 0
}

answer=$(printf '%s' "$resp" | jq -r '.candidates[0].content.parts[0].text // empty')
if [ -z "$answer" ]; then
  printf '{"reply":"gemini returned no answer"}'
  exit 0
fi

# Append (user q, model a) and trim to last 10 exchanges = last 20
# entries (alternating user/model). The trim happens BEFORE write so
# the file size stays bounded regardless of how chatty a user is.
answer_json=$(printf '%s' "$answer" | jq -Rs '.')
new_history=$(jq -cn \
  --argjson hist "$history" \
  --argjson q "$question_json" \
  --argjson a "$answer_json" \
  '($hist + [
    {role: "user",  parts: [{text: $q}]},
    {role: "model", parts: [{text: $a}]}
  ]) | (if length > 20 then .[-20:] else . end)')

# Atomic write so a crash mid-write can't leave the file half-written
# and unreadable on the next call.
printf '%s' "$new_history" > "$state_file.tmp"
mv "$state_file.tmp" "$state_file"

printf '%s' "$answer" | jq -Rs '{reply:.}'
