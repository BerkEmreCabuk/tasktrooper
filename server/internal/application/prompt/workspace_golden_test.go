package prompt_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestGoldenWorkspaceFactsBlock(t *testing.T) {
	assertGolden(t, "workspace_facts_empty", prompt.WorkspaceFactsBlock(nil, nil, nil, nil))

	projectID := uuid.New()
	assertGolden(t, "workspace_facts_repo_with_project", prompt.WorkspaceFactsBlock(
		[]domain.InitiativeProject{{ID: projectID, Name: "Acme"}},
		[]domain.Repository{{
			Name:        "acme-web",
			Kind:        domain.RepoKindFrontend,
			Description: "Marketing site",
			ProjectIDs:  []uuid.UUID{projectID},
		}},
		nil, nil,
	))

	assertGolden(t, "workspace_facts_truncated_description", prompt.WorkspaceFactsBlock(nil, []domain.Repository{{
		Name:        "big",
		Description: strings.Repeat("x", 400),
	}}, nil, nil))

	repoID := uuid.New()
	monoID := uuid.New()
	assertGolden(t, "workspace_facts_types_and_components", prompt.WorkspaceFactsBlock(
		[]domain.InitiativeProject{{ID: projectID, Name: "Acme"}},
		[]domain.Repository{
			{ID: repoID, Name: "acme-api"},
			{ID: monoID, Name: "acme-mono"},
		},
		map[uuid.UUID]domain.ProjectType{projectID: domain.ProjectTypeSingle},
		map[uuid.UUID][]domain.ComponentSummary{
			repoID: {{Path: ".", Role: domain.ComponentRoleBackend}},
			monoID: {
				{Path: "apps/web", Role: domain.ComponentRoleFrontend},
				{Path: "apps/api", Role: domain.ComponentRoleBackend},
			},
		},
	))
}
