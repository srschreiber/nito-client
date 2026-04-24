# nito — Security & E2EE Architecture

This document describes how nito keeps your data end-to-end encrypted. The goal
is for a motivated reader to understand exactly what the broker can and cannot
see, what guarantees we do and do not provide, and where to look for more
technical detail online.

The wire protocol lives in [`shared/websocket_types/websocket_types.go`](shared/websocket_types/websocket_types.go).
The crypto helpers live in [`engine/keys/`](engine/keys/). The voice encryption
pipeline lives in [`engine/sounds/voice.go`](engine/sounds/voice.go).

## Threat model

- **Protect against**: the broker, network observers, compromised TLS
  terminators, and anyone who later obtains broker storage. None of these can
  read message plaintext, room keys, or voice audio.
- **Do not protect against**: a compromised client machine, a user who
  voluntarily exports their private key, or an unverified key substitution
  on first contact (see TOFU below).

## Identity keys — RSA-2048

On registration, each user generates an RSA-2048 keypair. The private key stays
on disk at `~/.nito/<broker>/users/<username>/keys/private_key.pem` (mode 0600).
The public key is uploaded to the broker and advertised to peers.

RSA-2048 with OAEP-SHA256 and PSS-SHA256 is the 2020s-era baseline for
asymmetric signing and key transport in protocols that don't need forward
secrecy at the identity-key layer. It is slower but broadly compatible and
widely audited.

Further reading: [NIST SP 800-56B](https://csrc.nist.gov/pubs/sp/800/56/b/r2/final),
[PKCS#1 v2.2 (RFC 8017)](https://datatracker.ietf.org/doc/html/rfc8017).

## Login — challenge-response

The broker hands the client a random challenge string. The client computes
`Sign(sk_user, "username:challenge")` using RSA-PSS-SHA256 and returns it. The
broker verifies against the stored public key. No password is ever sent, and
no reusable token is created — a stolen challenge signature is only valid for
one session.

Further reading: "challenge-response authentication" on Wikipedia;
[RFC 8017 §8.1](https://datatracker.ietf.org/doc/html/rfc8017#section-8.1) for
RSA-PSS.

## Login self-check — broker key consistency

Immediately after every login, the client GETs its own public key from
`/users/public-key?username=<self>` and compares it against the on-disk PEM. If
the broker is serving a different key for our own username — indicating an
account-level substitution attack — the session is torn down before any room or
messaging activity begins.

This check also runs on reconnect. Because device IDs are derived locally as
`sha256(DER(pub_key))` and never accepted from the broker, a key substitution
that slipped past this check would still fail the derivation test on every
manifest and introduction it tried to fabricate.

## Per-user identity verification — TOFU by default, upgradable to mutual

**Trust-on-first-use (TOFU) is the default.** The very first time you interact
with a peer, the broker hands you a public key for that username, and the
client stores it as `method: tofu, verified: false`. If the broker lied to you
on that first fetch, you've pinned an attacker's key and will never know.
Every subsequent message, invite, and DM you encrypt for that "peer" will be
readable by whoever controls the substituted key.

This is the fundamental weakness of TOFU, and it's the same trust model as SSH
host keys. TOFU is convenient but **only as strong as the broker's honesty at
the moment you first saw the peer**.

Trust has three levels, ordered weakest to strongest:

| Level | How acquired | What it means |
|---|---|---|
| `tofu` | First broker fetch | Broker-asserted; no independent corroboration |
| `introduced` | Majority of your verified peers vouch for the same key | Broker substitution would require corrupting multiple verified relationships |
| `verified` | You ran the mutual-verify handshake yourself | Cryptographically bound out-of-band; broker cannot forge |

A peer's trust level controls whether their key rotations are automatically
accepted when joining a room — `tofu` alone causes an `UnverifiedRotatorError`
popup; `introduced` and `verified` are both sufficient.

To upgrade a TOFU-pinned peer to cryptographically verified, nito supports a
**3-message mutual verification handshake**:

```
A generates:  code (6 digits), session_id
A →   out-of-band (voice, in-person): share code with B
A → B  (via broker): challenge { session_id, pk_A, expires_at }
B → A  (via broker): response  { pk_B, Sign(sk_B, H(code | sid | pk_A | pk_B | "B")) }
A verifies B's signature with pk_B
A → B  (via broker): confirm   { Sign(sk_A, H(code | sid | pk_A | pk_B | "A")) }
B verifies A's signature with pk_A
Both sides pin the peer key as method: verified
```

Key properties:

- The 6-digit code is **shared out-of-band only** — never sent through the
  broker. An attacker controlling the broker cannot know it.
- Both `pk_A` and `pk_B` are inside the signed hash. A MITM cannot substitute
  either key without breaking the signatures.
- The `"A"` / `"B"` role tag in the hash prevents signature reflection — B's
  response signature cannot be replayed as A's confirm, even though both
  signatures cover otherwise identical bytes. This is tested in
  `TestResponseSignatureReflectionAttack` and the inverse.
- The session has an `expires_at`; both sides discard stale challenges.
- The flow is symmetric: both A and B end up with the peer pinned as
  `method: verified`, not just one side.

Further reading: [Signal's safety numbers](https://signal.org/blog/safety-number-updates/),
SAS (Short Authentication String) comparison protocols,
["Cryptographic Doom Principle"](https://moxie.org/2011/12/13/the-cryptographic-doom-principle.html).

## Web-of-trust introductions

After both sides complete the mutual-verify handshake, each side publishes a
**signed introduction** to the broker:

```
SignedIntroduction {
  username             // the peer being vouched for
  publicKey            // their public key PEM
  verifiedByUsername   // the signer (me)
  verifiedByDeviceID   // sha256(DER(my pub key)), hex
  verifiedSignature    // see below
}
```

The signed payload uses the same alphabetical key-value attestation format as
manifests:

```
signature = RSA-PSS-SHA256(sk_signer,
  "<targetDeviceID>;<targetUsername>;<signerDeviceID>;<signerUsername>")
```

where `targetDeviceID` is derived from the target's `publicKey` field
(`sha256(DER(pub_key))`), so swapping the PEM breaks the signature.

When a client joins a room it fetches introductions from the broker, filtered
to members of that room. For each introduction:

1. The client checks that the introducer's device id matches the derivation of
   the introducer's locally-pinned public key — a broker swapping keys would
   break this binding.
2. The signature is verified locally against the pinned public key.
3. If the introduction comes from a peer whose pin is `verified`, it is applied
   via `AddIntroduction` — upgrading the target from `tofu` to `introduced` if
   a majority of your verified contacts agree on the same public key.

**Contested introductions.** If two of your verified contacts vouch for
*different* public keys for the same username, the resolution is `contested` and
neither key is automatically elevated. The UI surfaces this as an ambiguous
trust state that the user must resolve manually.

`AddIntroduction` requires that the introducer already has a verified pin
locally — an unverified introducer has no trust weight and is rejected as a
precondition before the signature is even checked.

## Room creation attestation

When a room is created, the creator signs:

```
signature = RSA-PSS-SHA256(sk_creator,
  "<creatorDeviceID>;<roomName>;<creatorUsername>")
```

The broker stores this signature immutably in `rooms.signature`. When any
client joins the room, it:

1. Resolves the creator's public key.
   - If no key is known: blocks the join — a broker claiming a room was signed by someone we've never even TOFU-seen is suspicious.
   - If the creator is `tofu`-only: still verifies the signature against the
     TOFU-pinned key. A failing check means the signature is wrong even on
     the broker's own terms — join is blocked. A passing check is a weaker
     signal (the broker supplied the key, so it could be self-consistent),
     surfaced as a warning rather than full trust.
   - If the creator is `verified` or `introduced`: verifies the signature
     against that independently-sourced key. A mismatch aborts the join.
2. Derives the expected device id from the resolved public key and checks it
   matches what the broker reported.
3. If verification passes, logs it. If the creator is unverifiable, the join
   proceeds with a warning surfaced in the client log.

This closes a broker-level room impersonation vector: a malicious broker
cannot create a fake room under an existing user's name and have clients
silently accept it. The creator's signature is set once at creation and
verified on every subsequent join.

## Room key distribution — RSA-OAEP envelope

Rooms have a random 32-byte AES-256 key generated locally. When you invite a
peer to a room, the client fetches their public key and sends:

```
EncryptedRoomKey = RSA-OAEP-SHA256(pk_peer, room_key)
```

The broker stores and relays this ciphertext; it cannot decrypt it. The invitee
uses their private key to unwrap the room key.

**Invariant**: the room key is never encrypted with the sender's own key, so a
compromised sender cannot later decrypt a stolen envelope meant for someone
else.

Further reading: [RFC 8017 §7](https://datatracker.ietf.org/doc/html/rfc8017#section-7)
for OAEP.

## Room-key manifest — detecting silent key substitution

Encrypting the room key under each member's public key protects against a
passive broker, but a compromised broker could still re-encrypt a *different*
key for every member and sit in the middle. To catch that, each room-key
version is accompanied by a signed manifest:

```
RoomKeyManifest {
  cur_key_hash     // hex sha256 of the raw room key
  cur_version_num  // monotonic, starts at 1
  device_id        // which device of rotated_by signed this — sha256(DER(pub_key)), hex
  nonce            // 16 random bytes, base64
  prev_key_hash    // sha256 of prior key, empty on first version
  prev_version_num // 0 on first version
  rotated_by       // username who created/rotated this version
  timestamp        // unix seconds — broker rejects stale requests
}
signature = RSA-PSS-SHA256(sk_rotator,
  "cur_key_hash;cur_version_num;device_id;nonce;prev_key_hash;prev_version_num;rotated_by;timestamp")
```

The signed payload is the field values concatenated **alphabetically by JSON
key** with `;` separators, so both sides produce identical bytes without
JSON-encoding ambiguity.

Every client fetching a room key:

1. Downloads the manifest + signature alongside their encrypted key copy.
2. Resolves `rotated_by`'s public key. For self, the local on-disk PEM —
   the broker never gets a say about our own identity. For peers, disk
   first (verified or TOFU pin), falling back to the broker's
   `/users/public-key` endpoint and TOFU-pinning the result.
3. **Derives the expected device id** from that public key
   (`sha256(DER(pub_key))`, hex) and rejects if it doesn't match
   `manifest.device_id`. Because every device id on the wire is a pure
   function of a public key, the broker can't swap a signature+key pair
   onto a different pinned device id without the derivation falling out
   of agreement.
4. Verifies the signature. A mismatch aborts the join and surfaces an
   error rather than silently trusting the substituted key.
5. Checks the rotator's *trust level*. If the rotator is only TOFU-pinned
   (not cryptographically verified out-of-band), the client surfaces an
   `UnverifiedRotatorError` to the UI. The UI shows a warning popup: users
   can run the mutual-verify handshake against the rotator first or
   consciously continue at their own risk. The room is not joined until
   that decision is made. See the TOFU / verification section above for
   why this distinction matters.

The `cur_key_hash` lets each member verify the bytes they decrypt match what
the rotator signed; `prev_key_hash` chains successive versions so a broker
can't roll back to an earlier compromised key without the rotator's
cooperation; `nonce` + `timestamp` prevent replay of an old manifest; and
`device_id` scopes the signature to a single device within a user's account,
so a compromised non-root device can't silently rotate keys as the root user.
Because the id is derived (`sha256(DER(pub_key))`) rather than broker-assigned,
the broker has no ambient authority to relabel one device's signature as
another's — any relabelling produces a derivation mismatch and the client
rejects.

**Limits of this defense.** A fully compromised broker can still trick a
client on *first contact* by forging a fresh identity, signing a substituted
manifest with it, and letting TOFU pin the fake key. The manifest closes the
silent-substitution hole only for rotators the client has already verified
or seen before. For strong protection against an adversarial broker, run
the mutual-verify handshake against peers you care about — verified pins
are never overwritten by the TOFU path, so a later substitution attempt
fails even if the broker is cooperating with it.

Further reading: [Merkle / hash chains](https://en.wikipedia.org/wiki/Hash_chain),
the [Double Ratchet "safety numbers"](https://signal.org/blog/safety-number-updates/)
design (similar goal, different mechanism).

## Message encryption — HMAC ratchet + ChaCha20-Poly1305

The room key is never used directly. Instead, every message uses a per-user,
per-count derived key:

```
K_0     = room_key
K_i+1   = HMAC-SHA256(K_i, "username/counter")
```

Each `K_i` is used exactly once as the key to ChaCha20-Poly1305 AEAD. The
per-message nonce is 12 random bytes prepended to the ciphertext.

Why this design:

- **Forward secrecy within a room**: if `K_i` is stolen, messages before `i`
  are safe because the ratchet is one-way (HMAC is preimage-resistant).
- **Authentication**: ChaCha20-Poly1305 is AEAD; any bit flip in the wire
  ciphertext causes decryption to fail. Tested in
  `TestTamperedCiphertextRejected`.
- **Replay protection**: the client tracks seen nonces per user in
  `NonceMap`; reusing a nonce is rejected on the live path (history loads
  intentionally skip this).

Further reading: [RFC 5869](https://datatracker.ietf.org/doc/html/rfc5869)
for HMAC-based extract/expand, [RFC 8439](https://datatracker.ietf.org/doc/html/rfc8439)
for ChaCha20-Poly1305, the Signal "double ratchet" paper for the full forward-
secret protocol nito's ratchet is a simplified variant of.

## Voice encryption — HKDF + AES-256-GCM per RTP frame

Voice is a separate pipeline because it runs over WebRTC RTP, not the
message WebSocket. The room key feeds into HKDF:

```
voice_key = HKDF-SHA256(room_key, info = "sounds", salt = nil, 32 bytes)
```

Each outgoing Opus frame is then:

```
nonce = 12 random bytes
RTP payload = nonce || AES-256-GCM(voice_key, nonce, opus_frame)
```

The broker acts as an SFU (selective forwarding unit) — it sees RTP headers
and routes packets between participants but cannot decrypt the payload. Tested
in `TestFrameRoundTrip`, `TestFrameEncryptNonDeterministic`,
`TestFrameTamperedPayloadRejected`, and `TestFrameWrongKeyRejected`.

Further reading: [RFC 5869](https://datatracker.ietf.org/doc/html/rfc5869)
for HKDF, [NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final) for
GCM.

## Wire-message authentication — nonce + timestamp

Every WebSocket message to the broker carries:

- a unique `Nonce` (fresh per message, checked server-side against replays),
- a `Timestamp` (rejected if stale by more than ~30 seconds),
- the authenticated `UserID` from the session JWT.

This protects against replayed RPCs even if TLS is compromised at the broker
edge.

## What the broker can see

- Usernames, timestamps, message sizes, voice participants, who invites whom.
- Public keys (by design — the broker distributes them).
- Encrypted payloads (it forwards them but cannot decrypt).

## What the broker cannot see

- Message plaintext.
- Room keys.
- Voice audio.
- Private keys.
- The 6-digit verification code exchanged out-of-band.

## Bot accounts

Bots are regular nito users registered with an `is_bot=true` flag —
same identity keys, same E2EE, same trust model, just headless. The
single-owner trust root, per-command rate limit, multi-room behaviour,
and `!`-prefixed command protocol are documented in [BOTS.md](BOTS.md).
Nothing about bots weakens the properties above; they ride on the
existing `room_message` RPC and inherit every guarantee it provides.

**Script isolation.** A bot's commands are user-defined shell scripts
named in `bot.yml`. To prevent a malicious or buggy script from reading
the bot's RSA private key (or its password `.env`), every script runs
inside a Docker worker container started from an operator-supplied image
(`worker.image`). The worker has:

- A read-only root filesystem (`--read-only`).
- A 64 MB tmpfs at `/tmp` for scratch space (`nosuid`, `nodev`).
- The script source dir bind-mounted at `/scripts` **read-only**.
- **No** mount of the bot's data dir — the RSA private key,
  `bot-state.yml`, and `.env` are invisible to the container.
- All Linux capabilities dropped (`--cap-drop ALL`).
- `no-new-privileges` set so setuid binaries cannot escalate.
- A 256-process pid cap.
- Optional network isolation (`worker.network: false` → `--network none`).

A compromised script can hijack its own command's reply or burn its rate
limit, but cannot impersonate the bot to the broker or rotate room keys.
See [BOTS.md §Sandbox model](BOTS.md#sandbox-model) for the full
breakdown.

## Testing

Security-sensitive paths have unit tests under
[`engine/keys/crypto_test.go`](engine/keys/crypto_test.go),
[`engine/keys/introduction_test.go`](engine/keys/introduction_test.go),
[`engine/keys/manifest_test.go`](engine/keys/manifest_test.go),
[`engine/keys/peer_resolve_test.go`](engine/keys/peer_resolve_test.go),
[`engine/keys/device_id_test.go`](engine/keys/device_id_test.go),
[`engine/connection/self_check_test.go`](engine/connection/self_check_test.go), and
[`engine/sounds/crypto_test.go`](engine/sounds/crypto_test.go). These are
gated on every release build via the `test` job in
[`.github/workflows/release.yml`](.github/workflows/release.yml) — a failing
crypto test blocks the release.

The test philosophy is documented in [CLAUDE.md](CLAUDE.md#testing-philosophy):
UI correctness is verified visually, crypto correctness is verified in code.
