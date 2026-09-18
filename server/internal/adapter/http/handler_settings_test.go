package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSettingsStoreHTTP struct {
	got         domain.AppSettings
	updateCalls int
}

func (f *fakeSettingsStoreHTTP) Get(context.Context) (domain.AppSettings, error) { return f.got, nil }

func (f *fakeSettingsStoreHTTP) Update(context.Context, domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	f.updateCalls++
	return f.got, nil
}

// fakeAgentCatalogHTTP satisfies both application/settings.AgentCatalog and
// workflow.AgentCatalog — shared with handler_workflow_test.go's
// role-assignment tool-grant tests.
type fakeAgentCatalogHTTP struct {
	agents map[string]domain.Agent
}

func (f *fakeAgentCatalogHTTP) ListAgents(context.Context) ([]domain.Agent, error) {
	out := make([]domain.Agent, 0, len(f.agents))
	for _, a := range f.agents {
		out = append(out, a)
	}
	return out, nil
}

// GetAgent satisfies workflow.AgentCatalog too (reused by
// handler_workflow_test.go's role-assignment tool-grant tests).
func (f *fakeAgentCatalogHTTP) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	for _, a := range f.agents {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.Agent{}, notFoundErrHTTP{}
}

func (f *fakeAgentCatalogHTTP) UpdateAgent(_ context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error) {
	updated := domain.Agent{ID: id, Name: req.Name, ToolPolicy: req.ToolPolicy}
	for name, a := range f.agents {
		if a.ID == id {
			f.agents[name] = updated
		}
	}
	return updated, nil
}

func newAnalizAssignmentTestApp(store *fakeSettingsStoreHTTP, catalog *fakeAgentCatalogHTTP) (*fiber.App, *Handler) {
	svc := settings.NewService(store)
	if catalog != nil {
		svc.SetAgentCatalog(catalog)
	}
	h := &Handler{settingsSvc: svc}
	app := fiber.New()
	h.registerSettingsRoutes(app)
	return app, h
}

func putAnalizAssignment(t *testing.T, app *fiber.App, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest("PUT", "/v1/settings/analiz-assignment", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, out
}

// TestUpdateAnalizAssignmentGone pins the retired route's replacement: it
// answers 410 with a hint pointing at /v1/roles instead of writing anything,
// regardless of body or whether a settings service is even wired.
func TestUpdateAnalizAssignmentGone(t *testing.T) {
	store := &fakeSettingsStoreHTTP{}
	app, _ := newAnalizAssignmentTestApp(store, nil)

	status, out := putAnalizAssignment(t, app, map[string]any{"backend": "system-architect"})

	if status != fiber.StatusGone {
		t.Fatalf("expected 410, got %d: %v", status, out)
	}
	if store.updateCalls != 0 {
		t.Fatalf("expected nothing written, got %d update calls", store.updateCalls)
	}
	errBody, _ := out["error"].(map[string]any)
	message, _ := errBody["message"].(string)
	if message == "" {
		t.Fatalf("expected an error message hinting at /v1/roles, got %v", out)
	}
}
