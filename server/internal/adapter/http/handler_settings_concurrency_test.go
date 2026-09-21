package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingSettingsStoreHTTP struct {
	req domain.UpdateSettingsRequest
	got domain.AppSettings
}

func (r *recordingSettingsStoreHTTP) Get(context.Context) (domain.AppSettings, error) { return r.got, nil }

func (r *recordingSettingsStoreHTTP) Update(_ context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	r.req = req
	return r.got, nil
}

func newConcurrencySettingsApp(store *recordingSettingsStoreHTTP) *fiber.App {
	svc := settings.NewService(store)
	h := &Handler{settingsSvc: svc}
	app := fiber.New()
	h.registerSettingsRoutes(app)
	return app
}

func putSettings(t *testing.T, app *fiber.App, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("PUT", "/v1/settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out
}

// The concurrency limits are ints, so the "at least one field" guard must not
// reject a request that only carries max_concurrent_agents.
func TestUpdateSettingsAcceptsConcurrencyLimits(t *testing.T) {
	store := &recordingSettingsStoreHTTP{}
	app := newConcurrencySettingsApp(store)

	status, _ := putSettings(t, app, map[string]any{"max_concurrent_agents": 2, "max_concurrent_tasks": 1})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if store.req.MaxConcurrentAgents == nil || store.req.MaxConcurrentTasks == nil ||
		*store.req.MaxConcurrentAgents != 2 || *store.req.MaxConcurrentTasks != 1 {
		t.Fatalf("handler did not forward limits, got agents=%v tasks=%v", store.req.MaxConcurrentAgents, store.req.MaxConcurrentTasks)
	}
}

// 0 restores the unlimited default; it is a real, wanted value and must pass
// the guard. The empty body must still be rejected.
func TestUpdateSettingsAllowsZeroResetButNotEmptyBody(t *testing.T) {
	store := &recordingSettingsStoreHTTP{}
	app := newConcurrencySettingsApp(store)

	status, _ := putSettings(t, app, map[string]any{"max_concurrent_agents": 0})
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for the 0 reset, got %d", status)
	}
	if store.req.MaxConcurrentAgents == nil || *store.req.MaxConcurrentAgents != 0 {
		t.Fatalf("handler did not forward the 0 reset, got %v", store.req.MaxConcurrentAgents)
	}

	if status, _ := putSettings(t, app, map[string]any{}); status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for an empty body, got %d", status)
	}
	if store.req.MaxConcurrentTasks != nil {
		t.Fatal("empty body must not reach the store")
	}
}