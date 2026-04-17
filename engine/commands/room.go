// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/shared/utils"
)

func roomCreateCmd(args []Argument) (string, error) {
	name := strings.Join(extractArgValues(args, "n", "name"), " ")
	if name == "" {
		return "", errors.New("room-create: -n/--name <name> is required")
	}

	roomKey, err := keys.GenerateRoomKey()
	if err != nil {
		return "", fmt.Errorf("room-create: %w", err)
	}

	encryptedKey, err := keys.EncryptRoomKey(roomKey, connection.GetSessionUserID())
	if err != nil {
		return "", fmt.Errorf("room-create: %w", err)
	}

	id, roomName, err := connection.CreateRoom(name, encryptedKey)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("room %q created (id: %s)", roomName, id), nil
}

func roomListCmd() (string, error) {
	rooms, err := connection.ListRooms()
	if err != nil {
		return "", err
	}
	if len(rooms) == 0 {
		return "no rooms", nil
	}
	var lines []string
	for _, r := range rooms {
		owner := ""
		if r.IsOwner {
			owner = " (owner)"
		}
		lines = append(lines, fmt.Sprintf("%s  %s%s", r.ID, r.Name, owner))
	}
	return strings.Join(lines, "\n"), nil
}

func roomSelectCmd(args []Argument) (string, Signal, error) {
	query := extractArg(args, "r", "room")
	if query == "" {
		return "", SignalNone, errors.New("room-select: -r/--room <name or id> is required")
	}
	rooms, err := connection.ListRooms()
	if err != nil {
		return "", SignalNone, err
	}
	var matched []string
	for _, r := range rooms {
		if r.ID == query || strings.HasPrefix(r.ID, query) || strings.EqualFold(r.Name, query) {
			matched = append(matched, r.ID)
		}
	}
	if len(matched) == 0 {
		return "", SignalNone, fmt.Errorf("room-select: no room matching %q", query)
	}
	if len(matched) > 1 {
		return "", SignalNone, fmt.Errorf("room-select: ambiguous: %d rooms match %q", len(matched), query)
	}

	err = connection.SetSessionRoom(matched[0])
	if err != nil {
		return "", SignalNone, fmt.Errorf("room-select: set current room failed: %w", err)
	}

	return fmt.Sprintf("selected room %s", matched[0]), SignalRoomSelected, nil
}

// TODO: see the todo.txt. we will not assign a key until users join
func roomInviteCmd(args []Argument) (string, error) {
	username := extractArg(args, "u", "user")
	if username == "" {
		return "", errors.New("room-invite: -u/--user <username> is required")
	}
	roomID := utils.DerefOrZero(connection.GetSessionRoomID())
	if roomID == "" {
		return "", errors.New("room-invite: no room selected (use room-select or select in UI)")
	}

	encryptedKey := connection.GetSessionEncryptedRoomKey()
	if encryptedKey == nil {
		return "", errors.New("room-invite: no encrypted room key in session")
	}

	roomKey, err := keys.DecryptRoomKey(utils.DerefOrZero(encryptedKey), connection.GetSessionUserID())
	if err != nil {
		return "", fmt.Errorf("room-invite: decrypt room key: %w", err)
	}

	inviteePub, err := connection.GetUserPublicKey(username)
	if err != nil {
		return "", fmt.Errorf("room-invite: get invitee public key: %w", err)
	}

	encryptedForInvitee, err := keys.EncryptDataWithPEM(roomKey, inviteePub)
	if err != nil {
		return "", fmt.Errorf("room-invite: encrypt for invitee: %w", err)
	}

	if err := connection.InviteUser(roomID, username, encryptedForInvitee); err != nil {
		return "", err
	}

	return fmt.Sprintf("invited %s to room %s", username, roomID), nil
}

// CreateRoomDirect creates a new room with the given name.
func CreateRoomDirect(name string) (string, error) {
	return roomCreateCmd([]Argument{{Name: "n", Values: []string{name}}})
}

// InviteUserDirect invites a user to the currently selected room.
func InviteUserDirect(username string) (string, error) {
	return roomInviteCmd([]Argument{{Name: "u", Values: []string{username}}})
}

func roomInvitesCmd() (string, error) {
	invites, err := connection.ListPendingInvites()
	if err != nil {
		return "", err
	}
	if len(invites) == 0 {
		return "no pending invites", nil
	}
	var lines []string
	for _, inv := range invites {
		lines = append(lines, fmt.Sprintf("%s  %s", inv.RoomID, inv.RoomName))
	}
	return strings.Join(lines, "\n"), nil
}

func roomAcceptCmd(args []Argument) (string, error) {
	roomID := extractArg(args, "r", "room")
	if roomID == "" {
		return "", errors.New("room-accept: -r/--room <room_id> is required")
	}
	if err := connection.AcceptInvite(roomID); err != nil {
		return "", err
	}
	return fmt.Sprintf("joined room %s", roomID), nil
}
