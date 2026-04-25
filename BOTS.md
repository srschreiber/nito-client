# nito — Bot accounts

This document is for anyone running (or thinking about running) a nito bot.
The security model, trust boundaries, and wire protocol are all the same as
the desktop client — a bot is just a nito user without a GUI. What's
specific to bots is how they bootstrap, who they accept commands from, and
how their commands are defined.

For the broader E2EE architecture see [SECURITY.md](SECURITY.md). The bot
source lives under [`botcli/`](botcli/); the binary entry point is
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

- **Headless.** No voice, no DMs, no image ops, no room creation.
- **Single owner.** The first user a bot mutual-verifies with becomes its
  operator and is pinned as `Verified`. Only that user can invite the bot
  to rooms.
- **Multi-room.** A single bot serves any number of rooms. The owner just
  invites it; the bot accepts every owner-issued invite and joins the room.
- **Narrow request surface.** Room messages whose plaintext starts with
  `!` are matched against the configured command table. Everything else
  is ignored.

## Running

The bot is a single Go binary, distributed for Linux / macOS / Windows.

**Prerequisite: Docker.** The bot shells out to the `docker` CLI to run
your scripts inside an isolated worker container. Install Docker Desktop
(macOS/Windows) or your distro's docker package before first launch; the
bot verifies the daemon is reachable and aborts with a helpful error if
not. Docker is *not* bundled into the nito-bot binary — standard operator
setup, keeps the binary small.

Install with the helper script (mirrors the desktop installer):

```bash
curl -fsSL https://raw.githubusercontent.com/srschreiber/nito-client/main/scripts/install-bot.sh | sh
```

Then run with a config file and a script source directory:

```bash
nito-bot -f bot.yml -s ./scripts
```

The first launch walks through registration (broker URL, username,
password) and a verify handshake against your operator account. Subsequent
launches resume from the persisted state at `~/.nito-bot/bot-state.yml`
and skip straight to the serve loop. A Docker image is also available for
deployment-flavored installs (see [Deployment](#deployment) below).

### From a local checkout

If you've cloned the repo and want to iterate on the bot itself (or just
kick the tyres before installing), the Makefile has a source-run target
that builds the example worker image and compiles nito-bot on the fly:

```bash
make run-bot
```

This launches [`examples/bot/`](examples/bot/) — a minimal alpine worker
image plus a few example commands. State lands in `./nito-bot-data/` so
the example doesn't touch `~/.nito-bot`. Use it as the starting point
for your own bot.

For the production shape — bot binary itself wrapped in the distroless
container — use `make run-bot-docker` (interactive bootstrap) or
`make run-bot-docker-daemon` (detached, `--restart=unless-stopped`).
Both use the *same* per-command worker-container model for script
isolation; the only thing that differs is whether the bot process runs
on the host or inside its own container.

## Command config — `bot.yml`

Every command the bot serves is a shell script declared in `bot.yml`.
The `defaults:` block sets values that propagate to every command;
individual commands can override `image`, `network`, `rate_limit_rps`,
or `timeout_ms` to suit themselves.

```yaml
defaults:
  image: my-nito-worker:latest   # default sandbox image for every command
  network: false                 # all commands offline by default
  rate_limit_rps: 1              # 1 request per second per sender per command
  timeout_ms: 1000               # script killed after 1s

commands:
  hello:
    script: hello.sh
    usage: "!hello"

  ask:
    script: ask.sh
    usage: "!ask <message>"
    args_regex: "^(.+)$"
    arg_names: [message]
    image: python:3.12-slim      # this command needs python; overrides default
    network: true                # this command needs internet; overrides default
    env: [OPENAI_API_KEY]        # routed ONLY into this command's container
    rate_limit_rps: 0.1
    timeout_ms: 30000
```

### Defaults block

| Field              | Default | Description                                                                                              |
|--------------------|---------|----------------------------------------------------------------------------------------------------------|
| `defaults.image`   | (none)  | Default Docker image for command containers. Commands without their own `image:` use this.              |
| `defaults.network` | `false` | Default network policy. `true` lets the container reach the internet; `false` adds `--network none`.    |
| `defaults.rate_limit_rps` | `1`  | Default per-sender rate limit. Per-command override available.                                          |
| `defaults.timeout_ms`     | `5000` | Default hard-kill timeout. Per-command override available; capped at 60 000.                            |

The image is **not** built by nito-bot — you build it once with
`docker build -t <tag> .`, install whatever runtimes your scripts need
(curl, jq, python, node, ...), and reference the tag here. nito-bot only
launches containers from the existing image.

If neither `defaults.image` nor a command's `image:` is set, that command
runs unsandboxed (see [When no image is configured](#when-no-image-is-configured)
below).

Field reference (per command):

| Field            | Required | Description                                                                                  |
|------------------|----------|----------------------------------------------------------------------------------------------|
| `script`         | yes      | Relative path under `-s` source dir; must end in `.sh`; no `..`; no absolute paths.          |
| `usage`          | no       | Human-readable syntax. Posted in-room when args don't match `args_regex`.                    |
| `args_regex`     | no       | Go (RE2) pattern matched against everything after the command token. Capture groups are required if `arg_names` is set. |
| `arg_names`      | no       | Per-capture-group names; zips with regex captures into `NITO_ARG_<UPPERCASE_NAME>` env vars. |
| `image`          | no       | Override `defaults.image` for just this command (e.g. `python:3.12-slim`).                   |
| `network`        | no       | Override `defaults.network` for just this command. `true` = internet on, `false` = `--network none`. |
| `rate_limit_rps` | no       | Override `defaults.rate_limit_rps`.                                                          |
| `timeout_ms`     | no       | Override `defaults.timeout_ms`. Capped at 60 000.                                            |
| `env`            | no       | Allow-list of host env-var names to pass through to **only this command's** worker container. Reserved names (`NITO_*`, `REQUESTER`, `PATH`) are refused at load time. Per-command only — there is no `defaults.env`. |

### Sandbox model

When `worker.image` is set, the bot starts **one container per command**
(lazily, on first invocation of that command) from `worker.image` and
`docker exec`'s into it. Per-command isolation is what makes the
per-command `env:` allow-list actually safe — a host secret routed into
`!ask`'s container cannot be read by `!hello`'s scripts because they
run in different containers. Lifecycle (per command):

1. `nito-bot` boots and validates the image exists locally
   (`docker image inspect <image>`). It does NOT start any containers
   yet — those come up lazily on first use of each command.
2. First invocation of `!<cmd>` starts a dedicated worker for that
   command with a locked-down profile and the command's `env:` allow-
   list baked in:
   ```
   docker run -d --rm --name nito-worker-<cmd>-<rand> \
     -v <abs-source-dir>:/scripts:ro \
     -w /scripts \
     --read-only \                                  # root fs RO; only /tmp writable
     --tmpfs /tmp:rw,nosuid,nodev,size=64m,mode=1777 \
     --cap-drop ALL \                               # no Linux capabilities
     --security-opt no-new-privileges \             # setuid binaries can't escalate
     --pids-limit 256 \                             # forkbomb cap
     [--network none] \                             # if worker.network: false
     [-e MOTTO=$MOTTO -e API_KEY=$API_KEY] \        # one -e per name in `env:`
     --entrypoint tail \
     <image> -f /dev/null
   ```
3. Each subsequent invocation of that same command exec's into its
   running worker (per-call NITO_* vars are passed via `-e`):
   ```
   docker exec -i -e NITO_COMMAND=... -e NITO_ARGS=... ... \
     <worker-for-cmd> sh /scripts/<command-script>
   ```
4. On `nito-bot` shutdown (SIGINT / SIGTERM), every per-command worker
   container is removed (`docker rm -f`).

What the worker can see:

- **/scripts** — the `-s` source dir, **read-only**. Scripts cannot write
  to the host source.
- **/tmp** — a 64 MB tmpfs (`nosuid`, `nodev`). Scratch space for scripts
  that need to drop a temp file; wiped on container removal.
- The container's own filesystem (whatever you packaged in the image),
  but root is mounted read-only — scripts can read `/usr/bin/python3`
  but cannot rewrite it.
- Network — outbound only, gated by `worker.network`.

What the worker **cannot** see:

- The bot's data dir (`~/.nito-bot/` or `/data`). The RSA private key,
  the password `.env`, and the bot-state file are not bind-mounted.
- The bot process's environment. Only the `NITO_*` and `REQUESTER` vars
  plus a baseline `PATH` are passed through `docker exec -e`.
- Other containers, the docker socket, host devices.

What the worker **cannot do**:

- Write to its root filesystem (image is read-only; only `/tmp` is
  writable, capped at 64 MB).
- Hold any Linux capability (`--cap-drop ALL`) — no raw sockets, no
  ptrace, nothing privileged.
- Escalate via setuid binaries (`--security-opt no-new-privileges`).
- Fork past 256 processes (forkbomb cap).

That's the isolation guarantee: a malicious or buggy script can do
anything inside its container, but it cannot extract the bot's identity
keys or impersonate the bot to the broker.

If a script times out, the worker is force-recycled before the next
command — a runaway script can't hold the worker hostage.

### When no image is configured

If any command resolves to no image (no `image:` on the command and no
`defaults.image`), `nito-bot` prints a loud security warning naming the
affected commands and prompts the operator for an explicit `y/N`
confirmation before starting. A non-`y` answer — or any detached run
without a TTY (e.g. `docker run -d`, CI) — aborts:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  SECURITY WARNING: some commands have no image configured

  Unsandboxed commands: hello, motto

  These scripts will run in the bot's own process namespace.
  They CAN read the bot's RSA private key, .env password, and
  bot-state.yml. ...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

continue without sandbox? [y/N]:
```

This hard-fails any accidental detached deployment that forgot to set
an image, while still letting a developer opt in from an attached
terminal for quick local iteration.

### Path safety

Two rules, enforced at config load (so a bad config fails loud at startup,
not on first message):

1. Script paths must be relative under `-s`. `..` segments and absolute
   paths are refused. The source directory is the bot's sandbox.
2. Only `.sh` entry points are allowed. The script is free to call
   python, node, cargo, whatever — that's a script-level decision the
   bot doesn't audit.

### Args parser

Go's `regexp` package (RE2 syntax) is what's used. Use plain `(...)`
captures, not Python-style `(?P<name>...)` — the binding from capture to
env var name comes from `arg_names`, not the regex itself.

`args_regex` runs against everything after the command token (whitespace
trimmed). On mismatch the bot replies with `usage` (or a generic
`usage: !cmd` if no `usage` was set) and does not run the script.

If `args_regex` is omitted, every message that begins with the command
token is accepted; the script just receives `NITO_ARGS` and decides
what to do with it.

### Script protocol

The bot exec's `sh <script>` (inside the worker container, if configured)
with **a sandboxed env** — the host's environment is *not* inherited, so
scripts cannot read `NITO_BOT_PASSWORD` or any other host secret. Only
these variables are passed:

| Variable             | Value                                                              |
|----------------------|--------------------------------------------------------------------|
| `PATH`               | Standard system path so shebangs and `command -v` work.            |
| `NITO_COMMAND`       | The matched command name (e.g. `ask`).                             |
| `NITO_ARGS`          | Raw text after the command token, whitespace-trimmed.              |
| `NITO_ARG_<NAME>`    | One per `arg_names` entry, populated from the regex capture group. |
| `REQUESTER`          | The username of the in-room sender.                                |

The script must print exactly one JSON object on stdout:

```json
{"reply": "anything you want said in the room"}
```

An empty `reply` (or empty stdout) means "no reply" — useful for fire-
and-forget side effects. Non-JSON output is logged as an error and
produces no reply. Stderr is captured into the bot's log on script
failure for debugging; it never reaches the room.

#### Example: `!ask why is the sky blue` with the config above

```
NITO_COMMAND=ask
NITO_ARGS=why is the sky blue
NITO_ARG_MESSAGE=why is the sky blue
REQUESTER=alice
```

`ask.sh` might shell out to `curl ... | jq` to call OpenAI and emit
`{"reply": "<answer>"}` on stdout.

## Bootstrap flow — resumable

The bot persists its progress to `${NITO_BOT_DATA}/bot-state.yml` (default
`~/.nito-bot/bot-state.yml`) and resumes from the right step on every
launch. Killing the process at any step is safe.

```
fresh        →  registration wizard (broker URL, username, password)
registered   →  prompt for "verify with" username; run A-side handshake
verified     →  wait for first invite; on accept transition to ready
ready        →  serve forever; accept further invites in the background
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
   are peers the resolver flags as `Contested`. An introduction whose
   introducer is anyone other than the owner does not count — the bot's
   trust root is its single owner, not the wider web-of-trust.

Concretely: a room member that the owner has not verified cannot make the
bot do anything, even if they're in the same room.

**Introduction freshness.** Desktop clients only pull introductions at
room-join time because they join often. A bot stays in its rooms
indefinitely, so it re-pulls each room's introductions every **5 minutes**
via `connection.RefreshIntroductions`. This lets a peer the owner
verifies *after* the bot has joined start using bot commands within a few
minutes, rather than being silently refused forever.

## Request protocol

Bot requests ride inside the normal `room_message` WebSocket frame — there
is no dedicated RPC type, and the broker cannot distinguish bot traffic
from chat traffic because the `!` prefix lives inside the ciphertext.

**Dispatch rules**

- A message that doesn't start with `!` is ignored.
- The first whitespace-delimited token (minus the `!`) is matched against
  the `commands:` table. Unknown commands are silently dropped.
- If `args_regex` is configured, the rest of the message must match — on
  mismatch the bot replies with `usage`.
- Otherwise the script is exec'd with the env vars listed above; its
  `{"reply": "..."}` is posted in-room.

**Rate limit.** Per command, per sender, configurable via `rate_limit_rps`.
Default 1 rps. When a sender hits the limit the bot replies "hold your
horses" once per denied request and skips the script.

**Reply surface.** Replies are posted in-room using the standard room
ratchet — every member decrypts them. We don't DM replies because DMs
would (a) cost an extra RSA-OAEP envelope per message and (b) hide the
interaction from other room members, which is rarely what you want for a
bot.

**Self-echo.** The broker echoes every `room_message` back to the sender
so other clients stay in sync; the bot drops its own echoes at the
dispatch layer to avoid replying to itself.

## Deployment

The bot also ships as a distroless Docker image:

```bash
# From the repo root:
./scripts/generate-bot-image.sh   # builds nito-bot:latest (~8 MB image)
./scripts/run-bot.sh              # interactive first run (wizard + verify prompt)

# Once bootstrapped, detach for always-on operation:
DETACH=1 ./scripts/run-bot.sh
```

When running in Docker, mount your scripts into the container and pass
the same `-f`/`-s` flags via a wrapper or by overriding `CMD`. All bot
state (keys, .env, bot-state.yml) lives under `./nito-bot-data` (mounted
at `/data`).

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
  write across every joined room and nothing more.
- **Scripts are sandboxed in a worker container** (when `worker.image` is
  set). The worker has no view of the bot's keys, .env, or state file —
  only the read-only source dir and whatever the operator baked into the
  image. A malicious script can hijack its own command's reply, but
  cannot impersonate the bot or steal the bot's identity.
- **Env is sandboxed too.** Host vars (including `NITO_BOT_PASSWORD`) are
  never inherited; only the `NITO_*` and `REQUESTER` vars plus `PATH` are
  passed by default. Per-command secrets are opt-in via the `env:`
  allow-list and routed only to that command's container — a key
  configured for `!ask` cannot be read by `!hello`'s script. Script
  authors who shell out to other tools should still treat `NITO_ARGS`
  as untrusted user input and quote/escape accordingly.
- One owner per bot, in v1. Multi-owner support would require maintaining
  the owner set by vouching, similar to room-member inclusion.
- Rate limiting and the trust gate are defense-in-depth, not a primary
  control. The primary control is that only room members can send
  encrypted messages to the room at all.

## Out of scope (for now)

- Multi-owner bots.
- DM-based request protocol. DMs are drained and dropped.
- Voice / image / audio playback. Never, by design.
- Native Windows script execution. The bot runs on Windows but the
  command-script protocol assumes POSIX `sh`; on Windows the binary
  can be used for register/verify, but expect to deploy on Linux/macOS
  for the serve loop.
