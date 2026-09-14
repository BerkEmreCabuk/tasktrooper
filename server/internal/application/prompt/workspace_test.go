package prompt_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestWorkspaceFactsBlock_ListsReposWithProjects(t *testing.T) {
	projectID := uuid.New()
	block := prompt.WorkspaceFactsBlock(
		[]domain.InitiativeProject{{ID: projectID, Name: "Acme"}},
		[]domain.Repository{{
			Name:        "acme-web",
			Kind:        domain.RepoKindFrontend,
			Description: "Marketing site",
			ProjectIDs:  []uuid.UUID{projectID},
		}},
	)

	assert.Contains(t, block, "Projects (1): Acme")
	assert.Contains(t, block, "Repositories (1)")
	assert.Contains(t, block, "- acme-web (kind=frontend) — projects: Acme — Marketing site")
	assert.Contains(t, block, "never ask the stakeholder")
	assert.Contains(t, block, "fully accessible to the agent team")
}

func TestWorkspaceFactsBlock_EmptyStillCarriesTheRule(t *testing.T) {
	block := prompt.WorkspaceFactsBlock(nil, nil)

	assert.Contains(t, block, "Projects (0): none registered")
	assert.Contains(t, block, "Repositories (0): none registered")
	assert.Contains(t, block, "Do not ask the stakeholder to supply access details.")
}

func TestWorkspaceFactsBlock_TruncatesLongDescription(t *testing.T) {
	block := prompt.WorkspaceFactsBlock(nil, []domain.Repository{{
		Name:        "big",
		Description: strings.Repeat("x", 400),
	}})

	assert.Contains(t, block, "…")
	assert.NotContains(t, block, strings.Repeat("x", 200))
}

func TestWorkspaceFactsBlock_SkipsUnknownProjectLinks(t *testing.T) {
	block := prompt.WorkspaceFactsBlock(nil, []domain.Repository{{
		Name:       "orphan",
		ProjectIDs: []uuid.UUID{uuid.New()},
	}})

	assert.Contains(t, block, "- orphan")
	assert.NotContains(t, block, "projects:")
}
