package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	agentcliapp "github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
)

// The number is the point. Each of these answered 500 with a correct, permanent
// sentence in the body, and 500 is the one answer that tells a client and a
// monitor to send the same refused request again.
func TestPermanentProviderRefusalsAnswer409(t *testing.T) {
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, &fakeLLMEndpointStore{}, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	cases := []struct {
		name   string
		method string
		path   string
		code   string
	}{
		// cursor_agent, antigravity and opencode all have executors now (see
		// internal/adapter/agentcli/{cursor,antigravity,opencode}) and are
		// Available:true, so they fall through to the same HostExecuted check
		// claude_code always has — codeProviderUnavailable no longer applies
		// to any provider this route can be asked about.
		{"cursor_agent activate", "POST", "/v1/llm/providers/cursor_agent/activate", codeHostExecutedProvider},
		{"antigravity activate", "POST", "/v1/llm/providers/antigravity/activate", codeHostExecutedProvider},
		{"cursor_agent connect", "POST", "/v1/llm/providers/cursor_agent/connect", codeHostExecutedProvider},
		{"opencode connect", "POST", "/v1/llm/providers/opencode/connect", codeHostExecutedProvider},
		{"opencode activate", "POST", "/v1/llm/providers/opencode/activate", codeHostExecutedProvider},
		{"claude_code connect", "POST", "/v1/llm/providers/claude_code/connect", codeHostExecutedProvider},
		{"claude_code activate", "POST", "/v1/llm/providers/claude_code/activate", codeHostExecutedProvider},
		{"claude_code test", "POST", "/v1/llm/providers/claude_code/test", codeHostExecutedProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusConflict {
				t.Fatalf("status = %d, want 409: %s", resp.StatusCode, string(body))
			}
			var out struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode: %v (%s)", err, string(body))
			}
			// Both places, because tenant-manager writes the code at the top
			// level and this server's own errors carry it in error.type; a
			// client that learned one must not have to learn the other.
			if out.Code != tc.code || out.Error.Type != tc.code {
				t.Fatalf("code = %q / type = %q, want %q", out.Code, out.Error.Type, tc.code)
			}
			if out.Error.Message == "" {
				t.Fatal("the refusal must still say why")
			}
		})
	}
}

// The stale half of that sentence: in cloud claude_code runs on the assigned
// member's Mac, not on the machine this server is on.
func TestHostExecutedRefusalDoesNotClaimTheServerHost(t *testing.T) {
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, &fakeLLMEndpointStore{}, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	req := httptest.NewRequest("POST", "/v1/llm/providers/claude_code/connect", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, stale := range []string{"local process on the server host", "on the server host"} {
		if strings.Contains(string(body), stale) {
			t.Fatalf("refusal still claims %q: %s", stale, string(body))
		}
	}
	if !strings.Contains(string(body), "Mac") {
		t.Fatalf("the refusal should say where it does run: %s", string(body))
	}
}

// decodeCoded reads the two places a code appears. Both, always: tenant-manager
// writes it at the top level and this server's own errors carry it in
// error.type, and a client that learned one must not have to learn the other.
func decodeCoded(t *testing.T, body []byte) (message, typ, code string) {
	t.Helper()
	var out struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, string(body))
	}
	return out.Error.Message, out.Error.Type, out.Code
}

// An incomplete body is the caller's to fix and never becomes valid on a retry.
func TestAgentValidationAnswers400(t *testing.T) {
	h := &Handler{catalogSvc: catalog.NewService(nil, nil, "")}
	app := fiber.New()
	h.registerOrchestrationRoutes(app)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"update with no name", "PUT", "/admin/agents/6f1c1f7e-0d5e-4a6b-9d1a-2f3c4b5a6d7e", `{"description":"x"}`, "name is required"},
		{"create with no name", "POST", "/admin/agents", `{"description":"x"}`, "name is required"},
		{"create with a bad effort", "POST", "/admin/agents", `{"name":"n","effort":"enormous"}`, "effort must be one of"},
		{"create with a negative turn cap", "POST", "/admin/agents", `{"name":"n","max_turns":-1}`, "max_turns cannot be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("body must still say what is wrong, got %s", string(body))
			}
			// The status was already right; the code is what a client can
			// branch on without matching the sentence it also displays.
			_, typ, code := decodeCoded(t, body)
			if typ != codeInvalidCatalogInput || code != codeInvalidCatalogInput {
				t.Fatalf("type = %q / code = %q, want %q", typ, code, codeInvalidCatalogInput)
			}
		})
	}
}

// A flavor that names no CLI at all. Its 400 was right and its body said only
// prose — and the neighbouring refusal on the same route family (a flavor that
// IS known and not built) has answered a coded 409 since permanent_refusal.go
// landed, so a client could tell the two apart only by reading English.
func TestUnknownAgentCLIFlavorIsACoded400(t *testing.T) {
	h := &Handler{agentCLISvc: agentcliapp.NewService(agentcliapp.Deps{})}
	app := fiber.New()
	h.registerAgentCLIRoutes(app)

	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/agent-cli/emacs/connect"},
		{"DELETE", "/v1/agent-cli/emacs"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
			}
			message, typ, code := decodeCoded(t, body)
			if typ != codeUnknownAgentCLIFlavor || code != codeUnknownAgentCLIFlavor {
				t.Fatalf("type = %q / code = %q, want %q", typ, code, codeUnknownAgentCLIFlavor)
			}
			if message == "" {
				t.Fatal("the refusal must still say why")
			}
		})
	}
}
