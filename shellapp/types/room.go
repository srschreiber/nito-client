// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package types

import apitypes "github.com/srschreiber/nito-client/shared/api_types"

// RoomsUpdatedMsg is broadcast whenever the room list should be refreshed.
type RoomsUpdatedMsg struct {
	Rooms []apitypes.RoomEntry
}

// RoomsFetchMsg triggers an immediate room list fetch in RoomsComponent.
type RoomsFetchMsg struct{}

// RoomSelectedMsg is emitted when a room is selected (via UI or command).
type RoomSelectedMsg struct {
	RoomID string
}

// RoomDeselectedMsg is emitted when the user navigates away from a room to a non-room view.
type RoomDeselectedMsg struct {
	RoomID string
}

// RoomMembersUpdatedMsg is broadcast when the member list for the selected room is refreshed.
type RoomMembersUpdatedMsg struct {
	Members []apitypes.RoomMemberEntry
}

// RoomMembersFetchMsg triggers an immediate members fetch.
type RoomMembersFetchMsg struct {
	RoomID string
}

type ErrorMsg struct {
	Message string
}
