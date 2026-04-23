# nito — Bot accounts

This document is for anyone running (or thinking about running) a nito bot.
The security model, trust boundaries, and wire protocol are all the same as
the desktop client — a bot is just a nito user without a GUI. What's
specific to bots is how they bootstrap, who they accept commands from, and
how they're deployed.

For the broader E2EE architecture see [SECURITY.md](SECURITY.md). The bot
source lives under [`botcli/`](botcli/); the binary is
[`cmd/nito-bot/`](cmd/nito-bot/).

## What a bot is

A bot is a regular user account registered with `isBot: true`. The broker
persists that flag on the user row and exposes it in public-key and
member-list responses, which lets desktop clients render a small badge next
to bot participants. Identity keys (RSA-2048), the TOFU/verified/introduced
trust hierarchy, room-key distribution, message ratchet, and nonce /
timestamp authentication are all unchanged — a bot's ciphertext is
indistinguishable from a human's on the wire.

What's different:

- **Headless.** No voice, no DMs, no image ops, no room creation. The bot
  joins exactly one room.
- **Single owner.** The first user a bot mutual-verifies with becomes its
  operator and is pinned as `Verified`. Only that user can invite the bot
  to a room.
- **Narrow request surface.** Room messages whose plaintext starts with
  `!` are treated as commands. Everything else is ignored.

## Bootstrap flow — resumable

The bot persists its progress to `${NITO_BOT_DATA}/bot-state.yml` (default
`~/.nito-bot/bot-state.yml`) and resumes from the right step on every
launch. Killing the process at any step is safe.

```
fresh        →  registration wizard (broker URL, username, password)
registered   →  prompt for "verify with" username; run A-side handshake
verified     →  poll + listen for invite; accept first from owner
ready        →  serve !-prefixed commands forever
```

On reconnect the bot loops every **10 seconds** until the broker is back.
All verify, invite, and serve steps run over a live session; the reconnect
loop runs in parallel and is idempotent.

## Trust model — owner as root

Because the bot has no way to talk out-of-band to new peers, it can't
verify anyone on its own after bootstrap. Instead:

1. At verify time the bot is initiator **A** of the mutual-verify
   handshake described in SECURITY.md. The operator reads the 6-digit code
   off the bot's stdout and types it into their desktop client.
2. After the handshake completes, the bot pins the operator as
   `Verified` and publishes a signed introduction naming them — so other
   peers who have verified the bot can upgrade their operator pin from
   TOFU to Introduced.
3. **Invite acceptance** is cryptographically verified — the broker's
   word on `inviterUsername` is not trusted on its own. Every
   `PendingInvite` must carry `inviterUsername`, `inviterDeviceId`, and
   `membershipSignature`; the bot resolves the inviter's pubkey via the
   standard web-of-trust hierarchy (Verified > Introduced > TOFU),
   checks that the derived device id matches `inviterDeviceId`, and
   verifies the signature over the canonical membership bytes
   `device_id;invited_username;room_id`. On top of that the bot requires
   the inviter to be its pinned owner (Verified method only) — anything
   else, even a valid signature from an introduced peer, is refused.
   The same verification code path is shared with the desktop UI (via
   `connection.VerifyInvite`) — one implementation, two policies.
4. **Command acceptance** is gated on the sender being either the owner
   or someone the owner has introduced (via a `SignedIntroduction` the
   broker serves at room-join time). TOFU-only peers are refused, as
   are peers the resolver flags as `Contested`.

Concretely: a room member that the owner has not verified cannot make the
bot do anything, even if they're in the same room. This keeps the bot's
effective attack surface to "people the owner trusts".

**Introduction freshness.** Desktop clients only pull introductions at
room-join time because they join often. A bot joins its room once and
stays forever, so it re-pulls the room's introductions every **5 minutes**
via `connection.RefreshIntroductions`. This lets a peer the owner
verifies *after* the bot has joined start using `!hello` within a few
minutes, rather than being silently refused forever.

## Request protocol

Bot requests ride inside the normal `room_message` WebSocket frame — there
is no dedicated RPC type, and the broker cannot distinguish bot traffic
from chat traffic because the `!` prefix lives inside the ciphertext.

**v1 commands**

| Command  | Who can send | Reply |
|----------|--------------|-------|
| `!hello` | owner or owner-introduced peers | `hello, @<sender>` in the room |

Unknown `!`-prefixed commands are silently dropped. Non-`!` messages are
ignored by the bot entirely.

**Rate limit.** Bots are effectively servers, so we rate-limit per sender:
**1 request per 5 seconds**. Excess requests are dropped silently (no
reply, to avoid amplifying an abuse attempt). The limit is a sliding
window, not a token bucket — flooding the gate does not extend a sender's
cooldown past the next legitimate window.

**Reply surface.** Replies are posted in-room using the standard room
ratchet — every member decrypts them. We don't DM replies because DMs
would (a) cost an extra RSA-OAEP envelope per message and (b) hide the
interaction from other room members, which is rarely what you want for a
bot.

**Self-echo.** The broker echoes every `room_message` back to the sender
so mobile clients stay in sync; the bot drops its own echoes at the
dispatch layer to avoid replying to itself.

## Deployment

The bot ships as a distroless Docker image built from source:

```bash
# From the repo root:
./scripts/generate-bot-image.sh   # builds nito-bot:latest (~8 MB image)
./scripts/run-bot.sh              # interactive first run (wizard + verify prompt)

# Once bootstrapped, detach for always-on operation:
DETACH=1 ./scripts/run-bot.sh
```

All state lives under `./nito-bot-data` (mounted at `/data` inside the
container):

- `bot-state.yml` — current step, broker URL, owner username, room id
- `.env` — `NITO_BOT_PASSWORD=...` captured during first-run registration
- `.nito/<broker>/users/<bot>/keys/` — RSA keypair (mode 0600)
- `.nito/<broker>/publickeys/` — pinned peer pubkeys (owner + room members)

To deploy to a cloud runner, `docker save nito-bot:latest | gzip` and
transfer. The image is architecture-matched to the host that built it —
rebuild for the target CPU if you're crossing arm64/amd64.

**The password file is not a secret-management system.** It's an
operational convenience for restarts. If your threat model includes
offline access to the container volume, pass `NITO_BOT_PASSWORD` via your
orchestrator's secret store (Kubernetes Secret, Fly.io secret, etc.) and
delete `.env` after first launch — the bot prefers the process env over
the file.

## What the broker must do to support bots

See the broker repository's CLAUDE.md for the matching implementation
checklist. Summary:

1. Persist `is_bot` on the user record (set at registration, immutable
   thereafter).
2. Include `isBot` on `GetUserPublicKeyResponse` and on each
   `ListRoomMembersResponse.Members[]` entry.
3. On `PendingInvite`, populate **all three**: `inviterUsername`,
   `inviterDeviceId`, and `membershipSignature`. The broker already
   receives the signature + canonical bytes on `InviteUserRequest`; it
   just needs to echo them back, plus the authenticated inviter's
   username and the sha256 DER of their pubkey. None are optional —
   both bots and the desktop UI refuse invites that lack any of these.
4. (Recommended) reject `InviteUser` when the target is a bot and the
   room already contains one. Client-side enforced too, but a
   misbehaving client shouldn't be able to break the one-bot-per-room
   rule.

No new WebSocket message types are needed. Bot requests are plain
`room_message` frames — the broker sees `RoomKeyVersion`, timestamps,
ciphertext, and nothing else, exactly as it does for human chat.

## Security properties summary

- Same threat model as the desktop client (SECURITY.md). A bot does not
  expand what the broker can see — it receives the same encrypted
  payloads as any room member.
- **Compromising a bot compromises one room member, not the broker.** A
  bot's private key lives on the host; treat it like any other server
  secret. A bot machine compromise gives the attacker room-level read +
  write and nothing more.
- One owner per bot, one bot per room, in v1. These are policy choices —
  relaxing them later is a follow-up, not a v1 correctness concern.
- Rate limiting and the trust gate are defense-in-depth, not a primary
  control. The primary control is that only room members can send
  encrypted messages to the room at all.

## Out of scope (for now)

- Multi-owner bots (would require the owner set to be maintained by
  vouching, similar to room-member inclusion).
- Additional `!` commands. The dispatch table in `botcli/serve.go` is
  deliberately a switch statement — add a case and a test, no framework.
- DM-based request protocol. DMs are drained and dropped.
- Voice / image / audio playback. Never, by design.
