// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/srschreiber/nito-client/shared/utils"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/keys"
)

func echoCmd(args []Argument) (string, error) {
	s := connection.CurrentSession()
	if s == nil {
		return "", errors.New("echo: not connected (use connect first)")
	}

	text := strings.Join(extractArgValues(args, "m", "message"), " ")
	if text == "" {
		return "", errors.New("echo: -m/--message <text> is required")
	}

	payload, err := json.Marshal(wstypes.EchoPayload{Text: text})
	if err != nil {
		return "", fmt.Errorf("echo: %w", err)
	}
	msg := wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.RPCEcho,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    s.UserID,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("echo: %w", err)
	}
	if err := connection.Send(data); err != nil {
		return "", fmt.Errorf("echo: send: %w", err)
	}
	// Response arrives asynchronously via the readLoop → incomingChan → TUI model.
	return "", nil
}

// SendRoomMessage sends text to the currently selected room without going through the command parser.
// Used by chat mode so raw input can be sent directly.
func SendRoomMessage(text string) error {
	return sendRoomMessageWithType(text, wstypes.MessageTypeText)
}

// SendRoomImage sends an ASCII-art image string to the currently selected room,
// marking it with MessageType "image" so recipients render it without re-styling.
func SendRoomImage(text string) error {
	return sendRoomMessageWithType(text, wstypes.MessageTypeImage)
}

func sendRoomMessageWithType(text, msgType string) error {
	s := connection.CurrentSession()
	if s == nil {
		return errors.New("say: not connected (use connect first)")
	}
	roomID := utils.DerefOrZero(connection.GetSessionRoomID())
	if roomID == "" {
		return errors.New("say: no room selected (use room-select first)")
	}
	if text == "" {
		return errors.New("say: message text is required")
	}

	ukc, err := connection.GetOrCreateRoomKeyChain()
	if err != nil {
		return fmt.Errorf("say: get room key chain: %w", err)
	}
	roomKeyVersion := utils.DerefOrZero(connection.GetSessionRoomKeyVersion())

	roomInfo := connection.GetSessionRoomInfo()
	if roomInfo == nil {
		return errors.New("say: no room info available for selected room")
	}

	ciphertext, err := ukc.EncryptMessageWithRoomKey([]byte(text), s.UserID, &roomInfo.SentMessageCount)
	if err != nil {
		return fmt.Errorf("say: encrypt: %w", err)
	}

	payload, err := json.Marshal(wstypes.RoomMessagePayload{
		RoomID:             roomID,
		RoomKeyVersion:     roomKeyVersion,
		SenderMessageCount: roomInfo.SentMessageCount,
		FromUsername:       s.UserID,
		EncryptedText:      base64.StdEncoding.EncodeToString(ciphertext),
		MessageType:        msgType,
	})
	if err != nil {
		return fmt.Errorf("say: %w", err)
	}
	msg := wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.RPCRoomMessage,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    s.UserID,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("say: %w", err)
	}
	if err := connection.Send(data); err != nil {
		return fmt.Errorf("say: send: %w", err)
	}
	connection.IncrementSessionSentMessageCount()
	return nil
}

// SendDirectMessage sends an RSA-encrypted direct message to toUsername.
func SendDirectMessage(toUsername, text string) error {
	s := connection.CurrentSession()
	if s == nil {
		return errors.New("dm: not connected (use login first)")
	}

	toPEM, err := connection.GetUserPublicKey(toUsername)
	if err != nil {
		return fmt.Errorf("dm: get user public key: %w", err)
	}

	encryptedText, err := keys.EncryptDataWithPEM([]byte(text), toPEM)
	if err != nil {
		return fmt.Errorf("dm: encrypt: %w", err)
	}

	payload := wstypes.DirectMessagePayload{
		ToUsername:   toUsername,
		FromUsername: s.UserID,
		CipherText:   encryptedText,
	}

	marshaled, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dm: marshal payload: %w", err)
	}

	msg := wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.RPCDirectMessage,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    s.UserID,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   marshaled,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("dm: marshal message: %w", err)
	}
	return connection.Send(data)
}

func sayCmd(args []Argument) (string, error) {
	text := strings.Join(extractArgValues(args, "m", "message"), " ")
	if text == "" {
		return "", errors.New("say: -m/--message <text> is required")
	}
	return "", sendRoomMessageWithType(text, wstypes.MessageTypeText)
}
