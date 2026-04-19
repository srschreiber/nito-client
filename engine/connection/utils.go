// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/srschreiber/nito-client/engine/keys"
)

// normalizeURL strips any scheme prefix so downstream code can format ws://
// or http:// itself without double-prefixing.
func normalizeURL(url string) string {
	url = strings.TrimPrefix(url, "ws://")
	url = strings.TrimPrefix(url, "wss://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	return url
}

// signedPost builds a POST request with X-Username, X-Signature, and Authorization headers.
// apiPath is the bare path (e.g. "/api/v0/rooms") used as the signature payload.
func signedPost(url, username, apiPath string, body []byte) (*http.Response, error) {
	sig, err := keys.Sign(username+":"+apiPath, username)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Username", username)
	req.Header.Set("X-Signature", sig)
	if s := CurrentSession(); s != nil && s.JWTToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.JWTToken)
	}
	return http.DefaultClient.Do(req)
}

// signedGet builds a GET request with X-Username, X-Signature, and Authorization headers.
func signedGet(url, username, apiPath string) (*http.Response, error) {
	sig, err := keys.Sign(username+":"+apiPath, username)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Username", username)
	req.Header.Set("X-Signature", sig)
	if s := CurrentSession(); s != nil && s.JWTToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.JWTToken)
	}
	return http.DefaultClient.Do(req)
}
