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

## Testing

Security-sensitive paths have unit tests under
[`engine/keys/crypto_test.go`](engine/keys/crypto_test.go) and
[`engine/sounds/crypto_test.go`](engine/sounds/crypto_test.go). These are
gated on every release build via the `test` job in
[`.github/workflows/release.yml`](.github/workflows/release.yml) — a failing
crypto test blocks the release.

The test philosophy is documented in [CLAUDE.md](CLAUDE.md#testing-philosophy):
UI correctness is verified visually, crypto correctness is verified in code.
