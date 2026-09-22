package domain

import (
	"time"

	"github.com/google/uuid"
)

type AgentTemplate struct {
	ID           uuid.UUID                       `json:"id"`
	Name         string                          `json:"name"`
	Description  string                          `json:"description"`
	SubagentType string                          `json:"subagent_type"`
	SystemPrompt string                          `json:"system_prompt"`
	ProviderType LLMProviderType                 `json:"provider_type"`
	Model        string                          `json:"model"`
	ToolPolicy   ToolPolicy                      `json:"tool_policy"`
	TechStacks   []CreateTechStackRequest        `json:"tech_stacks"`
	Skills       []TemplateSkill                 `json:"skills"`
	Rules        []CreateOrchestratorRuleRequest `json:"rules"`
	KPIs         []CreateKPIRequest              `json:"kpis"`
	// Filled by CreateAgentFromTemplate only into a genuinely open seat;
	// naming a role key is data, not routing — the engine never reads a
	// template, and a missing key is simply skipped.
	Roles                []TemplateRoleSuggestion `json:"roles"`
	Subscriptions        []TaskColumn             `json:"subscriptions"`
	SelfEvolutionEnabled bool                     `json:"self_evolution_enabled"`
	BuiltIn              bool                     `json:"built_in"`
	CreatedAt            time.Time                `json:"created_at"`
	UpdatedAt            time.Time                `json:"updated_at"`
}

// TemplateRoleSuggestion is one role a template proposes its agent for. Key
// names a roles.key row; nil Areas means the same "any area" a RoleAssignment
// with nil Areas does.
type TemplateRoleSuggestion struct {
	Key   string   `json:"key"`
	Areas []string `json:"areas,omitempty"`
}

// TemplateSkill is a skill as a template carries it. It names its tech stack
// rather than pointing at one: a stack id belongs to the agent the template
// was saved from, and a new agent owns different rows. An empty TechStack is a
// general skill — what an old template predating stacks decodes to.
type TemplateSkill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Content     string   `json:"content"`
	Enabled     bool     `json:"enabled"`
	TechStack   string   `json:"tech_stack,omitempty"`
}

// CreateRequest is the skill minus its stack name, which only the template
// knows how to resolve into an id on the target agent.
func (s TemplateSkill) CreateRequest() CreateSkillRequest {
	return CreateSkillRequest{
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Tags:        s.Tags,
		Content:     s.Content,
		Enabled:     s.Enabled,
	}
}

func TemplateSkillsFrom(reqs []CreateSkillRequest) []TemplateSkill {
	out := make([]TemplateSkill, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, TemplateSkill{
			Name: r.Name, Description: r.Description, Category: r.Category,
			Tags: r.Tags, Content: r.Content, Enabled: r.Enabled,
		})
	}
	return out
}
