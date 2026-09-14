package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const ToolName = "fetch_url"

const fetchTimeout = 30 * time.Second

// fetchFailed is the only thing a rejected or failed fetch ever says.
//
// It used to be fmt.Sprintf("http error: %v", err), which handed the model Go's
// *url.Error verbatim — and "connection refused" reads differently from "i/o
// timeout", which is a port scanner. The model picks the URL and the model reads
// the answer, so the two halves of the scanner were both inside the loop. A
// blocked destination, a refused connection, a timeout and an unreadable body
// are now indistinguishable from outside; the reason goes to the log.
const fetchFailed = "could not fetch that URL"

type webTool struct {
	maxBytes int64
	policy   urlguard.Policy
}

type args struct {
	URL string `json:"url"`
}

// Option customises the tool at construction.
type Option func(*webTool)

// WithURLPolicy replaces the destination policy. Production takes the default
// (public internet only, loopback per ALLOW_LOOPBACK_TOOL_URLS); tests use this
// to reach an httptest server on loopback.
func WithURLPolicy(p urlguard.Policy) Option {
	return func(w *webTool) { w.policy = p }
}

func New(maxBytes int64, opts ...Option) port.ToolExecutor {
	t := &webTool{maxBytes: maxBytes, policy: urlguard.Default()}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (w *webTool) Name() string {
	return ToolName
}

func (w *webTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        ToolName,
			Description: "Fetch the content of a URL via HTTP GET. Returns the response body as text. For HTML pages, returns extracted readable text.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The full URL to fetch (must start with http:// or https://)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (w *webTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{
			Name:    ToolName,
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}
	}

	if a.URL == "" {
		return domain.ToolResult{
			Name:    ToolName,
			Content: "url is required",
			IsError: true,
		}
	}

	// The model chose this URL, and the text that made it choose came off the
	// open internet — so the destination is attacker-controlled and gets the
	// full guard: scheme allowlist, resolution, address rules, and a dial pinned
	// to the address that was checked. Without it, "url":"http://127.0.0.1:8080/
	// metrics" reaches this pod's own listener, where everything outside /v1 and
	// /admin is served unauthenticated (adapter/http/handler_ui.go).
	target, err := w.policy.Validate(ctx, a.URL)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogRaw(a.URL)).Msg("fetch_url destination refused")
		return domain.ToolResult{Name: ToolName, Content: fetchFailed, IsError: true}
	}

	// Log the destination without its query string: an agent-supplied URL
	// routinely carries a token there, and a log line is the wrong place to keep
	// one. Same reason the model no longer gets the URL echoed back in errors.
	log.Debug().Str("url", urlguard.LogValue(target.URL)).Msg("fetching url")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url request build failed")
		return domain.ToolResult{Name: ToolName, Content: fetchFailed, IsError: true}
	}
	req.Header.Set("User-Agent", "local-llm-bridge/1.0")

	resp, err := w.policy.ClientFor(target, fetchTimeout).Do(req)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url failed")
		return domain.ToolResult{Name: ToolName, Content: fetchFailed, IsError: true}
	}
	defer resp.Body.Close()

	maxBytes := w.maxBytes
	if maxBytes <= 0 {
		maxBytes = 1048576
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url body read failed")
		return domain.ToolResult{Name: ToolName, Content: fetchFailed, IsError: true}
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		content = extractText(content)
	}

	return domain.ToolResult{
		Name:    ToolName,
		Content: fmt.Sprintf("HTTP %d\nContent-Type: %s\n\n%s", resp.StatusCode, contentType, content),
		IsError: false,
	}
}

func extractText(html string) string {
	inTag := false
	var sb strings.Builder

	for i := 0; i < len(html); i++ {
		ch := html[i]
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
			sb.WriteByte(' ')
		case !inTag:
			sb.WriteByte(ch)
		}
	}

	text := sb.String()
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}
