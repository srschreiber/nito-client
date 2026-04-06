// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package types

// ConnectionStatusMsg is broadcast after any command that may change connection state.
type ConnectionStatusMsg struct {
	Connected bool
	BrokerURL string
	UserID    string
	LatencyMs int64
}

// ConnectedMsg is sent once after a successful connect, to re-arm WS listeners.
type ConnectedMsg struct{}

// MembersUpdatedMsg is sent when the broker pushes a "members_updated" RPC,
// signalling that room member online/offline status should be refetched.
type MembersUpdatedMsg struct{}
