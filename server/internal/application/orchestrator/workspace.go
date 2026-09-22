package orchestrator

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Supplies projects and repositories for the toolless intake and planner, so they never ask what the store has.
type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
}

// Optional; the pipeline keeps working, it just loses the never-ask-about-repos grounding.
func (s *Service) SetWorkspace(w WorkspaceLister) {
	s.workspace = w
}

// Lets each subtask re-read the board-action ledger instead of the run-start snapshot. Optional.
func (s *Service) SetSessionActions(r SessionActionReader) {
	if s.executor != nil {
		s.executor.actions = r
	}
}

// Errors degrade to a partial snapshot rather than failing the run — the old "one question too many" behaviour.
func (s *Service) workspaceFacts(ctx context.Context) string {
	if s.workspace == nil {
		return ""
	}
	projects, projErr := s.workspace.ListProjects(ctx)
	if projErr != nil {
		log.Warn().Err(projErr).Msg("workspace snapshot: project list failed")
	}
	repos, repoErr := s.workspace.ListRepositories(ctx)
	if repoErr != nil {
		log.Warn().Err(repoErr).Msg("workspace snapshot: repository list failed")
	}
	if projErr != nil && repoErr != nil {
		return ""
	}
	return prompt.WorkspaceFactsBlock(projects, repos)
}

func WorkspaceFactsForTest(w WorkspaceLister) string {
	s := &Service{}
	if w != nil {
		s.SetWorkspace(w)
	}
	return s.workspaceFacts(context.Background())
}
