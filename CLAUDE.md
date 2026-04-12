# nito-client — CLAUDE.md

## Repo layout

```
nito-client/
├── shared/            # Wire types shared between client and broker (Go module)
│   ├── api_types/     # HTTP request/response structs
│   ├── websocket_types/ # WebSocket message structs (ToBrokerWsMessage, etc.)
│   └── utils/
├── shellapp/          # Terminal UI application (bubbletea v2)
│   ├── commands/      # Command parsing and execution
│   ├── components/    # UI components (tabs, chat, voice settings, etc.)
│   ├── connection/    # WebSocket connection management
│   ├── keys/          # RSA key management, ChaCha20-Poly1305 encryption, key ratchet
│   ├── voice/         # Voice capture, Opus encoding, signalsmith pitch shift, RNNoise
│   ├── styles/        # Lipgloss style definitions
│   ├── types/         # Shared message types for the bubbletea event bus
│   ├── history/       # Command history persistence
│   └── clientlog/     # Structured in-process logging
├── sounds/            # Embedded audio assets (notification sounds)
└── scripts/           # Build/release helpers
```

## Relationship to nito-broker

The broker lives at `github.com/srschreiber/nito-broker`. Shared wire types
(HTTP request/response structs, WebSocket payload structs) live in this repo
under `shared/`. The broker imports them as a versioned Go module.

**Workflow for any shared type change:**
1. Make the change here and push a new semver tag (e.g. `v0.1.8`).
2. Tell the broker side to run: `go get github.com/srschreiber/nito-client@<new-tag>`

Any time you make a change that affects the broker, explicitly state what the
broker needs to do — the broker's CLAUDE.md uses the format:
> "Client change required (nito-client): \<what changed\> — push as vX.Y.Z"

## Security model

- **E2EE**: Room messages are encrypted with per-room AES-256 keys, stored at
  `~/.nito/users/<username>/`. Keys are ratcheted per-user per-message
  (ChaCha20-Poly1305 with a derived nonce).
- **Authentication**: Login uses RSA-2048 challenge-response. The client signs
  `username:challenge` with its private key; the broker verifies against the
  registered public key.
- **Nonces**: Every outbound WebSocket message (`ToBrokerWsMessage`) carries a
  unique `Nonce` field. `keys.NonceMap` tracks seen inbound nonces to reject replays.
- **Room key distribution**: When inviting a user, the room key is RSA-encrypted
  with the invitee's public key (retrieved from the broker), never the sender's.
- **Trust boundary**: The `shared/` directory is the canonical trust boundary.
  Anything in `shared/` is public protocol. The broker is closed source but
  `shared/` lets users audit the E2EE claims.

## Testing philosophy

**UI correctness is not the priority for automated tests. The priority is
security correctness.** This is a public repo, so source-level crypto bugs are
visible to anyone reading the code — but automated tests provide a runtime
regression net and verify correct behavior under real inputs, which code review
alone cannot guarantee.

### Every security-sensitive code path must have a test:

- **E2E encryption/decryption** — encrypt a message with the room key, decrypt
  it, assert plaintext round-trips correctly. Test that wrong key fails to
  decrypt (or produces wrong output).
- **Login challenge signing** — the client signs `username:challenge` with its
  RSA private key. Test that the signature verifies correctly against the stored
  public key. Test that a tampered challenge produces an invalid sig.
- **Nonce handling** — the client must generate a unique nonce for every
  outbound WebSocket message. Test that two consecutive messages never reuse a
  nonce. If the client tracks seen nonces for inbound messages, test that a
  duplicate inbound nonce is rejected.
- **Timestamp validation** — outbound WS messages must have a `Timestamp`
  within the current unix second. Test that stale timestamps (>30 s old) are
  flagged.
- **Room key encryption** — when inviting a user, the room key must be encrypted
  with the invitee's public key, not the sender's. Test that the encrypted blob
  can be decrypted by the invitee's private key and not by the sender's.
- **Voice packet encryption** — if voice packets are encrypted at the application
  layer, test that raw bytes written to the wire differ from the plaintext, and
  that decryption recovers the original payload.
- **Public key storage** — test that the public key stored/registered with the
  broker matches the private key held locally (sign something, verify with the
  stored public key).
- **fromUsername enforcement** — test that outbound room messages and DMs always
  set `fromUsername` to the authenticated user's username, never to an arbitrary
  string.

### What does NOT need tests:
- UI rendering, layout, component appearance
- Navigation flows (integration/E2E — separate suite)
- Network error handling UX (retry spinners, toast messages, etc.)

> **Key framing**: UI regressions are caught visually; crypto correctness is
> harder to verify through manual testing — tests provide the runtime regression
> net that code review alone cannot.

## Key packages

| Package | Role |
|---|---|
| `shellapp/keys` | RSA key gen/load/sign, ChaCha20-Poly1305 encrypt/decrypt, `RoomKeyChain` ratchet, nonce tracking |
| `shellapp/voice` | Opus capture/encode, signalsmith-stretch pitch shift (MIT CGo), RNNoise VAD, oto audio playback |
| `shellapp/commands` | Command parser, `ExecCommand`, voice/room/messaging RPCs |
| `shellapp/components` | Bubbletea UI components; chat ops (`.play`, `.image`, `.jump`) live in `handleChatOp` |
| `shellapp/connection` | WebSocket lifecycle, `Send`, session state |
| `shared/websocket_types` | Wire types for all WS RPCs (both directions) |

## Chat ops vs commands

- **Commands** (`ExecCommand`): typed at the command prompt (`wcid`, `room-select`, `voice-join`, etc.)
- **Chat ops** (`handleChatOp` in `components/commands.go`): `.`-prefixed inputs
  in chat mode (`.play <url>`, `.image <file>`, `.jump <line>`)

### `.play <url>`

Sends a `play_audio` RPC to the broker, which relays it to all users in the
active voice room. Each recipient streams and plays the MP3 locally via oto.

- Must be in a voice call; returns an error toast if not.
- URL must point to an MP3; capped at 5 MB.
- Playback is cancellable — starting a new clip cancels the previous one.
- **Good source**: [archive.org](https://archive.org) hosts a large public-domain
  audio library with direct `.mp3` links suitable for use with `.play`.

## CMD tab visibility

The CMD tab is hidden by default. Users opt in via `~/.nito/defaults.yml`:
```yaml
show_cmd_tab: true
```
Controlled by `components.ShowCmdTab` (set in `main()` from prefs before `initialModel()`).

## Voice audio pipeline

Capture → RNNoise VAD → Opus encode → WebRTC (pion) → peer decode → Opus decode → oto playback

Pitch/vibrato (signalsmith-stretch, CGo):
- Block size: 2048 samples, interval: 512 (~43 ms latency)
- Vibrato: sine oscillator, 1–8 Hz, ±0.5–3.0 st range
- Configured in `VoiceSettingsScreen`; atomics in `voice/voice.go`

## Build

```bash
cd shellapp && go build .
```

Requires CGo (signalsmith-stretch C++ wrapper, RNNoise, Opus). On Windows,
MinGW must supply `<cstring>` — `signalsmith_wrap.cpp` includes it explicitly.
