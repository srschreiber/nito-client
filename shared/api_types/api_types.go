// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package api_types

// User

type RegisterRequest struct {
	Username   string `json:"username" validate:"required"`
	PublicKey  string `json:"publicKey" validate:"required"`
	Password   string `json:"password" validate:"required"`
	DeviceName string `json:"deviceName" validate:"required"`
	// IsBot marks the account as a bot. The broker persists this on the
	// users row; it cannot be changed after registration (see BOTS.md).
	// Default false — omitted by the standard desktop client.
	IsBot bool `json:"isBot,omitempty"`
}

type RegisterResponse struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	AlreadyRegistered bool   `json:"alreadyRegistered,omitempty"`
	// DeviceID echoes the broker's derivation of the registered pubkey
	// (sha256(DER(pub_key)) hex). Advisory — client ignores it and
	// re-derives locally to avoid trusting broker-provided ids.
	DeviceID string `json:"deviceId"`
}

// Ping

type PingRequest struct {
	Message string `json:"message" validate:"required,max=256"`
}

type PingResponse struct {
	Message string `json:"message"`
}

// Rooms

type CreateRoomRequest struct {
	Name                     string          `json:"name" validate:"required"`
	UserID                   string          `json:"userId" validate:"required"`
	EncryptedRoomKey         string          `json:"encryptedRoomKey" validate:"required"`
	VersionManifest          RoomKeyManifest `json:"versionManifest" validate:"required"`
	VersionManifestSignature string          `json:"versionManifestSignature" validate:"required"`
	// RoomSignature is the creator's attestation over the canonical room
	// bytes "device_id;name;owner" (alphabetical, ';'-joined). The
	// device_id in the canonical form is derivable by the broker from
	// the authed session's pubkey, so it's not transmitted here —
	// duplicating it would just give attackers a field to lie about.
	RoomSignature string `json:"roomSignature" validate:"required"`
}

type CreateRoomResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoomEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsOwner bool   `json:"isOwner"`

	// SignedByUsername / SignedByDeviceID describe the room's
	// *creation* signature (rooms.signature + rooms.signed_by_device_id).
	// This is immutable — set once at room creation — and is what the
	// sidebar trust icon reflects. Both empty for legacy rooms created
	// before row signatures were stored.
	SignedByUsername string `json:"signedByUsername,omitempty"`
	SignedByDeviceID string `json:"signedByDeviceId,omitempty"`

	// RotatorUsername / RotatorDeviceID describe the *current* key
	// version's manifest signer (from the latest room_key_versions row).
	// Distinct from the room signature because it changes on every key
	// rotation. Used by the rotator banner inside the active room.
	RotatorUsername string `json:"rotatorUsername,omitempty"`
	RotatorDeviceID string `json:"rotatorDeviceId,omitempty"`
}

type ListRoomsResponse struct {
	Rooms []RoomEntry `json:"rooms"`
}

type InviteUserRequest struct {
	RoomID           string `json:"roomId" validate:"required"`
	InvitedUsername  string `json:"invitedUsername" validate:"required"`
	EncryptedRoomKey string `json:"encryptedRoomKey" validate:"required"`
	// MembershipSignature is the inviter's attestation over
	// "device_id;invited_username;room_id" (alphabetical, ';'-joined).
	// The device_id in the canonical form is derivable by the broker
	// from the authed session's pubkey, so it's not transmitted here.
	MembershipSignature string `json:"membershipSignature" validate:"required"`
}

type InviteUserResponse struct {
	RoomID string `json:"roomId"`
	UserID string `json:"userId"`
}

type RoomMemberEntry struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Online   bool   `json:"online"`
	IsBot    bool   `json:"isBot,omitempty"`
}

type ListRoomMembersResponse struct {
	Members []RoomMemberEntry `json:"members"`
}

type PendingInvite struct {
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
	// InviterUsername is the user who sent the invite. Required — without
	// it, bots cannot enforce owner-only invite acceptance. The broker
	// takes this from the authenticated session at InviteUser time, so
	// it is not attacker-controlled on the happy path.
	InviterUsername string `json:"inviterUsername"`
	// InviterDeviceID is sha256(DER(inviter public key)), hex. Required —
	// it both (a) names which device signed MembershipSignature and (b)
	// binds that signature to a specific pubkey, blocking broker key
	// substitution in the same way room-key manifests do. The broker
	// already received this on InviteUserRequest's MembershipSignature
	// input (canonical form includes device_id); the broker just needs
	// to echo it back.
	InviterDeviceID string `json:"inviterDeviceId"`
	// MembershipSignature is the inviter's RSA-PSS attestation over the
	// canonical form "device_id;invited_username;room_id" (alphabetical,
	// `;`-joined) — see engine/commands/room.go and SECURITY.md. Required
	// so the invitee can verify cryptographically who invited them,
	// rather than trusting the broker's word on InviterUsername.
	MembershipSignature string `json:"membershipSignature"`
}

type ListPendingInvitesResponse struct {
	Invites []PendingInvite `json:"invites"`
}

type AcceptInviteRequest struct {
	RoomID string `json:"roomId" validate:"required"`
}

// RoomKeyManifest is the signed metadata that accompanies every room-key
// version so members can detect silent broker-side key rotation / substitution.
// Fields are concatenated alphabetically (by JSON key) with `;` separators to
// produce the signed payload.
type RoomKeyManifest struct {
	CurKeyHash     string `json:"cur_key_hash"`     // hex sha256 of the raw (unencrypted) current key
	CurVersionNum  int    `json:"cur_version_num"`  // monotonically increasing version
	DeviceID       string `json:"device_id"`        // signing device id — sha256(DER(rotator's public key)), hex. Required.
	Nonce          string `json:"nonce"`            // random, base64 — prevents replay
	PrevKeyHash    string `json:"prev_key_hash"`    // empty on first key
	PrevVersionNum int    `json:"prev_version_num"` // 0 on first key
	RotatedBy      string `json:"rotated_by"`       // username who created/rotated this key
	Timestamp      int64  `json:"timestamp"`        // unix seconds — broker checks freshness
}

type GetRoomKeyResponse struct {
	EncryptedRoomKey         string          `json:"encryptedRoomKey"`
	KeyVersion               int             `json:"keyVersion"`
	VersionManifest          RoomKeyManifest `json:"versionManifest"`
	VersionManifestSignature string          `json:"versionManifestSignature"`

	// Fields needed to verify the room's creation attestation
	// (rooms.signature over canonical "device_id;name;owner"). The
	// broker has these in its rooms row and returns them here so the
	// client can verify in the same round-trip that fetches the key —
	// no second call to ListRooms to retrieve the same info.
	Name             string `json:"name"`
	RoomSignature    string `json:"roomSignature"`
	SignedByUsername string `json:"signedByUsername"`
	SignedByDeviceID string `json:"signedByDeviceId"`
}

type GetUserPublicKeyResponse struct {
	PublicKey string `json:"publicKey"`
	IsBot     bool   `json:"isBot,omitempty"`
}

type GetRoomInfoResponse struct {
	// SentMessageCount is the total number of messages this user has sent in this room.
	// It is used as a strictly increasing counter for encryption key derivation (ratcheting).
	//
	// TODO: Will also include:
	//   - Historic messages for catch-up on reconnect
	//   - Per-member sent message counts, needed to derive decryption keys via the
	//     key-chain ratchet (each member's count advances their own ratchet state)
	SentMessageCount int `json:"sentMessageCount"`
}

// LoginChallengeRequest presents the username for which the broker responds with a login challenge
type LoginChallengeRequest struct {
	Username string `json:"username" validate:"required"`
}

// LoginChallengeResponse contains the login challenge string and the user's public key PEM
type LoginChallengeResponse struct {
	Challenge string `json:"challenge"`
}

type LoginRequest struct {
	Username  string `json:"username" validate:"required"`
	Password  string `json:"password" validate:"required"`
	Challenge string `json:"challenge" validate:"required"`
	Signature string `json:"signature" validate:"required"` // login:<username>:<challenge>
	DeviceID  string `json:"deviceId,omitempty"`
}

type LoginResponse struct {
	Token    string `json:"token"` // JWT token for authenticating future requests
	DeviceID string `json:"deviceId"`
}

// ── Giphy proxy requests ─────────────────────────────────────────────────────
//
// The broker proxies Giphy API calls (keeps the API key server-side). These
// request types are what clients send; broker forwards to Giphy and returns
// the raw response. Mirrors the Giphy REST API shape.

type GiphySearchRequest struct {
	Query string `json:"q" validate:"required"`
	// Limit max 50, default 20.
	Limit  *int    `json:"limit" validate:"omitempty,gte=1,lte=50"`
	Rating *string `json:"rating" validate:"omitempty,oneof=g pg pg-13 r"`
	Offset *int    `json:"offset" validate:"omitempty,gte=0"`
	// Sticker routes to /v1/stickers/* instead of /v1/gifs/*.
	Sticker bool `json:"sticker"`
}

type GiphyTrendingRequest struct {
	// Limit max 50, default 25.
	Limit  *int    `json:"limit" validate:"omitempty,gte=1,lte=50"`
	Rating *string `json:"rating" validate:"omitempty,oneof=g pg pg-13 r"`
	// Offset max 499, default 0.
	Offset *int `json:"offset" validate:"omitempty,gte=0,lte=499"`
	// Sticker routes to /v1/stickers/* instead of /v1/gifs/*.
	Sticker bool `json:"sticker"`
}

type GiphyTranslateRequest struct {
	Phrase string  `json:"s" validate:"required"`
	Rating *string `json:"rating" validate:"omitempty,oneof=g pg pg-13 r"`
	// Sticker routes to /v1/stickers/* instead of /v1/gifs/*.
	Sticker bool `json:"sticker"`
}

type GiphyRandomRequest struct {
	Tag    *string `json:"tag" validate:"omitempty"`
	Rating *string `json:"rating" validate:"omitempty,oneof=g pg pg-13 r"`
	// Sticker routes to /v1/stickers/* instead of /v1/gifs/*.
	Sticker bool `json:"sticker"`
}

type GiphyEmojiRequest struct {
	// Limit max 50, default 25.
	Limit *int `json:"limit" validate:"omitempty,gte=1,lte=50"`
	// Offset default 0.
	Offset *int `json:"offset" validate:"omitempty,gte=0"`
}

// SignedIntroductionRequest
// An introduction vouching for a verified users identity, specifically their public key
type SignedIntroductionRequest struct {
	SignedIntroduction
}

type SignedIntroduction struct {
	Username           string `json:"username"`
	PublicKey          string `json:"publicKey"`
	VerifiedByUsername string `json:"verifiedByUsername"`
	VerifiedByDeviceID string `json:"verifiedByDeviceID"`
	VerifiedSignature  string `json:"verifiedSignature"`
}

// ListSignedIntroductionsFromVerifiedUsersResponse
// Returns a list of signed introductions from users the currect user has verified.
// This allows receiving public keys from trusted sources rather than the broker and setting their verified
// state to introduced rather than unverified, which is a slightly stronger form of trust. Each signature must
// be verified and all discrepancies must be made known to the user.
type ListSignedIntroductionsFromVerifiedUsersResponse struct {
	// RoomIDFilter to just pull in the introductions for a specific room.
	RoomIDFilter  *string              `json:"roomIDFilter,omitempty"`
	Introductions []SignedIntroduction `json:"introductions"`
}
