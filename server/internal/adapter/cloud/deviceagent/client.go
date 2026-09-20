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

type Result struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
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

func New(baseURL, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Configured() bool { return c != nil }

func (c *Client) Pair(ctx context.Context, addr, code string) (Result, error) {
	var out Result
	err := c.post(ctx, "/pair", map[string]string{"addr": addr, "code": code}, &out)
	return out, err
}

func (c *Client) Connect(ctx context.Context, addr string) (Result, error) {
	var out Result
	err := c.post(ctx, "/connect", map[string]string{"addr": addr}, &out)
	return out, err
}

func (c *Client) Disconnect(ctx context.Context, addr string) (Result, error) {
	var out Result
	err := c.post(ctx, "/disconnect", map[string]string{"addr": addr}, &out)
	return out, err
}

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
