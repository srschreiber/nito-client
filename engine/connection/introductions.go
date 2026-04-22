// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

// HTTP client for the signed-introductions endpoints. Introductions
// publish *after* a mutual-verify completes and are pulled *before*
// joining a room — both sides of the web-of-trust story go through
// this file.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// PublishIntroductionFor builds and signs an introduction stating that
// *we* (the authenticated session) have verified `peerUsername` has
// public key `peerPubPEM`, then uploads it. Called from the mutual-verify
// success paths so every completed handshake emits a fresh vouch into
// the broker's introduction store. Best-effort: a publish failure is
// logged but does not fail the verification itself.
func PublishIntroductionFor(peerUsername, peerPubPEM string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	selfPub, err := keys.LoadPublicKeyPEM(s.Username)
	if err != nil {
		return fmt.Errorf("publish introduction: load self pub: %w", err)
	}
	selfDevice, err := keys.DeviceIDFromPublicKeyPEM(selfPub)
	if err != nil {
		return fmt.Errorf("publish introduction: derive self device id: %w", err)
	}
	intro := apitypes.SignedIntroduction{
		Username:           peerUsername,
		PublicKey:          peerPubPEM,
		VerifiedByUsername: s.Username,
		VerifiedByDeviceID: selfDevice,
	}
	sig, err := keys.SignIntroduction(&intro, s.Username)
	if err != nil {
		return fmt.Errorf("publish introduction: sign: %w", err)
	}
	intro.VerifiedSignature = sig
	return PublishIntroduction(intro)
}

// PublishIntroduction uploads a signed introduction to the broker.
// Callers must have already populated intro.VerifiedSignature via
// keys.SignIntroduction. The broker stores it and returns it later in
// ListIntroductions responses to users who have verified us.
func PublishIntroduction(intro apitypes.SignedIntroduction) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(apitypes.SignedIntroductionRequest{SignedIntroduction: intro})
	resp, err := signedPost(s.v0("/introductions"), s.Username, "/api/v0/introductions", body)
	if err != nil {
		return fmt.Errorf("publish introduction: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("publish introduction: broker returned %s", resp.Status)
	}
	return nil
}

// ListIntroductions fetches signed introductions relevant to the current
// user. When roomID is non-empty, the broker filters to introductions
// about members of that room — essential at room-join time to avoid
// pulling the user's entire social graph per join. The broker is
// expected to scope results to introductions signed by users the caller
// has already verified (since unverified signers are worthless to us),
// but the client re-verifies every signature before trusting any of it.
func ListIntroductions(roomID string) ([]apitypes.SignedIntroduction, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	url := s.v0("/introductions/list")
	if roomID != "" {
		url += "?roomId=" + roomID
	}
	resp, err := signedGet(url, s.Username, "/api/v0/introductions/list")
	if err != nil {
		return nil, fmt.Errorf("list introductions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list introductions: broker returned %s", resp.Status)
	}
	var result apitypes.ListSignedIntroductionsFromVerifiedUsersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list introductions: decode: %w", err)
	}
	return result.Introductions, nil
}
