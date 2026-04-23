// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
)

// verifyWindow is how long the bot waits for the operator to complete the
// handshake after the 6-digit code is displayed. Matches the desktop client's
// behaviour so a single operator juggling both sides doesn't hit different
// timeouts depending on which side they're on.
const verifyWindow = 5 * time.Minute

// runVerifyStep drives the bot's side of the mutual-verify handshake.
// The bot is always initiator A: it generates a session + 6-digit code,
// displays the code, and blocks until B (the operator) completes the flow
// from their desktop client.
//
// Persistent state is only written after both the response check and
// the confirm have round-tripped successfully — a mid-flight abort leaves
// the bot in StepRegistered and the operator can retry from a clean slate.
func runVerifyStep(ctx context.Context, state BotState) (BotState, error) {
	m := NewVerifyPromptModel(state.Username)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return state, fmt.Errorf("verify prompt: %w", err)
	}
	fm, ok := finalModel.(*Model)
	if !ok {
		return state, errors.New("verify prompt: unexpected model type")
	}
	if fm.phase == phaseAborted {
		return state, errors.New("verify aborted")
	}
	target := fm.verifyTarget
	if target == "" {
		return state, errors.New("verify: owner username is empty")
	}

	code, sessionID, err := keys.GenerateVerificationChallenge()
	if err != nil {
		return state, fmt.Errorf("verify: generate challenge: %w", err)
	}
	// Reload our own public key fresh from disk rather than cache — if a
	// concurrent rotation happened between registration and verify, the
	// disk copy is the source of truth the broker will accept.
	pubPEM, err := keys.LoadPublicKeyPEM(state.Username)
	if err != nil {
		return state, fmt.Errorf("verify: load self pub: %w", err)
	}
	expiresAt := time.Now().Add(verifyWindow).Unix()

	log.Printf("verify: share this code with %q out of band: %s", target, styleCode.Render(code))
	log.Printf("verify: session=%s expires in %s", sessionID, verifyWindow)

	if err := connection.SendKeyVerifyChallenge(target, sessionID, pubPEM, expiresAt); err != nil {
		return state, fmt.Errorf("verify: send challenge: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, verifyWindow)
	defer cancel()
	resp, err := connection.WaitForKeyVerifyResponse(waitCtx, sessionID)
	if err != nil {
		return state, fmt.Errorf("verify: wait response: %w", err)
	}

	// The response carries pk_B and sig_B; pk_B is ground truth going
	// forward, not a broker-supplied copy.
	if err := keys.VerifyResponseSignature(code, sessionID, pubPEM, resp.PublicKeyPEM, resp.Signature); err != nil {
		return state, fmt.Errorf("verify: bad response signature from %q: %w", target, err)
	}

	confirmSig, err := keys.SignVerificationConfirm(code, sessionID, pubPEM, resp.PublicKeyPEM, state.Username)
	if err != nil {
		return state, fmt.Errorf("verify: sign confirm: %w", err)
	}
	if err := connection.SendKeyVerifyConfirm(target, sessionID, confirmSig); err != nil {
		return state, fmt.Errorf("verify: send confirm: %w", err)
	}

	// Ground truth: pin the owner as mutually-verified. IsBot is false —
	// the owner is a human operator, and this label flows into UI
	// badges wherever the bot's pubkey_cache is inspected.
	if err := keys.SavePeerPublicKey(target, keys.TrustedKey{
		PublicKey: resp.PublicKeyPEM,
		Verified:  true,
		Method:    keys.TrustMethodVerified,
	}); err != nil {
		return state, fmt.Errorf("verify: pin owner key: %w", err)
	}

	// Publish introduction so other clients that have verified us can
	// upgrade their owner pin from TOFU to Introduced — supports the
	// eventual "multiple owners" / "multiple bots" extensions. Best-effort.
	if err := connection.PublishIntroductionFor(target, resp.PublicKeyPEM); err != nil {
		log.Printf("verify: publish introduction failed (non-fatal): %v", err)
	}

	ownerDevice, err := keys.DeviceIDFromPublicKeyPEM(resp.PublicKeyPEM)
	if err != nil {
		return state, fmt.Errorf("verify: derive owner device id: %w", err)
	}

	state.OwnerUsername = target
	state.OwnerDeviceID = ownerDevice
	state.Step = StepVerified
	if err := SaveState(state); err != nil {
		return state, fmt.Errorf("verify: save state: %w", err)
	}
	log.Printf("verify: mutually verified %q (device=%s) — awaiting room invite", target, ownerDevice)
	return state, nil
}
