// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// Register sends username and public key to the broker, creating a DB entry if the user
// doesn't exist yet. Returns the user's ID and whether they were already registered.
// isBot marks the account as a bot on the broker; once set, the broker refuses
// to change it, so a user registration accidentally submitted with IsBot=true
// cannot be "un-botted" without re-registering a new name.
func Register(ctx context.Context, brokerURL, username, password, publicKey string, isBot bool) (*apitypes.RegisterResponse, error) {
	brokerURL = normalizeURL(brokerURL)
	deviceName := "primary"
	if isBot {
		deviceName = "bot"
	}
	body, _ := json.Marshal(apitypes.RegisterRequest{
		Username:   username,
		Password:   password,
		PublicKey:  publicKey,
		DeviceName: deviceName,
		IsBot:      isBot,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/register", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("register: broker returned %s", resp.Status)
	}
	var result apitypes.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("register: decode response: %w", err)
	}
	return &result, nil
}

// Login performs the full authentication flow against the broker:
// requests a challenge, signs it, and exchanges credentials for a JWT
// token. The server-assigned DeviceID on the response is ignored — the
// client derives its own device id from the local public key at Connect
// time, so a broker can never swap a session to a different id.
func Login(ctx context.Context, brokerURL, username, password string) (token string, err error) {
	brokerURL = normalizeURL(brokerURL)
	keys.SetActiveBroker(brokerURL)

	// Request challenge.
	challengeBody, _ := json.Marshal(apitypes.LoginChallengeRequest{Username: username})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/login/challenge", bytes.NewReader(challengeBody))
	if err != nil {
		return "", fmt.Errorf("login challenge: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login challenge: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login challenge: broker returned %s", resp.Status)
	}
	var challengeResp apitypes.LoginChallengeResponse
	if err := json.NewDecoder(resp.Body).Decode(&challengeResp); err != nil {
		return "", fmt.Errorf("login challenge: decode: %w", err)
	}

	// Sign "login:<username>:<challenge>" with our private key.
	msg := fmt.Sprintf("login:%s:%s", username, challengeResp.Challenge)
	sig, err := keys.Sign(msg, username)
	if err != nil {
		return "", fmt.Errorf("login sign: %w", err)
	}

	// Identify which device (key) is logging in so the broker can pick
	// the right pubkey to verify the signature against. Single-key-per-
	// user today makes this advisory; multi-device makes it load-bearing.
	// The broker must still verify the signature actually checks out
	// against the named key — device_id alone is never authoritative.
	pubPEM, err := keys.LoadPublicKeyPEM(username)
	if err != nil {
		return "", fmt.Errorf("login: load public key: %w", err)
	}
	deviceID, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		return "", fmt.Errorf("login: derive device id: %w", err)
	}

	// Exchange credentials + signature for a JWT.
	loginBody, _ := json.Marshal(apitypes.LoginRequest{
		Username:  username,
		Password:  password,
		Challenge: challengeResp.Challenge,
		Signature: sig,
		DeviceID:  deviceID,
	})
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/login", bytes.NewReader(loginBody))
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login: broker returned %s", loginResp.Status)
	}
	var result apitypes.LoginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("login: decode: %w", err)
	}
	return result.Token, nil
}

// fetchUserPublicKey calls GET /users/public-key?username=X without touching
// session state. Suitable for Connect-time self-checks where the session
// isn't set up yet. Callers must not use this for peer trust decisions —
// it returns the raw broker response with no TOFU pinning.
func fetchUserPublicKey(brokerURL, username string) (string, error) {
	resp, err := http.Get("http://" + brokerURL + "/api/v0/users/public-key?username=" + username)
	if err != nil {
		return "", fmt.Errorf("get public key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get public key: broker returned %s", resp.Status)
	}
	var result apitypes.GetUserPublicKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("get public key: decode: %w", err)
	}
	return result.PublicKey, nil
}

// AssertBrokerServesOurPublicKey fetches the pubkey the broker holds for
// username and rejects if it doesn't match the local PEM. Run at Connect
// time: a compromised broker serving a forged identity under our username
// is caught before any room key is touched. Compares by derived device id
// so PEM whitespace variations don't produce false positives.
func AssertBrokerServesOurPublicKey(brokerURL, username string) error {
	localPEM, err := keys.LoadPublicKeyPEM(username)
	if err != nil {
		return fmt.Errorf("self-check: load local public key: %w", err)
	}
	localID, err := keys.DeviceIDFromPublicKeyPEM(localPEM)
	if err != nil {
		return fmt.Errorf("self-check: derive local device id: %w", err)
	}
	brokerPEM, err := fetchUserPublicKey(brokerURL, username)
	if err != nil {
		return fmt.Errorf("self-check: fetch broker copy: %w", err)
	}
	brokerID, err := keys.DeviceIDFromPublicKeyPEM(brokerPEM)
	if err != nil {
		return fmt.Errorf("self-check: derive broker device id: %w", err)
	}
	if brokerID != localID {
		return fmt.Errorf("self-check: broker returned a different public key for %s (local=%s broker=%s) — possible broker compromise", username, localID, brokerID)
	}
	return nil
}

// GetOrStoreUserPublicKey returns the PEM-encoded public key for a username.
//
// The signed-in user's own key is always served from their local keystore
// (written at registration), never from the broker — a compromised broker
// must never be able to hand us a forged copy of our *own* key.
//
// For peers it's disk-first, TOFU-on-miss: if the key has already been pinned
// (verified via mutual-handshake, or TOFU'd on a prior fetch), that stored
// copy is returned and the broker is not contacted. This matters for paths
// like room-key manifest verification where a compromised broker could
// otherwise serve a substituted public key matched to a substituted signature.
//
// If no peer record exists, we fall back to the broker, save the result as
// an unverified TOFU pin, and return it. Subsequent calls hit disk.
func GetOrStoreUserPublicKey(username string) (string, error) {
	// Self: always use our own on-disk public key, never the broker's view.
	if s := CurrentSession(); s != nil && s.Username == username {
		return keys.LoadPublicKeyPEM(username)
	}
	if rec, ok := keys.LoadPeerPublicKey(username); ok && rec.PublicKey != "" {
		return rec.PublicKey, nil
	}
	s := CurrentSession()
	if s == nil {
		return "", errors.New("not connected")
	}
	resp, err := http.Get(s.v0("/users/public-key?username=" + username))
	if err != nil {
		return "", fmt.Errorf("get public key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get public key: broker returned %s", resp.Status)
	}
	var result apitypes.GetUserPublicKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("get public key: decode: %w", err)
	}
	// TOFU-pin on first sight so every future caller — including the
	// manifest verifier — gets the same bytes without re-contacting the
	// broker. Verified pins (set via the mutual-handshake flow) are never
	// overwritten here because LoadPeerPublicKey short-circuited above.
	// IsBot is copied from the broker response; it's advisory (see the
	// TrustedKey.IsBot comment) — a UI label, not an authorisation bit.
	_ = keys.SavePeerPublicKey(username, keys.TrustedKey{
		PublicKey: result.PublicKey,
		Verified:  false,
		Method:    keys.TrustMethodTOFU,
		IsBot:     result.IsBot,
	})
	return result.PublicKey, nil
}
