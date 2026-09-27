package skill

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGuardProseUnchanged pins the exact wording load_skill/create_skill hand
// back to the model, ahead of moving it into catalog/system.
func TestGuardProseUnchanged(t *testing.T) {
	agentID := uuid.New()

	// load_skill: not found, with the available list.
	kit := &ToolKit{Catalog: &fakeCatalog{
		agent:  domain.Agent{ID: agentID},
		skills: []domain.Skill{{Name: "release checklist", Enabled: true}, {Name: "qa checklist", Enabled: true}},
	}}
	res := NewExecutors(kit)[0].Execute(agentCtx(agentID), `{"skill":"deploy checklist"}`)
	if want := "skill not found: deploy checklist. Available: release checklist, qa checklist"; res.Content != want {
		t.Errorf("load_skill not-found = %q, want %q", res.Content, want)
	}

	// create_skill: self-evolution disabled.
	kit2 := &ToolKit{Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: false}}, Creator: &fakeCreator{}}
	res2 := NewExecutors(kit2)[1].Execute(agentCtx(agentID), createArgs("deploy checklist"))
	if want := "self-evolution is disabled for this agent; you cannot create skills. Work with the skills you have."; res2.Content != want {
		t.Errorf("create_skill disabled = %q, want %q", res2.Content, want)
	}

	// create_skill: max skills reached.
	kit3 := &ToolKit{
		Catalog:   &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}, skills: []domain.Skill{{Name: "a"}, {Name: "b"}}},
		Creator:   &fakeCreator{},
		MaxSkills: 2,
	}
	res3 := NewExecutors(kit3)[1].Execute(agentCtx(agentID), createArgs("deploy checklist"))
	if want := "you already hold 2 skills, the maximum for one agent. Load the closest existing skill and work from it; a new skill can only be added after your next reflection merges or retires an old one."; res3.Content != want {
		t.Errorf("create_skill max-skills = %q, want %q", res3.Content, want)
	}

	// create_skill: name already exists.
	kit4 := &ToolKit{
		Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}, skills: []domain.Skill{{Name: "deploy checklist"}}},
		Creator: &fakeCreator{},
	}
	res4 := NewExecutors(kit4)[1].Execute(agentCtx(agentID), createArgs("deploy checklist"))
	if want := `a skill named "deploy checklist" already exists — call load_skill and follow it, or pick a different name if this is genuinely new ground.`; res4.Content != want {
		t.Errorf("create_skill name-exists = %q, want %q", res4.Content, want)
	}

	// create_skill: success message.
	kit5 := &ToolKit{Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}}, Creator: &fakeCreator{}}
	res5 := NewExecutors(kit5)[1].Execute(agentCtx(agentID), createArgs("deploy checklist"))
	if !containsSubstring(res5.Content, "Skill created and added to your index. Apply its instructions now; future runs load it with load_skill.") {
		t.Errorf("create_skill success message = %q", res5.Content)
	}
}

// create_skill: no tech stacks / unknown tech stack.
func TestTechStackGuardProseUnchanged(t *testing.T) {
	agentID := uuid.New()

	raw := `{"name":"deploy checklist","description":"when to use it","content":"do it","tech_stack":"django"}`
	kit := &ToolKit{Catalog: &fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}}, Creator: &fakeCreator{}}
	res := NewExecutors(kit)[1].Execute(agentCtx(agentID), raw)
	if want := `you have no tech stacks, so a skill cannot be filed under "django". Omit tech_stack to create a general skill`; res.Content != want {
		t.Errorf("no-tech-stacks = %q, want %q", res.Content, want)
	}

	kit2 := &ToolKit{
		Catalog: &fakeCatalogWithStacks{fakeCatalog: fakeCatalog{agent: domain.Agent{ID: agentID, SelfEvolutionEnabled: true}}, stacks: []domain.TechStack{{Name: "React"}, {Name: "Rails"}}},
		Creator: &fakeCreator{},
	}
	res2 := NewExecutors(kit2)[1].Execute(agentCtx(agentID), raw)
	if want := `unknown tech stack "django". Available: React, Rails. Omit tech_stack to create a general skill`; res2.Content != want {
		t.Errorf("unknown-tech-stack = %q, want %q", res2.Content, want)
	}
}

type fakeCatalogWithStacks struct {
	fakeCatalog
	stacks []domain.TechStack
}

func (f *fakeCatalogWithStacks) ListTechStacksByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error) {
	return f.stacks, nil
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}
