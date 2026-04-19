// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"context"
	"errors"

	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
)

// SendKeyVerifyChallenge sends A's verification challenge to toUsername via the
// broker. Carries pk_A so B can sign the role-tagged mutual-handshake hash.
// The secret code is never transmitted — it is shared out-of-band.
func SendKeyVerifyChallenge(toUsername, sessionID, initiatorPubPEM string, expiresAt int64) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	return sendWsRPC(wstypes.RPCKeyVerifyChallenge, s.UserID, wstypes.KeyVerifyChallengePayload{
		SessionID:             sessionID,
		FromUsername:          s.UserID,
		ToUsername:            toUsername,
		InitiatorPublicKeyPEM: initiatorPubPEM,
		ExpiresAt:             expiresAt,
	})
}

// SendKeyVerifyResponse sends B's signed proof back to A (identified by toUsername).
func SendKeyVerifyResponse(toUsername, sessionID, pubKeyPEM, sig string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	return sendWsRPC(wstypes.RPCKeyVerifyResponse, s.UserID, wstypes.KeyVerifyResponsePayload{
		SessionID:    sessionID,
		ToUsername:   toUsername,
		PublicKeyPEM: pubKeyPEM,
		Signature:    sig,
	})
}

// SendKeyVerifyConfirm sends A's closing signature back to B, completing the
// mutual handshake.
func SendKeyVerifyConfirm(toUsername, sessionID, sig string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	return sendWsRPC(wstypes.RPCKeyVerifyConfirm, s.UserID, wstypes.KeyVerifyConfirmPayload{
		SessionID:  sessionID,
		ToUsername: toUsername,
		Signature:  sig,
	})
}

// WaitForKeyVerifyResponse blocks until A receives B's signed response for the
// given session or until ctx is cancelled/deadline exceeded.
func WaitForKeyVerifyResponse(ctx context.Context, sessionID string) (wstypes.KeyVerifyResponsePayload, error) {
	ch := make(chan wstypes.KeyVerifyResponsePayload, 1)
	pendingVerifications.Store(sessionID, ch)
	defer pendingVerifications.Delete(sessionID)
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return wstypes.KeyVerifyResponsePayload{}, errors.New("verification timed out")
		}
		return wstypes.KeyVerifyResponsePayload{}, errors.New("verification cancelled")
	}
}
