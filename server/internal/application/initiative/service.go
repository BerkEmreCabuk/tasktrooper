package initiative

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	projects port.InitiativeProjectStore
}

func NewService(projects port.InitiativeProjectStore) *Service {
	return &Service{projects: projects}
}

func (s *Service) List(ctx context.Context) ([]domain.InitiativeProject, error) {
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	if projects == nil {
		projects = []domain.InitiativeProject{}
	}
	return projects, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error) {
	return s.projects.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, req domain.CreateInitiativeProjectRequest) (domain.InitiativeProject, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return domain.InitiativeProject{}, fmt.Errorf("name is required")
	}
	return s.projects.Create(ctx, name, strings.TrimSpace(req.Description))
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.UpdateInitiativeProjectRequest) (domain.InitiativeProject, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return domain.InitiativeProject{}, err
	}
	return s.projects.Update(ctx, id, req.Name, req.Description)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.projects.Delete(ctx, id)
}
