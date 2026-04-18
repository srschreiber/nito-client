// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package websocket_types

import "encoding/json"

type ToBrokerWsMessage struct {
	RPCName   string          `json:"rpcName" validate:"required"`
	RequestID string          `json:"requestId,omitempty" validate:"required"`
	UserID    string          `json:"userId" validate:"required"`
	Nonce     string          `json:"nonce" validate:"required"`
	Timestamp int64           `json:"timestamp" validate:"required"`
	Payload   json.RawMessage `json:"payload" validate:"required"`
}

type ToClientWsMessage struct {
	RPCName   string          `json:"rpcName" validate:"required"`
	RequestID string          `json:"requestId,omitempty" validate:"required"`
	UserID    string          `json:"userId" validate:"required"`
	Nonce     string          `json:"nonce" validate:"required"`
	Timestamp int64           `json:"timestamp" validate:"required"`
	Payload   json.RawMessage `json:"payload" validate:"required"`
}

const (
	RPCEcho               = "echo"
	RPCKeyVerifyChallenge = "key_verify_challenge" // A → broker → B: initiate verification session
	RPCKeyVerifyResponse  = "key_verify_response"  // B → broker → A: signed proof of key ownership
	RPCRoomMessage        = "room_message"
	RPCNotification       = "notification"
	RPCMembersUpdated     = "members_updated"
	RPCDirectMessage      = "direct_message"
	PlayAudio             = "play_audio"

	// Room presence RPCs.
	RPCRoomEnter = "room_enter" // client → broker: user is now viewing this room
	RPCRoomLeave = "room_leave" // client → broker: user navigated away from the room

	// Voice signaling RPCs.
	RPCVoiceJoin             = "voice_join"               // client → broker: join sounds call, carries SDP offer
	RPCVoiceAnswer           = "voice_answer"             // broker → client: SDP answer for initial join
	RPCVoiceLeave            = "voice_leave"              // client → broker: leave sounds call
	RPCVoiceOffer            = "voice_offer"              // broker → client: renegotiation offer (new participant joined)
	RPCVoiceRenegAnswer      = "voice_reneg_answer"       // client → broker: answer to broker renegotiation offer
	RPCVoiceICERestart       = "voice_ice_restart"        // client → broker: ICE restart offer
	RPCVoiceICERestartAnswer = "voice_ice_restart_answer" // broker → client: ICE restart answer
)

var VoiceChatWithSelf = "self"

type RoomEnterPayload struct {
	RoomID string `json:"roomId"`
}

type RoomLeavePayload struct {
	RoomID string `json:"roomId"`
}

type VoiceJoinPayload struct {
	RoomID   string `json:"roomId"` // special value of "self" to relay sounds to self
	SDPOffer string `json:"sdpOffer"`
}

type VoiceAnswerPayload struct {
	RoomID    string `json:"roomId"`
	SDPAnswer string `json:"sdpAnswer"`
}

type VoiceLeavePayload struct {
	RoomID string `json:"roomId"`
}

type VoiceOfferPayload struct {
	RoomID   string `json:"roomId"`
	SDPOffer string `json:"sdpOffer"`
}

type VoiceRenegAnswerPayload struct {
	RoomID    string `json:"roomId"`
	SDPAnswer string `json:"sdpAnswer"`
}

type VoiceICERestartPayload struct {
	RoomID   string `json:"roomId"`
	SDPOffer string `json:"sdpOffer"`
}

type VoiceICERestartAnswerPayload struct {
	RoomID    string `json:"roomId"`
	SDPAnswer string `json:"sdpAnswer"`
}

const EchoMaxChars = 1024

type EchoPayload struct {
	Text string `json:"text"`
}

//create table if not exists room_message (
//room_id UUID REFERENCES rooms(id) ON DELETE CASCADE,
//key_version_num INTEGER NOT NULL,
//sender_message_count INTEGER NOT NULL DEFAULT 0, -- this is given by the client
//sender_user_id UUID NOT NULL, -- no foreign key because if user is deleted, we still want to keep the message
//encrypted_text TEXT NOT NULL,
//created_at TIMESTAMPTZ DEFAULT now(),
//updated_at TIMESTAMPTZ DEFAULT now(),
//PRIMARY KEY (room_id, sender_user_id, key_version_num, sender_message_count)
//);

const (
	MessageTypeText  = "text"
	MessageTypeImage = "image"
)

type RoomMessagePayload struct {
	RoomID             string `json:"roomId" validate:"required"`
	RoomKeyVersion     int    `json:"roomKeyVersion" validate:"required" description:"the version, or epoch, of the room key used to encrypt this message"`
	SenderMessageCount int    `json:"senderMessageCount" validate:"required" description:"a strictly increasing count of messages sent by this user in this room for encryption key generation purposes."`
	FromUsername       string `json:"fromUsername" validate:"required"`
	EncryptedText      string `json:"encryptedText" validate:"required"`
	MessageType        string `json:"messageType,omitempty"` // "text" (default) or "image"
}

// NotificationType identifies what kind of notification the broker is sending.
type NotificationType string

const (
	NotificationTypeSoundClip           NotificationType = "notification_type_sound_clip"
	NotificationTypeUserJoinedRoom      NotificationType = "user_joined_room"
	NotificationTypeUserLeftRoom        NotificationType = "user_left_room"
	NotificationTypeRoomKeyRotated      NotificationType = "room_key_rotated"
	NotificationTypeUserAddedToRoom     NotificationType = "user_added_to_room"
	NotificationTypeUserJoinedVoiceChat NotificationType = "user_joined_voice_chat"
	NotificationTypeUserLeftVoiceChat   NotificationType = "user_left_voice_chat"
)

type NotificationPayload struct {
	Type     NotificationType `json:"type"`
	Text     string           `json:"text"`
	Username string           `json:"username,omitempty"` // populated for sounds chat notifications
	Data     any              `json:"data,omitempty"`     // additional data depending on notification type, e.g. audio URL for sounds clip notifications
}

type DirectMessagePayload struct {
	ToUsername   string `json:"toUsername" validate:"required"`
	FromUsername string `json:"fromUsername"`
	CipherText   string `json:"cipherText" validate:"required"`
	MessageType  string `json:"messageType,omitempty"`
}

// KeyVerifyChallengePayload is sent by A to B to start a verification session.
// The session ID is routed through the broker; the secret code is shared out-of-band.
// ExpiresAt is a unix timestamp; B must ignore the challenge if it has already passed.
type KeyVerifyChallengePayload struct {
	SessionID    string `json:"sessionId"`
	FromUsername string `json:"fromUsername"`
	ToUsername   string `json:"toUsername"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// KeyVerifyResponsePayload is sent by B back to A as proof of key ownership.
// Signature = Sign(sk_B, SHA-256(code|sessionId|publicKeyPem)).
type KeyVerifyResponsePayload struct {
	SessionID    string `json:"sessionId"`
	ToUsername   string `json:"toUsername"` // A's username
	PublicKeyPEM string `json:"publicKeyPem"`
	Signature    string `json:"signature"` // base64-encoded RSA-PSS signature
}

type PlayAudioPayload struct {
	FromUsername string `json:"fromUsername" validate:"required"`
	RoomID       string `json:"roomId" validate:"required"`
	AudioURL     string `json:"audioURL" validate:"required"`
	Track        int    `json:"track"` // 0–2; which local playback track to use
}
