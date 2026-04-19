// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package api_types

// User

type RegisterRequest struct {
	Username   string `json:"username" validate:"required"`
	PublicKey  string `json:"publicKey" validate:"required"`
	Password   string `json:"password" validate:"required"`
	DeviceName string `json:"deviceName" validate:"required"`
}

type RegisterResponse struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	AlreadyRegistered bool   `json:"alreadyRegistered,omitempty"`
	DeviceID          string `json:"deviceId"`
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
}

type CreateRoomResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoomEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsOwner bool   `json:"isOwner"`
}

type ListRoomsResponse struct {
	Rooms []RoomEntry `json:"rooms"`
}

type InviteUserRequest struct {
	RoomID           string `json:"roomId" validate:"required"`
	InvitedUsername  string `json:"invitedUsername" validate:"required"`
	EncryptedRoomKey string `json:"encryptedRoomKey" validate:"required"`
}

type InviteUserResponse struct {
	RoomID string `json:"roomId"`
	UserID string `json:"userId"`
}

type RoomMemberEntry struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Online   bool   `json:"online"`
}

type ListRoomMembersResponse struct {
	Members []RoomMemberEntry `json:"members"`
}

type PendingInvite struct {
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
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
	CurKeyHash     string  `json:"cur_key_hash"`        // hex sha256 of the raw (unencrypted) current key
	CurVersionNum  int     `json:"cur_version_num"`     // monotonically increasing version
	DeviceID       *string `json:"device_id,omitempty"` // which device of rotated_by signed this; nil means the user's root device
	Nonce          string  `json:"nonce"`               // random, base64 — prevents replay
	PrevKeyHash    string  `json:"prev_key_hash"`       // empty on first key
	PrevVersionNum int     `json:"prev_version_num"`    // 0 on first key
	RotatedBy      string  `json:"rotated_by"`          // username who created/rotated this key
	Timestamp      int64   `json:"timestamp"`           // unix seconds — broker checks freshness
}

type GetRoomKeyResponse struct {
	EncryptedRoomKey         string          `json:"encryptedRoomKey"`
	KeyVersion               int             `json:"keyVersion"`
	VersionManifest          RoomKeyManifest `json:"versionManifest"`
	VersionManifestSignature string          `json:"versionManifestSignature"`
}

type GetUserPublicKeyResponse struct {
	PublicKey string `json:"publicKey"`
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
