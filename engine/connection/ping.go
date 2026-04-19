package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// pingClient uses a short timeout so a stalled broker doesn't hold up the ping loop.
var pingClient = &http.Client{Timeout: 5 * time.Second}

// PingBroker sends a ping to the broker's HTTP API and returns an error if unreachable.
// Returns an error immediately if there is no active session.
func PingBroker() error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{"message": "ping"})
	resp, err := pingClient.Post(s.v0("/ping"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping: broker returned %s", resp.Status)
	}
	return nil
}
