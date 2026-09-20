package postgres

import (
	"encoding/json"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeTemplateCatalogReadsBothStoredShapes(t *testing.T) {
	// Written before tech stacks existed: a bare array of skills.
	var legacy domain.AgentTemplate
	decodeTemplateCatalog([]byte(`[{"name":"a","description":"d","content":"body","enabled":true}]`), &legacy)
	require.Len(t, legacy.Skills, 1)
	assert.Equal(t, "a", legacy.Skills[0].Name)
	assert.Empty(t, legacy.Skills[0].TechStack)
	assert.Empty(t, legacy.TechStacks)

	current, err := json.Marshal(templateCatalog{
		Skills:     []domain.TemplateSkill{{Name: "a", Content: "body", Enabled: true, TechStack: "Django"}},
		TechStacks: []domain.CreateTechStackRequest{{Name: "Django", Description: "DRF backend", Position: 1}},
	})
	require.NoError(t, err)

	var tpl domain.AgentTemplate
	decodeTemplateCatalog(current, &tpl)
	require.Len(t, tpl.Skills, 1)
	assert.Equal(t, "Django", tpl.Skills[0].TechStack)
	require.Len(t, tpl.TechStacks, 1)
	assert.Equal(t, "DRF backend", tpl.TechStacks[0].Description)

	var empty domain.AgentTemplate
	decodeTemplateCatalog(nil, &empty)
	assert.Empty(t, empty.Skills)
	assert.Empty(t, empty.TechStacks)
}
