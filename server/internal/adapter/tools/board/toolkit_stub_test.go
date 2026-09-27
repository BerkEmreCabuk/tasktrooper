package board

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fullStubKit wires just enough of ToolKit — Tasks, Workspace, Team,
// Attachments, PullRequests — for NewExecutors to register all 43 board
// tools (see the nil-gates in tools.go). Definition() never reads a kit
// field (only Execute does), so every field here is inert: it exists to
// satisfy an interface, not to answer a call. Shared by the catalog-doc
// generator (gendocs_test.go) and the tests that pin its output.
func fullStubKit() *ToolKit {
	return &ToolKit{
		Tasks:        &fakeTaskManager{},
		Workspace:    stubWorkspaceLister{},
		Team:         stubTeamLister{},
		Attachments:  &fakeAttachments{},
		PullRequests: &fakeTaskPullRequests{},
	}
}

type stubWorkspaceLister struct{}

func (stubWorkspaceLister) ListProjects(context.Context) ([]domain.InitiativeProject, error) {
	return nil, nil
}
func (stubWorkspaceLister) ListRepositories(context.Context) ([]domain.Repository, error) {
	return nil, nil
}
func (stubWorkspaceLister) CreateProject(context.Context, string, string) (domain.InitiativeProject, error) {
	return domain.InitiativeProject{}, nil
}
func (stubWorkspaceLister) UpdateProject(context.Context, uuid.UUID, string, string) (domain.InitiativeProject, error) {
	return domain.InitiativeProject{}, nil
}
func (stubWorkspaceLister) SetRepositoryProjects(context.Context, uuid.UUID, []uuid.UUID) (domain.Repository, error) {
	return domain.Repository{}, nil
}

type stubTeamLister struct{}

func (stubTeamLister) ListAgents(context.Context) ([]domain.Agent, error) { return nil, nil }
