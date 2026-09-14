package http

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// modelsFor drives the endpoint the web's listModels() already calls, with the
// route mounted directly so the test is about the handler rather than about the
// auth middleware in front of it.
func modelsFor(t *testing.T, h *Handler, provider string) (int, modelsResponse) {
	t.Helper()
	app := fiber.New()
	app.Get("/v1/models", h.Models)

	url := "/v1/models"
	if provider != "" {
		url += "?provider=" + provider
	}
	resp, err := app.Test(httptest.NewRequest("GET", url, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var out modelsResponse
	if resp.StatusCode == fiber.StatusOK {
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode response: %v (%s)", err, body)
		}
	}
	return resp.StatusCode, out
}

// claude_code is a binary on this host, not an endpoint, so there is no client
// in the multi-provider map for it and nothing to ask. Before this it fell
// through to that map and the picker got "provider is not configured" — which is
// why the web offered a free-text box in the first place.
//
// The handler is built with a nil llmClient on purpose: reaching the client at
// all for this provider is the bug.
func TestModelsServesTheCuratedListForClaudeCode(t *testing.T) {
	status, out := modelsFor(t, &Handler{}, string(domain.LLMProviderClaudeCode))

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for a host-executed provider, got %d", status)
	}
	if out.Object != "list" {
		t.Fatalf("expected the same envelope every other provider returns, got %q", out.Object)
	}

	want := domain.ClaudeCodeModels()
	if len(out.Data) != len(want) {
		t.Fatalf("expected %d options, got %d: %+v", len(want), len(out.Data), out.Data)
	}
	for i, opt := range want {
		if out.Data[i].ID != opt.ID {
			t.Fatalf("option %d: expected id %q, got %q", i, opt.ID, out.Data[i].ID)
		}
		if out.Data[i].Label != opt.Label {
			t.Fatalf("option %d: expected label %q, got %q", i, opt.Label, out.Data[i].Label)
		}
		if out.Data[i].Object != "model" {
			t.Fatalf("option %d: the envelope must stay OpenAI-shaped, got %q", i, out.Data[i].Object)
		}
	}
}

// The first option is the one that means "send no --model", and it has to keep
// meaning that: an empty id is what claudecode.buildArgs reads as "omit the
// flag", which hands the choice to whatever the operator's own CLI is set to.
// A picker whose default silently pinned a model would take that away.
func TestClaudeCodeModelsLeadWithTheCLIDefault(t *testing.T) {
	models := domain.ClaudeCodeModels()
	if len(models) == 0 {
		t.Fatal("the picker cannot be empty")
	}
	if models[0].ID != "" {
		t.Fatalf("the first option must be the CLI default (empty id), got %q", models[0].ID)
	}
	if models[0].Label == "" {
		t.Fatal("the empty id is unreadable without a label; that is what Label is for")
	}
}

// Every other option must be a non-empty alias, unique, and labelled. Uniqueness
// matters because these are <option value> keys in the web picker: a duplicate
// would make one of them unselectable.
func TestClaudeCodeModelsAreUniqueAndLabelled(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range domain.ClaudeCodeModels() {
		if seen[m.ID] {
			t.Fatalf("duplicate model id %q", m.ID)
		}
		seen[m.ID] = true
		if m.Label == "" {
			t.Fatalf("model %q has no label", m.ID)
		}
	}
	// The aliases probed as working against the installed CLI. haiku[1m] and
	// fable[1m] are deliberately absent — the first is refused by the API ("the
	// long context beta is not yet available for this subscription") and the
	// second is silently downgraded to plain fable — so an option that always
	// fails, or that quietly does something other than what it says, is never
	// offered. See domain.ClaudeCodeModels for the full probe transcript.
	for _, want := range []string{"opus", "sonnet", "haiku", "fable", "opus[1m]", "sonnet[1m]"} {
		if !seen[want] {
			t.Fatalf("expected the picker to offer %q", want)
		}
	}
	for _, unwanted := range []string{"haiku[1m]", "fable[1m]", "default"} {
		if seen[unwanted] {
			t.Fatalf("%q must not be offered; see domain.ClaudeCodeModels", unwanted)
		}
	}
}

// A provider that IS an endpoint must be unaffected: it still goes to the
// multi-provider client, and with none wired the handler still says so rather
// than inventing a list.
func TestModelsStillRefusesAnUnresolvableProvider(t *testing.T) {
	status, _ := modelsFor(t, &Handler{}, string(domain.LLMProviderOpenAI))
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 when the multi client cannot be resolved, got %d", status)
	}
}
