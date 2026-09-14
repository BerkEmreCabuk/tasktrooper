package orchestrator

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// WorkspaceLister supplies the projects and repositories the system already
// knows about. Intake and the planner run without tools, so without this they
// cannot tell whether the team has a repository and fall back to asking the
// stakeholder — which is what list_repositories exists to answer.
type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
}

// SetWorkspace attaches the workspace snapshot source. Optional: without it the
// pipeline keeps working, it just loses the never-ask-about-repos grounding.
func (s *Service) SetWorkspace(w WorkspaceLister) {
	s.workspace = w
}

// SetSessionActions lets each subtask re-read the board-action ledger instead of
// inheriting the snapshot taken when the run started. Optional.
func (s *Service) SetSessionActions(r SessionActionReader) {
	if s.executor != nil {
		s.executor.actions = r
	}
}

// workspaceFacts renders the snapshot for the toolless pipeline prompts. A store
// error degrades to a partial (or empty) snapshot rather than failing the run —
// the worst case is the old behaviour of asking one question too many.
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
