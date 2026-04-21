// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shared/utils"
	"github.com/srschreiber/nito-client/ui/clientlog"
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

	username := connection.GetSessionUsername()
	encryptedKey, err := keys.EncryptRoomKey(roomKey, username)
	if err != nil {
		return "", fmt.Errorf("room-create: %w", err)
	}

	// Build and sign the initial room-key manifest. First version has no
	// predecessor so PrevVersionNum=0 and PrevKeyHash="".
	nonce, err := keys.GenerateManifestNonce()
	if err != nil {
		return "", fmt.Errorf("room-create: %w", err)
	}
	manifest := apitypes.RoomKeyManifest{
		CurKeyHash:     keys.HashRoomKey(roomKey),
		CurVersionNum:  1,
		Nonce:          nonce,
		PrevKeyHash:    "",
		PrevVersionNum: 0,
		RotatedBy:      username,
		Timestamp:      time.Now().Unix(),
	}
	// The manifest carries the signing device's id (sha256 of the signer's
	// public key). Verifiers re-derive it from whichever pub key they're
	// about to verify with and reject on mismatch, so the broker can't
	// swap a pinned device id.
	manifest.DeviceID = connection.GetSessionDeviceID()
	if manifest.DeviceID == "" {
		return "", fmt.Errorf("room-create: no session device id")
	}
	manifestSig, err := keys.SignRoomKeyManifest(&manifest, username)
	if err != nil {
		return "", fmt.Errorf("room-create: sign manifest: %w", err)
	}

	// Row-level room attestation (rooms.signature). Signed over
	// "device_id;name;owner" alphabetically joined. Broker stores but does
	// not yet verify — future verification pass will re-derive these bytes
	// from the stored row + device record.
	deviceID := connection.GetSessionDeviceID()
	roomSig, err := keys.SignAttestation(map[string]string{
		"device_id": deviceID,
		"name":      name,
		"owner":     username,
	}, username)
	if err != nil {
		return "", fmt.Errorf("room-create: sign room: %w", err)
	}

	id, roomName, err := connection.CreateRoom(name, encryptedKey, manifest, manifestSig, roomSig, deviceID)
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

	// Check the local trust store first; fall back to broker with TOFU caching.
	keyPEM := ""
	if rec, ok := keys.LoadPeerPublicKey(username); ok {
		if !rec.Verified {
			clientlog.Warn("inviting user with unverified key: %s", username)
		}
		keyPEM = rec.PublicKey
	} else {
		inviteePub, err := connection.GetOrStoreUserPublicKey(username)
		if err != nil {
			return "", fmt.Errorf("room-invite: get invitee public key: %w", err)
		}
		_ = keys.SavePeerPublicKey(username, keys.TrustedKey{
			PublicKey: inviteePub,
			Verified:  false,
			Method:    keys.TrustMethodTOFU,
		})
		keyPEM = inviteePub
	}

	return InviteUserWithKey(username, keyPEM)
}

// InviteUserWithKey encrypts the current room key with keyPEM and sends the invite.
// The caller is responsible for obtaining and trusting keyPEM before calling this.
func InviteUserWithKey(username, keyPEM string) (string, error) {
	roomID := utils.DerefOrZero(connection.GetSessionRoomID())
	if roomID == "" {
		return "", errors.New("room-invite: no room selected (use room-select or select in UI)")
	}

	encryptedKey := connection.GetSessionEncryptedRoomKey()
	if encryptedKey == nil {
		return "", errors.New("room-invite: no encrypted room key in session")
	}

	roomKey, err := keys.DecryptRoomKey(utils.DerefOrZero(encryptedKey), connection.GetSessionUsername())
	if err != nil {
		return "", fmt.Errorf("room-invite: decrypt room key: %w", err)
	}

	encryptedForInvitee, err := keys.EncryptDataWithPEM(roomKey, keyPEM)
	if err != nil {
		return "", fmt.Errorf("room-invite: encrypt for invitee: %w", err)
	}

	// Row-level membership attestation (room_members.signature). Signed by
	// the INVITER over "device_id;invited_username;room_id" alphabetically.
	// Broker stores without verifying for now.
	me := connection.GetSessionUsername()
	deviceID := connection.GetSessionDeviceID()
	membershipSig, err := keys.SignAttestation(map[string]string{
		"device_id":        deviceID,
		"invited_username": username,
		"room_id":          roomID,
	}, me)
	if err != nil {
		return "", fmt.Errorf("room-invite: sign membership: %w", err)
	}

	if err := connection.InviteUser(roomID, username, encryptedForInvitee, membershipSig, deviceID); err != nil {
		return "", err
	}

	return fmt.Sprintf("invited %s to room %s", username, roomID), nil
}

// CreateRoomDirect creates a new room with the given name.
func CreateRoomDirect(name string) (string, error) {
	return roomCreateCmd([]Argument{{Name: "n", Values: []string{name}}})
}

// InviteUserDirect invites a user using the cached key (TOFU fallback if unknown).
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
