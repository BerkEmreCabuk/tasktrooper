package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubStore struct {
	plan    domain.BillingPlan
	planErr error
	usd     float64

	unlockedReads int
	syncedPlan    domain.BillingPlan
	paused        []domain.QuotaPausedTask
	resumed       []uuid.UUID
}

func (s *stubStore) GetPlan(context.Context) (domain.BillingPlan, error) {
	return s.plan, s.planErr
}

func (s *stubStore) UpdatePlan(context.Context, domain.UpdateBillingPlanRequest) (domain.BillingPlan, error) {
	return s.plan, nil
}

func (s *stubStore) SetPeriodStart(_ context.Context, start time.Time) error {
	s.plan.PeriodStart = start
	return nil
}

func (s *stubStore) SyncPlan(_ context.Context, plan domain.BillingPlan) error {
	s.syncedPlan = plan
	return nil
}

func (s *stubStore) ListModelPrices(context.Context) ([]domain.ModelPrice, error) { return nil, nil }

func (s *stubStore) UpsertModelPrice(_ context.Context, p domain.ModelPrice) (domain.ModelPrice, error) {
	return p, nil
}
func (s *stubStore) DeleteModelPrice(context.Context, string) error                { return nil }
func (s *stubStore) ReplaceModelPrices(context.Context, []domain.ModelPrice) error { return nil }

func (s *stubStore) UsdSpentSince(context.Context, time.Time) (float64, error) {
	s.unlockedReads++
	return s.usd, nil
}
func (s *stubStore) TokensSince(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *stubStore) AddPausedTask(_ context.Context, t domain.QuotaPausedTask) error {
	s.paused = append(s.paused, t)
	return nil
}

func (s *stubStore) ListPausedTasks(context.Context) ([]domain.QuotaPausedTask, error) {
	return s.paused, nil
}
func (s *stubStore) ClearPausedTasks(context.Context) error { return nil }

func (s *stubStore) RemovePausedTask(_ context.Context, id uuid.UUID) error {
	s.resumed = append(s.resumed, id)
	return nil
}

type lockingStubStore struct {
	stubStore
	lockedReads int
}

func (s *lockingStubStore) LockedUsdSpentSince(context.Context, time.Time) (float64, error) {
	s.lockedReads++
	return s.usd, nil
}

func livePlan(budget, days int) domain.BillingPlan {
	return domain.BillingPlan{
		Name:        "pro",
		UsdBudget:   float64(budget),
		PeriodDays:  days,
		PeriodStart: time.Now().Add(-24 * time.Hour),
	}
}

func TestAllowPrefersTheLockedSpendRead(t *testing.T) {
	store := &lockingStubStore{stubStore: stubStore{plan: livePlan(50, 30), usd: 1}}
	if allowed, reason := NewService(store).Allow(context.Background()); !allowed {
		t.Fatalf("allowed = false (%q), want true", reason)
	}
	if store.lockedReads != 1 || store.unlockedReads != 0 {
		t.Fatalf("locked=%d unlocked=%d, want locked=1 unlocked=0", store.lockedReads, store.unlockedReads)
	}
}

func TestAllowFallsBackWhenTheStoreCannotLock(t *testing.T) {
	store := &stubStore{plan: livePlan(50, 30), usd: 50}
	allowed, reason := NewService(store).Allow(context.Background())
	if allowed {
		t.Fatal("allowed = true, want false with the budget spent")
	}
	if reason == "" {
		t.Fatal("refusal carried no reason")
	}
	if store.unlockedReads != 1 {
		t.Fatalf("unlocked reads = %d, want 1", store.unlockedReads)
	}
}

func TestSyncDoesNotResumeWhenThePreviousPlanIsUnreadable(t *testing.T) {
	taskID := uuid.New()
	store := &stubStore{
		plan:    livePlan(50, 30),
		planErr: errors.New("connection refused"),
		paused:  []domain.QuotaPausedTask{{TaskID: taskID, RepositoryID: uuid.New()}},
	}
	svc := NewService(store)

	err := svc.Sync(context.Background(), domain.BillingSyncRequest{
		Plan: domain.BillingPlan{Name: "pro", UsdBudget: 50, PeriodDays: 30, PeriodStart: time.Now()},
	})
	if err != nil {
		t.Fatalf("Sync returned %v, want nil", err)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resumed %d task(s) on an unreadable previous plan, want 0", len(store.resumed))
	}
	if store.syncedPlan.Name != "pro" {
		t.Fatalf("plan was not synced: %+v", store.syncedPlan)
	}
}

func TestSyncResumesOnARealRenewal(t *testing.T) {
	taskID := uuid.New()
	store := &stubStore{
		plan:   livePlan(50, 30),
		paused: []domain.QuotaPausedTask{{TaskID: taskID, RepositoryID: uuid.New()}},
	}

	err := NewService(store).Sync(context.Background(), domain.BillingSyncRequest{
		Plan: domain.BillingPlan{Name: "pro", UsdBudget: 50, PeriodDays: 30, PeriodStart: time.Now()},
	})
	if err != nil {
		t.Fatalf("Sync returned %v, want nil", err)
	}
	if len(store.resumed) != 1 || store.resumed[0] != taskID {
		t.Fatalf("resumed = %v, want [%v]", store.resumed, taskID)
	}
}
