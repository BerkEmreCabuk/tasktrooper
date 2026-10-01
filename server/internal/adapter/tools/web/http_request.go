package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// HTTPRequestToolName is http_request: a loopback-only HTTP client for an
// agent to exercise a service IT started (start_task_preview, a local dev
// server) with a real request — GET for read checks, a non-GET verb for the
// ones fetch_url cannot make.
const HTTPRequestToolName = "http_request"

const (
	httpRequestDefaultTimeout = 10 * time.Second
	httpRequestMaxTimeout     = 30 * time.Second
	// httpRequestMaxBodyBytes caps the response body returned to the model —
	// generous for a JSON API response, far short of fetch_url's 1 MB default
	// because this tool's answer is read and judged inline, not summarized.
	httpRequestMaxBodyBytes = 16 << 10
)

var httpRequestAllowedMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodHead,
	http.MethodOptions,
}

// httpRequestResponseHeaders is the allowlist of response headers worth
// returning; an arbitrary header dump is noise and a leak surface, this tool's
// callers judge status, shape and location, not cache or server banners.
var httpRequestResponseHeaders = []string{
	"Content-Type",
	"Content-Length",
	"Location",
	"Set-Cookie",
	"WWW-Authenticate",
	"Retry-After",
}

func httpMethodAllowed(m string) bool {
	for _, allowed := range httpRequestAllowedMethods {
		if m == allowed {
			return true
		}
	}
	return false
}

type httpRequestTool struct {
	policy urlguard.Policy
}

type httpRequestArgs struct {
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	TimeoutSeconds float64           `json:"timeout_seconds"`
}

// NewHTTPRequestTool builds the loopback HTTP client. It reuses webTool's
// Option so tests can override the policy exactly as fetch_url and
// download_file do; production never overrides urlguard.LoopbackOnly(), which
// — unlike fetch_url's urlguard.Default() — admits no environment switch to
// widen it.
func NewHTTPRequestTool(opts ...Option) port.ToolExecutor {
	w := &webTool{policy: urlguard.LoopbackOnly()}
	for _, opt := range opts {
		opt(w)
	}
	return &httpRequestTool{policy: w.policy}
}

func (t *httpRequestTool) Name() string {
	return HTTPRequestToolName
}

func (t *httpRequestTool) Definition() domain.ToolDefinition {
	methods := append([]string(nil), httpRequestAllowedMethods...)
	sort.Strings(methods)
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: HTTPRequestToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"method": map[string]interface{}{
						"type": "string",
						"enum": methods,
					},
					"url": map[string]interface{}{
						"type": "string",
					},
					"headers": map[string]interface{}{
						"type": "object",
						"additionalProperties": map[string]interface{}{
							"type": "string",
						},
					},
					"body": map[string]interface{}{
						"type": "string",
					},
					"timeout_seconds": map[string]interface{}{
						"type": "number",
					},
				},
				"required": []string{"method", "url"},
			},
		},
	}
}

func (t *httpRequestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a httpRequestArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{Name: HTTPRequestToolName, Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" || strings.TrimSpace(a.URL) == "" {
		return domain.ToolResult{Name: HTTPRequestToolName, Content: "method and url are both required", IsError: true}
	}
	if !httpMethodAllowed(method) {
		return domain.ToolResult{
			Name:    HTTPRequestToolName,
			Content: fmt.Sprintf("method %q is not one of %s", a.Method, strings.Join(httpRequestAllowedMethods, ", ")),
			IsError: true,
		}
	}
	if a.TimeoutSeconds < 0 || a.TimeoutSeconds > httpRequestMaxTimeout.Seconds() {
		return domain.ToolResult{Name: HTTPRequestToolName, Content: "timeout_seconds must be between 0 and 30", IsError: true}
	}
	timeout := httpRequestDefaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(a.TimeoutSeconds * float64(time.Second))
	}

	// The model chose this URL; LoopbackOnly refuses everything but this
	// machine's own 127.0.0.0/8 and ::1 — no SSRF path to the LAN or the
	// public internet, checked again on every redirect hop (CheckRedirect).
	target, err := t.policy.Validate(ctx, a.URL)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogRaw(a.URL)).Msg("http_request destination refused")
		return domain.ToolResult{Name: HTTPRequestToolName, Content: prompt.HTTPRequestLoopbackOnlyText(), IsError: true}
	}

	var bodyReader io.Reader
	if a.Body != "" {
		bodyReader = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.URL.String(), bodyReader)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("http_request build failed")
		return domain.ToolResult{Name: HTTPRequestToolName, Content: prompt.HTTPRequestFailedText(), IsError: true}
	}
	req.Header.Set("User-Agent", "local-llm-bridge/1.0")
	for k, v := range a.Headers {
		if k == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := t.policy.ClientFor(target, timeout).Do(req)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("http_request failed")
		return domain.ToolResult{Name: HTTPRequestToolName, Content: prompt.HTTPRequestFailedText(), IsError: true}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpRequestMaxBodyBytes+1))
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("http_request body read failed")
		return domain.ToolResult{Name: HTTPRequestToolName, Content: prompt.HTTPRequestFailedText(), IsError: true}
	}
	truncated := len(body) > httpRequestMaxBodyBytes
	if truncated {
		body = body[:httpRequestMaxBodyBytes]
	}

	return domain.ToolResult{
		Name:    HTTPRequestToolName,
		Content: formatHTTPRequestResult(resp, body, truncated),
		IsError: false,
	}
}

func formatHTTPRequestResult(resp *http.Response, body []byte, truncated bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", resp.Proto, resp.Status)
	for _, h := range httpRequestResponseHeaders {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&sb, "%s: %s\n", h, v)
		}
	}
	sb.WriteString("\n")
	sb.Write(body)
	if truncated {
		sb.WriteString("\n\n")
		sb.WriteString(prompt.HTTPRequestTruncatedText(httpRequestMaxBodyBytes >> 10))
	}
	return sb.String()
}
