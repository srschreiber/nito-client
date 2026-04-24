// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import "sync"

// BotRuntime wraps the persisted BotState with a mutex so the serve loop
// (reader of RoomIDs) and the background invite-accept loop (writer of
// RoomIDs) can race-freely share it. Snapshot returns a defensive copy so
// callers can iterate without holding the lock.
type BotRuntime struct {
	mu    sync.RWMutex
	state BotState
}

func NewRuntime(s BotState) *BotRuntime {
	return &BotRuntime{state: s}
}

func (r *BotRuntime) Snapshot() BotState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.state
	if len(r.state.RoomIDs) > 0 {
		c.RoomIDs = make([]string, len(r.state.RoomIDs))
		copy(c.RoomIDs, r.state.RoomIDs)
	}
	return c
}

// AddRoom appends a new room and persists. Returns true if the room was
// new, false if it was already in the list.
func (r *BotRuntime) AddRoom(rid string) (bool, error) {
	r.mu.Lock()
	for _, existing := range r.state.RoomIDs {
		if existing == rid {
			r.mu.Unlock()
			return false, nil
		}
	}
	r.state.RoomIDs = append(r.state.RoomIDs, rid)
	snapshot := r.state
	if len(snapshot.RoomIDs) > 0 {
		cp := make([]string, len(snapshot.RoomIDs))
		copy(cp, snapshot.RoomIDs)
		snapshot.RoomIDs = cp
	}
	r.mu.Unlock()
	return true, SaveState(snapshot)
}

// SetStep transitions to a new step + persists. Used at the StepVerified →
// StepReady boundary when the first invite is accepted.
func (r *BotRuntime) SetStep(step Step) error {
	r.mu.Lock()
	r.state.Step = step
	snapshot := r.state
	if len(snapshot.RoomIDs) > 0 {
		cp := make([]string, len(snapshot.RoomIDs))
		copy(cp, snapshot.RoomIDs)
		snapshot.RoomIDs = cp
	}
	r.mu.Unlock()
	return SaveState(snapshot)
}
