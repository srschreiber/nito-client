// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// CreateRoom creates a new room on the broker. Requires an active session.
// encryptedRoomKey is the base64 RSA-OAEP ciphertext of the room's AES key.
// manifest + manifestSig are the signed metadata every member will use to
// detect if the broker later substitutes a different key. roomSig + roomSigDev
// are the creator's attestation over the row itself (rooms.signature column;
// immutable, set at creation — the broker stores without verifying for now).
func CreateRoom(name, encryptedRoomKey string, manifest apitypes.RoomKeyManifest, manifestSig, roomSig, roomSigDev string) (id, roomName string, err error) {
	s := CurrentSession()
	if s == nil {
		return "", "", errors.New("not connected")
	}
	body, _ := json.Marshal(apitypes.CreateRoomRequest{
		Name:                     name,
		UserID:                   s.Username,
		EncryptedRoomKey:         encryptedRoomKey,
		VersionManifest:          manifest,
		VersionManifestSignature: manifestSig,
		RoomSignature:            roomSig,
		RoomSignedByDevice:       roomSigDev,
	})
	resp, err := signedPost(s.v0("/rooms"), s.Username, "/api/v0/rooms", body)
	if err != nil {
		return "", "", fmt.Errorf("create room: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("create room: broker returned %s", resp.Status)
	}
	var result apitypes.CreateRoomResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("create room: decode: %w", err)
	}
	return result.ID, result.Name, nil
}

// ListRooms returns all rooms the current user is a member of.
func ListRooms() ([]apitypes.RoomEntry, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/list?user_id="+s.Username),
		s.Username,
		"/api/v0/rooms/list",
	)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list rooms: broker returned %s", resp.Status)
	}
	var result struct {
		Rooms []apitypes.RoomEntry `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list rooms: decode: %w", err)
	}
	return result.Rooms, nil
}

// getMyRoomKey fetches the caller's encrypted room key + version for the
// given room and verifies the accompanying signed manifest. Returns the
// manifest's `rotated_by` username alongside so the caller can decide
// whether the rotator's identity is sufficiently trusted.
func getMyRoomKey(roomID string) (encryptedKey string, keyVersion int, rotator, rotatorDevice string, err error) {
	s := CurrentSession()
	if s == nil {
		return "", 0, "", "", errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/key?room_id="+roomID),
		s.Username,
		"/api/v0/rooms/key",
	)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("get room key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, "", "", fmt.Errorf("get room key: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, "", "", fmt.Errorf("get room key: decode: %w", err)
	}

	// Verify the manifest signature against the rotator's public key. Device
	// ids are sha256(DER(pub_key)) hex, so the manifest's claimed device_id
	// must match the derivation of whichever pub key we end up verifying
	// with — otherwise the broker could substitute a different (user, key)
	// pair whose signature happens to verify. Self uses the on-disk PEM;
	// peers use the pinned PEM (or TOFU-fetch from the broker under the
	// same derivation check).
	if result.VersionManifestSignature != "" {
		rotator = result.VersionManifest.RotatedBy
		if rotator == "" {
			return "", 0, "", "", fmt.Errorf("get room key: manifest has no rotator")
		}
		rotatorDevice = result.VersionManifest.DeviceID
		if rotatorDevice == "" {
			return "", 0, "", "", fmt.Errorf("get room key: manifest has no device id")
		}
		var pubPEM string
		switch {
		case rotator == s.Username:
			// Self-signed manifest (we created this room, or we rotated
			// its key). Always use our own on-disk public key — never
			// the broker's view, which could be substituted.
			pubPEM, err = keys.LoadPublicKeyPEM(rotator)
			if err != nil {
				return "", 0, "", "", fmt.Errorf("get room key: load self public key: %w", err)
			}
		default:
			if rec, ok := keys.LoadPeerPublicKeyByDevice(rotator, rotatorDevice); ok {
				pubPEM = rec.PublicKey
			} else {
				// TOFU-fetch. The derivation check below is what makes
				// this safe: we will reject if the broker hands us a pub
				// key that doesn't hash to the claimed device_id.
				pubPEM, err = GetOrStoreUserPublicKey(rotator)
				if err != nil {
					return "", 0, "", "", fmt.Errorf("get room key: fetch rotator public key: %w", err)
				}
			}
		}
		// Binding check: the key we're about to verify with must be the
		// key whose id the manifest claims. Stops broker swaps cold.
		derivedDevice, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
		if err != nil {
			return "", 0, "", "", fmt.Errorf("get room key: derive device id: %w", err)
		}
		if derivedDevice != rotatorDevice {
			return "", 0, "", "", fmt.Errorf("get room key: device id mismatch (rotator=%s claimed=%s derived=%s)", rotator, rotatorDevice, derivedDevice)
		}
		if err := keys.VerifyRoomKeyManifest(&result.VersionManifest, result.VersionManifestSignature, pubPEM); err != nil {
			return "", 0, "", "", fmt.Errorf("get room key: manifest signature invalid (rotator=%s device=%s): %w", rotator, rotatorDevice, err)
		}
		clientlog.Info("manifest signature valid for room %s key version %d (rotated_by=%s device_id=%s)", roomID, result.VersionManifest.CurVersionNum, rotator, rotatorDevice)
	}

	return result.EncryptedRoomKey, result.KeyVersion, rotator, rotatorDevice, nil
}

// getRoomInfo fetches the caller's room info (e.g. sent message count) for the given room.
func getRoomInfo(roomID string) (*RoomInfo, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/info?room_id="+roomID),
		s.Username,
		"/api/v0/rooms/info",
	)
	if err != nil {
		return nil, fmt.Errorf("get room info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get room info: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get room info: decode: %w", err)
	}
	return &RoomInfo{SentMessageCount: result.SentMessageCount}, nil
}

// InviteUser invites a user to a room, sending their encrypted copy of the
// room key. membershipSig + membershipSigDev are the inviter's attestation
// over the membership row (room_members.signature; stored without broker
// verification until the verification pass ships).
func InviteUser(roomID, invitedUsername, encryptedRoomKey, membershipSig, membershipSigDev string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(apitypes.InviteUserRequest{
		RoomID:                   roomID,
		InvitedUsername:          invitedUsername,
		EncryptedRoomKey:         encryptedRoomKey,
		MembershipSignature:      membershipSig,
		MembershipSignedByDevice: membershipSigDev,
	})
	resp, err := signedPost(s.v0("/rooms/invite"), s.Username, "/api/v0/rooms/invite", body)
	if err != nil {
		return fmt.Errorf("invite user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite user: broker returned %s", resp.Status)
	}
	return nil
}

// ListRoomMembers returns the joined members of a room.
func ListRoomMembers(roomID string) ([]apitypes.RoomMemberEntry, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/members?room_id="+roomID),
		s.Username,
		"/api/v0/rooms/members",
	)
	if err != nil {
		return nil, fmt.Errorf("list room members: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list room members: broker returned %s", resp.Status)
	}
	var result struct {
		Members []apitypes.RoomMemberEntry `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list room members: decode: %w", err)
	}
	return result.Members, nil
}

// ListPendingInvites returns rooms the current user has been invited to but not yet joined.
func ListPendingInvites() ([]apitypes.PendingInvite, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/invites?user_id="+s.Username),
		s.Username,
		"/api/v0/rooms/invites",
	)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list invites: broker returned %s", resp.Status)
	}
	var result struct {
		Invites []apitypes.PendingInvite `json:"invites"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list invites: decode: %w", err)
	}
	return result.Invites, nil
}

// GetRoomMessages fetches the most recent limit messages from the given room along with
// the encrypted room keys needed to decrypt them.
func GetRoomMessages(roomID string, limit int) (*apitypes.GetRoomMessagesResponse, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	body, _ := json.Marshal(apitypes.GetRoomMessagesRequest{
		RoomID: roomID,
		Limit:  &limit,
	})
	resp, err := signedPost(s.v0("/rooms/messages"), s.Username, "/api/v0/rooms/messages", body)
	if err != nil {
		return nil, fmt.Errorf("get room messages: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get room messages: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get room messages: decode: %w", err)
	}
	return &result, nil
}

// AcceptInvite accepts a pending room invitation.
func AcceptInvite(roomID string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{"roomId": roomID})
	resp, err := signedPost(s.v0("/rooms/invites/accept"), s.Username, "/api/v0/rooms/invites/accept", body)
	if err != nil {
		return fmt.Errorf("accept invite: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("accept invite: broker returned %s", resp.Status)
		clientlog.Error("invite accept failed: %v", err)
		return err
	}
	clientlog.Info("accepted invite to room %s", roomID)
	return nil
}
