package billing

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultDisplayRate = 0.000002

type Service struct {
	store      port.BillingStore
	redispatch func(ctx context.Context, repositoryID, taskID uuid.UUID) error
}

func NewService(store port.BillingStore) *Service {
	return &Service{store: store}
}

func (s *Service) SetRedispatcher(fn func(ctx context.Context, repositoryID, taskID uuid.UUID) error) {
	s.redispatch = fn
}

func (s *Service) Plan(ctx context.Context) (domain.BillingPlan, error) {
	return s.store.GetPlan(ctx)
}

func (s *Service) UpdatePlan(ctx context.Context, req domain.UpdateBillingPlanRequest) (domain.BillingPlan, error) {
	return s.store.UpdatePlan(ctx, req)
}

func (s *Service) ListModelPrices(ctx context.Context) ([]domain.ModelPrice, error) {
	return s.store.ListModelPrices(ctx)
}

func (s *Service) UpsertModelPrice(ctx context.Context, price domain.ModelPrice) (domain.ModelPrice, error) {
	return s.store.UpsertModelPrice(ctx, price)
}

func (s *Service) DeleteModelPrice(ctx context.Context, model string) error {
	return s.store.DeleteModelPrice(ctx, model)
}

func (s *Service) Allow(ctx context.Context) (bool, string) {
	plan, err := s.store.GetPlan(ctx)
	if err != nil {
		return true, ""
	}
	if s.ensurePeriod(ctx, plan) {
		if plan, err = s.store.GetPlan(ctx); err != nil {
			return true, ""
		}
	}
	if plan.Unlimited() {
		return true, ""
	}
	usd, err := s.gateSpend(ctx, plan.PeriodStart)
	if err != nil {
		return true, ""
	}
	if usd >= plan.UsdBudget {
		return false, "period token budget exhausted"
	}
	return true, ""
}

type lockingBillingStore interface {
	LockedUsdSpentSince(ctx context.Context, since time.Time) (float64, error)
}

func (s *Service) gateSpend(ctx context.Context, since time.Time) (float64, error) {
	if locking, ok := s.store.(lockingBillingStore); ok {
		return locking.LockedUsdSpentSince(ctx, since)
	}
	return s.store.UsdSpentSince(ctx, since)
}

func (s *Service) PauseTask(ctx context.Context, repositoryID, taskID uuid.UUID) {
	if err := s.store.AddPausedTask(ctx, domain.QuotaPausedTask{TaskID: taskID, RepositoryID: repositoryID}); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("billing: pause task failed")
	}
}

func (s *Service) Status(ctx context.Context) (domain.BillingStatus, error) {
	plan, err := s.store.GetPlan(ctx)
	if err != nil {
		return domain.BillingStatus{}, err
	}
	if s.ensurePeriod(ctx, plan) {
		if plan, err = s.store.GetPlan(ctx); err != nil {
			return domain.BillingStatus{}, err
		}
	}
	usd, err := s.store.UsdSpentSince(ctx, plan.PeriodStart)
	if err != nil {
		return domain.BillingStatus{}, err
	}
	rawTokens, err := s.store.TokensSince(ctx, plan.PeriodStart)
	if err != nil {
		return domain.BillingStatus{}, err
	}
	rate := plan.DisplayTokenRate
	if rate <= 0 {
		rate = defaultDisplayRate
	}
	status := domain.BillingStatus{
		PlanName:      plan.Name,
		Unlimited:     plan.Unlimited(),
		UsdBudget:     plan.UsdBudget,
		UsdSpent:      usd,
		RawTokensUsed: rawTokens,
		PeriodStart:   plan.PeriodStart,
		ResetAt:       plan.ResetAt(),
	}
	status.TokenBudgetUsed = int64(math.Round(usd / rate))
	if !plan.Unlimited() {
		status.TokenBudget = int64(math.Round(plan.UsdBudget / rate))
		status.TokenRemaining = status.TokenBudget - status.TokenBudgetUsed
		if status.TokenRemaining < 0 {
			status.TokenRemaining = 0
		}
		status.Exhausted = usd >= plan.UsdBudget
	}
	return status, nil
}

func (s *Service) Tick(ctx context.Context) {
	plan, err := s.store.GetPlan(ctx)
	if err != nil {
		return
	}
	s.ensurePeriod(ctx, plan)
}

func (s *Service) ensurePeriod(ctx context.Context, plan domain.BillingPlan) bool {
	now := time.Now()
	if now.Before(plan.ResetAt()) {
		return false
	}
	days := plan.PeriodDays
	if days <= 0 {
		days = 30
	}
	newStart := plan.PeriodStart
	for !now.Before(newStart.AddDate(0, 0, days)) {
		newStart = newStart.AddDate(0, 0, days)
	}
	if err := s.store.SetPeriodStart(ctx, newStart); err != nil {
		log.Warn().Err(err).Msg("billing: roll period failed")
		return false
	}
	s.resumePaused(ctx)
	return true
}

func (s *Service) resumePaused(ctx context.Context) {
	tasks, err := s.store.ListPausedTasks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("billing: list paused tasks failed")
		return
	}
	resumed := 0
	for _, t := range tasks {
		if s.redispatch != nil {
			if err := s.redispatch(ctx, t.RepositoryID, t.TaskID); err != nil {
				log.Warn().Err(err).Str("task_id", t.TaskID.String()).Msg("billing: resume redispatch failed, staying paused for the next attempt")
				continue
			}
		}
		if err := s.store.RemovePausedTask(ctx, t.TaskID); err != nil {
			log.Warn().Err(err).Str("task_id", t.TaskID.String()).Msg("billing: clear paused task failed")
			continue
		}
		resumed++
	}
	if resumed > 0 {
		log.Info().Int("count", resumed).Msg("billing: budget renewed, paused tasks resumed")
	}
}
