// Package deviceagent talks to the adb sidecar running next to Appium
// (cmd/device-agent), so the settings UI can pair and connect a phone without
// anyone opening a shell in the cluster.
package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is one bridge operation's outcome. Detail is adb's own words: "failed
// to connect to 100.x.y.z:5555", "device unauthorized". They are the most
// useful thing the operator can be shown, and paraphrasing them into "could not
// connect" is what makes a phone that is simply waiting for an allow-debugging
// tap look broken.
type Result struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// LocalAddr is the loopback address the bridge mapped this phone onto, and
	// the only authority on it: the bridge allocates a port per device, so a
	// caller that assumed 127.0.0.1:5555 would be right about the first phone
	// and wrong about every one after it. It is what Appium is handed as the
	// udid, which is why a connect is also how a registration learns its UDID.
	LocalAddr string `json:"local_addr"`
}

// Status is what adb currently sees for one address.
type Status struct {
	Online    bool   `json:"online"`
	Detail    string `json:"detail"`
	LocalAddr string `json:"local_addr"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns nil when no bridge is configured — an installation can run the
// device tools against a hub whose adb was wired by hand, and the UI then
// simply cannot offer the pairing step.
func New(baseURL, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// Longer than a normal API call: `adb connect` to a sleeping phone
		// waits on a TCP handshake that the phone's radio may take seconds to
		// answer, and pairing is a round trip through the same radio.
		http: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Configured() bool { return c != nil }

// Pair runs the one-time pairing with the six-digit code the phone is showing.
func (c *Client) Pair(ctx context.Context, addr, code string) (Result, error) {
	var out Result
	err := c.post(ctx, "/pair", map[string]string{"addr": addr, "code": code}, &out)
	return out, err
}

// Connect attaches adb to an already-paired device.
func (c *Client) Connect(ctx context.Context, addr string) (Result, error) {
	var out Result
	err := c.post(ctx, "/connect", map[string]string{"addr": addr}, &out)
	return out, err
}

// Disconnect detaches it. Used when a device is unregistered, so the sidecar
// stops re-dialling a phone the installation no longer claims.
func (c *Client) Disconnect(ctx context.Context, addr string) (Result, error) {
	var out Result
	err := c.post(ctx, "/disconnect", map[string]string{"addr": addr}, &out)
	return out, err
}

// Status asks whether adb currently has this address in the "device" state.
func (c *Client) Status(ctx context.Context, addr string) (Status, error) {
	if c == nil {
		return Status{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/status?addr="+url.QueryEscape(addr), nil)
	if err != nil {
		return Status{}, err
	}
	var out Status
	return out, c.do(req, &out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	if c == nil {
		return fmt.Errorf("no device bridge is configured")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("device bridge unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &errBody)
		if errBody.Error == "" {
			errBody.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("device bridge: %s", errBody.Error)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
