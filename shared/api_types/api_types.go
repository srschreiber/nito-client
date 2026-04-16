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
	Name             string `json:"name" validate:"required"`
	UserID           string `json:"userId" validate:"required"`
	EncryptedRoomKey string `json:"encryptedRoomKey" validate:"required"`
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

type GetRoomKeyResponse struct {
	EncryptedRoomKey string `json:"encryptedRoomKey"`
	KeyVersion       int    `json:"keyVersion"`
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
