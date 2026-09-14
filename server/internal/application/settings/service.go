package settings

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store port.SettingsStore
}

func NewService(store port.SettingsStore) *Service {
	return &Service{store: store}
}

func (s *Service) Get(ctx context.Context) (domain.AppSettings, error) {
	return s.store.Get(ctx)
}

func (s *Service) Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return s.store.Update(ctx, req)
}
