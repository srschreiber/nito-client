// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/keys"
)

var pendingRegisterBroker string
var pendingRegisterUsername string

func registerCmd(args []Argument) (Signal, error) {
	brokerURL := extractArg(args, "b", "broker")
	if brokerURL == "" {
		return SignalNone, errors.New("register: -b/--broker <url> is required")
	}
	username := extractArg(args, "u", "user")
	if username == "" {
		return SignalNone, errors.New("register: -u/--user <username> is required")
	}
	pendingRegisterBroker = brokerURL
	pendingRegisterUsername = username
	return SignalNeedRegisterPassword, nil
}

// CompleteRegister finishes the register flow with the password the user entered.
func CompleteRegister(ctx context.Context, password string) (string, Signal, error) {
	broker := pendingRegisterBroker
	username := pendingRegisterUsername
	pendingRegisterBroker = ""
	pendingRegisterUsername = ""

	keys.SetActiveBroker(broker)
	publicKey, err := keys.LoadOrGenerate(username)
	if err != nil {
		return "", SignalNone, fmt.Errorf("register: key setup failed: %w", err)
	}
	resp, err := connection.Register(ctx, broker, username, password, publicKey)
	if err != nil {
		return "", SignalNone, err
	}
	if resp.AlreadyRegistered {
		return fmt.Sprintf("user %q already registered (id: %s)", username, resp.ID), SignalNone, nil
	}
	return fmt.Sprintf("registered %q successfully (id: %s)", username, resp.ID), SignalNone, nil
}

// pendingLogin holds broker/username across the two-step login flow (args → password prompt).
var pendingLoginBroker string
var pendingLoginUsername string

// loginCmd parses login arguments and signals the TUI to prompt for a hidden password.
func loginCmd(args []Argument) (Signal, error) {
	brokerURL := extractArg(args, "b", "broker")
	if brokerURL == "" {
		return SignalNone, errors.New("login: -b/--broker <url> is required")
	}
	username := extractArg(args, "u", "user")
	if username == "" {
		return SignalNone, errors.New("login: -u/--user <username> is required")
	}
	keys.SetActiveBroker(brokerURL)
	if !keys.HaveKeys(username) {
		return SignalNone, errors.New("login: no keys found — run register first")
	}
	pendingLoginBroker = brokerURL
	pendingLoginUsername = username
	return SignalNeedPassword, nil
}

// CompleteLogin finishes the login flow with the password the user entered.
func CompleteLogin(ctx context.Context, password string) (string, Signal, error) {
	broker := pendingLoginBroker
	username := pendingLoginUsername
	pendingLoginBroker = ""
	pendingLoginUsername = ""

	token, err := connection.Login(ctx, broker, username, password)
	if err != nil {
		return "", SignalNone, err
	}
	if err := connection.Connect(ctx, broker, username, token); err != nil {
		return "", SignalNone, fmt.Errorf("connect: %w", err)
	}
	return fmt.Sprintf("logged in to %s as %q", connection.BrokerURL(), username), SignalConnected, nil
}

// LoginDirect performs the full login flow synchronously with known credentials.
func LoginDirect(ctx context.Context, broker, username, password string) (string, Signal, error) {
	pendingLoginBroker = broker
	pendingLoginUsername = username
	return CompleteLogin(ctx, password)
}

// RegisterDirect registers a new user then immediately logs them in.
func RegisterDirect(ctx context.Context, broker, username, password string) (string, Signal, error) {
	pendingRegisterBroker = broker
	pendingRegisterUsername = username
	regMsg, _, err := CompleteRegister(ctx, password)
	if err != nil {
		return "", SignalNone, err
	}
	pendingLoginBroker = broker
	pendingLoginUsername = username
	loginMsg, sig, err := CompleteLogin(ctx, password)
	if err != nil {
		return regMsg, SignalNone, err
	}
	return regMsg + "\n" + loginMsg, sig, nil
}

func ping(args []Argument) (string, error) {
	brokerURL := extractArg(args, "b", "broker")
	if brokerURL == "" {
		return "", errors.New("ping: -b/--broker <url> is required")
	}

	// Normalize: strip any scheme prefix, then prepend ws://
	brokerURL = strings.TrimPrefix(brokerURL, "ws://")
	brokerURL = strings.TrimPrefix(brokerURL, "wss://")
	brokerURL = strings.TrimPrefix(brokerURL, "http://")
	brokerURL = strings.TrimPrefix(brokerURL, "https://")
	wsURL := "ws://" + brokerURL + "/ws/ping"

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("ping: failed to connect to %s: %w", wsURL, err)
	}
	defer conn.Close()

	payload, _ := json.Marshal(map[string]string{"message": "ping"})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return "", fmt.Errorf("ping: write error: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("ping: read error: %w", err)
	}

	return fmt.Sprintf("pong from %s: %s", brokerURL, string(msg)), nil
}
