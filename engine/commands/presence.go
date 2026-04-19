// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

func sendPresence(rpcName string, payload any) {
	s := connection.CurrentSession()
	if s == nil {
		return
	}
	p, err := json.Marshal(payload)
	if err != nil {
		clientlog.Error("presence: marshal %s: %v", rpcName, err)
		return
	}
	msg := wstypes.ToBrokerWsMessage{
		RPCName:   rpcName,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    s.Username,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   p,
	}
	data, _ := json.Marshal(msg)
	if err := connection.Send(data); err != nil {
		clientlog.Error("presence: send %s: %v", rpcName, err)
	}
}

// SendRoomEnter notifies the broker the user is now viewing the given room.
func SendRoomEnter(roomID string) {
	sendPresence(wstypes.RPCRoomEnter, wstypes.RoomEnterPayload{RoomID: roomID})
}

// SendRoomLeave notifies the broker the user navigated away from the given room.
func SendRoomLeave(roomID string) {
	sendPresence(wstypes.RPCRoomLeave, wstypes.RoomLeavePayload{RoomID: roomID})
}
