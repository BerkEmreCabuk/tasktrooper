package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memTemplateStore struct {
	templates []domain.AgentTemplate
}

func (m *memTemplateStore) List(context.Context) ([]domain.AgentTemplate, error) {
	return append([]domain.AgentTemplate(nil), m.templates...), nil
}

func (m *memTemplateStore) Get(_ context.Context, id uuid.UUID) (domain.AgentTemplate, error) {
	for _, tpl := range m.templates {
		if tpl.ID == id {
			return tpl, nil
		}
	}
	return domain.AgentTemplate{}, assert.AnError
}

func (m *memTemplateStore) UpsertByName(_ context.Context, tpl domain.AgentTemplate) (domain.AgentTemplate, error) {
	if tpl.ID == uuid.Nil {
		tpl.ID = uuid.New()
	}
	for i, existing := range m.templates {
		if existing.Name == tpl.Name {
			tpl.ID = existing.ID
			m.templates[i] = tpl
			return tpl, nil
		}
	}
	m.templates = append(m.templates, tpl)
	return tpl, nil
}

func (m *memTemplateStore) Delete(context.Context, uuid.UUID) error { return nil }

func TestSaveAgentAsTemplateSnapshotsTechStacksByName(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(&memTemplateStore{})
	ctx := context.Background()

	source, err := svc.CreateAgent(ctx, domain.CreateAgentRequest{Name: "backend", Enabled: true})
	require.NoError(t, err)
	django, err := svc.CreateTechStackForAgent(ctx, source.ID, domain.CreateTechStackRequest{
		Name: "Django", Description: "DRF backend", Position: 1,
	})
	require.NoError(t, err)
	_, err = svc.CreateTechStackForAgent(ctx, source.ID, domain.CreateTechStackRequest{Name: "Flutter"})
	require.NoError(t, err)
	_, err = svc.CreateSkillForAgent(ctx, source.ID, domain.CreateSkillRequest{
		Name: "drf-viewsets", Description: "viewsets", Content: "body", Enabled: true, TechStackID: &django.ID,
	})
	require.NoError(t, err)
	_, err = svc.CreateSkillForAgent(ctx, source.ID, domain.CreateSkillRequest{
		Name: "code-review", Description: "any language", Content: "body", Enabled: true,
	})
	require.NoError(t, err)

	tpl, err := svc.SaveAgentAsTemplate(ctx, source.ID)
	require.NoError(t, err)
	require.Len(t, tpl.TechStacks, 2)
	assert.Equal(t, "Django", tpl.TechStacks[0].Name)
	assert.Equal(t, "DRF backend", tpl.TechStacks[0].Description)
	byName := map[string]domain.TemplateSkill{}
	for _, sk := range tpl.Skills {
		byName[sk.Name] = sk
	}
	assert.Equal(t, "Django", byName["drf-viewsets"].TechStack)
	assert.Empty(t, byName["code-review"].TechStack, "a general skill names no stack")

	// The new agent gets its own stack rows, and the skills land back on them.
	copied, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{Name: "backend-copy"})
	require.NoError(t, err)
	stacks, err := svc.ListTechStacksForAgent(ctx, copied.ID)
	require.NoError(t, err)
	require.Len(t, stacks, 2, "an empty stack is copied too")
	copiedStackID := map[string]uuid.UUID{}
	for _, st := range stacks {
		copiedStackID[st.Name] = st.ID
		assert.NotEqual(t, django.ID, st.ID, "the copy owns its own stack rows")
	}
	skills, err := svc.ListSkillsByAgent(ctx, copied.ID)
	require.NoError(t, err)
	require.Len(t, skills, 2)
	for _, sk := range skills {
		switch sk.Name {
		case "drf-viewsets":
			require.NotNil(t, sk.TechStackID)
			assert.Equal(t, copiedStackID["Django"], *sk.TechStackID)
		case "code-review":
			assert.Nil(t, sk.TechStackID)
		}
	}
}

// A template stored before tech stacks existed carries neither list; it must
// still produce an agent, with every skill general.
func TestCreateAgentFromTemplateWithoutTechStacks(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	templates := &memTemplateStore{}
	svc.SetTemplateStore(templates)
	ctx := context.Background()

	tpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name:   "legacy",
		Skills: []domain.TemplateSkill{{Name: "old-skill", Description: "d", Content: "body", Enabled: true}},
	})
	require.NoError(t, err)

	agent, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	stacks, err := svc.ListTechStacksForAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.Empty(t, stacks)
	skills, err := svc.ListSkillsByAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Nil(t, skills[0].TechStackID)
}
