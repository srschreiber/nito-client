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
	Signature string          `json:"signature" validate:"required"`
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
	RPCEcho           = "echo"
	RPCRoomMessage    = "room_message"
	RPCNotification   = "notification"
	RPCMembersUpdated = "members_updated"
	RPCDirectMessage  = "direct_message"

	// Voice signaling RPCs.
	RPCVoiceJoin             = "voice_join"               // client → broker: join voice call, carries SDP offer
	RPCVoiceAnswer           = "voice_answer"             // broker → client: SDP answer for initial join
	RPCVoiceLeave            = "voice_leave"              // client → broker: leave voice call
	RPCVoiceOffer            = "voice_offer"              // broker → client: renegotiation offer (new participant joined)
	RPCVoiceRenegAnswer      = "voice_reneg_answer"       // client → broker: answer to broker renegotiation offer
	RPCVoiceICERestart       = "voice_ice_restart"        // client → broker: ICE restart offer
	RPCVoiceICERestartAnswer = "voice_ice_restart_answer" // broker → client: ICE restart answer
)

type VoiceJoinPayload struct {
	RoomID   string `json:"roomId"`
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

type NotificationPayload struct {
	Text string `json:"text"`
}

type DirectMessagePayload struct {
	ToUsername   string `json:"toUsername" validate:"required"`
	FromUsername string `json:"fromUsername"`
	CipherText   string `json:"cipherText" validate:"required"`
	MessageType  string `json:"messageType,omitempty"`
}
