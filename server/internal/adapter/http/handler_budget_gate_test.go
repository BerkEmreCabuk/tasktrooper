package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/billing"
	"github.com/makifbaysal/tasktrooper/server/internal/application/job"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeBillingStore is a plan plus a spend figure — everything the budget gate
// reads and nothing else.
type fakeBillingStore struct {
	plan domain.BillingPlan
	usd  float64
}

func (f *fakeBillingStore) GetPlan(context.Context) (domain.BillingPlan, error) { return f.plan, nil }

func (f *fakeBillingStore) UpdatePlan(context.Context, domain.UpdateBillingPlanRequest) (domain.BillingPlan, error) {
	return f.plan, nil
}
func (f *fakeBillingStore) SetPeriodStart(_ context.Context, start time.Time) error {
	f.plan.PeriodStart = start
	return nil
}
func (f *fakeBillingStore) SyncPlan(_ context.Context, plan domain.BillingPlan) error {
	f.plan = plan
	return nil
}
func (f *fakeBillingStore) ListModelPrices(context.Context) ([]domain.ModelPrice, error) {
	return nil, nil
}
func (f *fakeBillingStore) UpsertModelPrice(_ context.Context, p domain.ModelPrice) (domain.ModelPrice, error) {
	return p, nil
}
func (f *fakeBillingStore) DeleteModelPrice(context.Context, string) error                { return nil }
func (f *fakeBillingStore) ReplaceModelPrices(context.Context, []domain.ModelPrice) error { return nil }
func (f *fakeBillingStore) UsdSpentSince(context.Context, time.Time) (float64, error) {
	return f.usd, nil
}
func (f *fakeBillingStore) TokensSince(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeBillingStore) AddPausedTask(context.Context, domain.QuotaPausedTask) error {
	return nil
}
func (f *fakeBillingStore) ListPausedTasks(context.Context) ([]domain.QuotaPausedTask, error) {
	return nil, nil
}
func (f *fakeBillingStore) ClearPausedTasks(context.Context) error            { return nil }
func (f *fakeBillingStore) RemovePausedTask(context.Context, uuid.UUID) error { return nil }

func billingSvcWithSpend(budget, spent float64) *billing.Service {
	return billing.NewService(&fakeBillingStore{
		plan: domain.BillingPlan{
			Name:        "pro",
			UsdBudget:   budget,
			PeriodDays:  30,
			PeriodStart: time.Now().Add(-24 * time.Hour),
		},
		usd: spent,
	})
}

func TestBudgetGate(t *testing.T) {
	newApp := func(svc *billing.Service) *fiber.App {
		h := &Handler{billingSvc: svc}
		app := fiber.New()
		app.Post("/run", func(c *fiber.Ctx) error {
			if err := h.budgetGate(c); err != nil {
				return err
			}
			return c.SendString("started")
		})
		return app
	}

	t.Run("exhausted budget is refused with 402", func(t *testing.T) {
		resp, err := newApp(billingSvcWithSpend(50, 50)).Test(httptest.NewRequest("POST", "/run", nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402", resp.StatusCode)
		}
	})

	t.Run("budget left runs", func(t *testing.T) {
		resp, err := newApp(billingSvcWithSpend(50, 1)).Test(httptest.NewRequest("POST", "/run", nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Self-hosted and desktop runs pay their own provider bills; billing is not
	// wired there and the gate must not turn into a wall.
	t.Run("billing not configured is a no-op", func(t *testing.T) {
		resp, err := newApp(nil).Test(httptest.NewRequest("POST", "/run", nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}

// The board runner enforced the budget from the start, but the documented API
// entry points started the very same agent loop without a check — so a key
// holder could bill past the plan by never touching a board. Each case drives
// the real handler and expects the refusal before any work is dispatched.
func TestAgentEntryPointsEnforceBudget(t *testing.T) {
	post := func(t *testing.T, h *Handler, register func(*fiber.App), method, path, body string) int {
		t.Helper()
		app := fiber.New()
		register(app)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	t.Run("POST /v1/chat/completions", func(t *testing.T) {
		h := &Handler{billingSvc: billingSvcWithSpend(5, 5)}
		status := post(t, h, func(app *fiber.App) {
			app.Post("/v1/chat/completions", h.ChatCompletions)
		}, "POST", "/v1/chat/completions", `{"messages":[{"role":"user","content":"hi"}]}`)
		if status != fiber.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402", status)
		}
	})

	t.Run("POST /v1/sessions/:id/messages", func(t *testing.T) {
		// Non-nil session service only so the handler reaches the gate rather
		// than short-circuiting on "sessions not enabled"; it is never called.
		h := &Handler{billingSvc: billingSvcWithSpend(5, 5), sessionSvc: &session.Service{}}
		status := post(t, h, func(app *fiber.App) {
			app.Post("/v1/sessions/:id/messages", h.SessionMessage)
		}, "POST", "/v1/sessions/"+uuid.New().String()+"/messages", `{"content":"hi"}`)
		if status != fiber.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402", status)
		}
	})

	t.Run("POST /v1/jobs", func(t *testing.T) {
		h := &Handler{billingSvc: billingSvcWithSpend(5, 5), jobSvc: &job.Service{}}
		status := post(t, h, func(app *fiber.App) {
			app.Post("/v1/jobs", h.CreateJob)
		}, "POST", "/v1/jobs", `{"messages":[{"role":"user","content":"hi"}]}`)
		if status != fiber.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402", status)
		}
	})
}
