package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentTemplateStore struct {
	pool *DB
}

func NewAgentTemplateStore(pool *DB) *AgentTemplateStore {
	return &AgentTemplateStore{pool: pool}
}

const agentTemplateColumns = "id, name, description, subagent_type, system_prompt, provider_type, model, tool_policy, skills, rules, kpis, self_evolution_enabled, built_in, created_at, updated_at"

func (s *AgentTemplateStore) List(ctx context.Context) ([]domain.AgentTemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+agentTemplateColumns+` FROM agent_templates ORDER BY built_in DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list agent templates: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentTemplate
	for rows.Next() {
		tpl, err := scanAgentTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, rows.Err()
}

func (s *AgentTemplateStore) Get(ctx context.Context, id uuid.UUID) (domain.AgentTemplate, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentTemplateColumns+` FROM agent_templates WHERE id = $1`, id)
	tpl, err := scanAgentTemplate(row)
	if err != nil {
		return domain.AgentTemplate{}, fmt.Errorf("get agent template: %w", err)
	}
	return tpl, nil
}

func (s *AgentTemplateStore) UpsertByName(ctx context.Context, tpl domain.AgentTemplate) (domain.AgentTemplate, error) {
	policyJSON, err := json.Marshal(tpl.ToolPolicy)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	skillsJSON, err := json.Marshal(templateCatalog{
		Skills:     orEmptySkills(tpl.Skills),
		TechStacks: orEmptyTechStacks(tpl.TechStacks),
	})
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	rulesJSON, err := json.Marshal(orEmptyRules(tpl.Rules))
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	kpisJSON, err := json.Marshal(orEmptyKPIs(tpl.KPIs))
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_templates (name, description, subagent_type, system_prompt, provider_type, model, tool_policy, skills, rules, kpis, self_evolution_enabled, built_in)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			subagent_type = EXCLUDED.subagent_type,
			system_prompt = EXCLUDED.system_prompt,
			provider_type = EXCLUDED.provider_type,
			model = EXCLUDED.model,
			tool_policy = EXCLUDED.tool_policy,
			skills = EXCLUDED.skills,
			rules = EXCLUDED.rules,
			kpis = EXCLUDED.kpis,
			self_evolution_enabled = EXCLUDED.self_evolution_enabled,
			built_in = EXCLUDED.built_in,
			updated_at = now()
		RETURNING `+agentTemplateColumns,
		tpl.Name, tpl.Description, tpl.SubagentType, tpl.SystemPrompt, tpl.ProviderType, tpl.Model, policyJSON, skillsJSON, rulesJSON, kpisJSON, tpl.SelfEvolutionEnabled, tpl.BuiltIn)
	out, err := scanAgentTemplate(row)
	if err != nil {
		return domain.AgentTemplate{}, fmt.Errorf("upsert agent template: %w", err)
	}
	return out, nil
}

func (s *AgentTemplateStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete agent template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent template not found")
	}
	return nil
}

func scanAgentTemplate(row pgx.Row) (domain.AgentTemplate, error) {
	var tpl domain.AgentTemplate
	var policyJSON, skillsJSON, rulesJSON, kpisJSON []byte
	if err := row.Scan(
		&tpl.ID, &tpl.Name, &tpl.Description, &tpl.SubagentType, &tpl.SystemPrompt,
		&tpl.ProviderType, &tpl.Model, &policyJSON, &skillsJSON, &rulesJSON, &kpisJSON, &tpl.SelfEvolutionEnabled, &tpl.BuiltIn, &tpl.CreatedAt, &tpl.UpdatedAt,
	); err != nil {
		return domain.AgentTemplate{}, err
	}
	_ = json.Unmarshal(policyJSON, &tpl.ToolPolicy)
	decodeTemplateCatalog(skillsJSON, &tpl)
	_ = json.Unmarshal(rulesJSON, &tpl.Rules)
	_ = json.Unmarshal(kpisJSON, &tpl.KPIs)
	return tpl, nil
}

// templateCatalog is what the skills column holds. The stack list lives in the
// same document as the skills it groups because a template's stacks are only
// ever read with them, and because a template stored before stacks existed is a
// bare JSON array — decodeTemplateCatalog still reads those, as a template
// whose skills are all general.
type templateCatalog struct {
	Skills     []domain.TemplateSkill          `json:"skills"`
	TechStacks []domain.CreateTechStackRequest `json:"tech_stacks"`
}

func decodeTemplateCatalog(raw []byte, tpl *domain.AgentTemplate) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return
	}
	if trimmed[0] == '[' {
		_ = json.Unmarshal(trimmed, &tpl.Skills)
		return
	}
	var catalog templateCatalog
	if err := json.Unmarshal(trimmed, &catalog); err != nil {
		return
	}
	tpl.Skills = catalog.Skills
	tpl.TechStacks = catalog.TechStacks
}

func orEmptySkills(in []domain.TemplateSkill) []domain.TemplateSkill {
	if in == nil {
		return []domain.TemplateSkill{}
	}
	return in
}

func orEmptyTechStacks(in []domain.CreateTechStackRequest) []domain.CreateTechStackRequest {
	if in == nil {
		return []domain.CreateTechStackRequest{}
	}
	return in
}

func orEmptyRules(in []domain.CreateOrchestratorRuleRequest) []domain.CreateOrchestratorRuleRequest {
	if in == nil {
		return []domain.CreateOrchestratorRuleRequest{}
	}
	return in
}

func orEmptyKPIs(in []domain.CreateKPIRequest) []domain.CreateKPIRequest {
	if in == nil {
		return []domain.CreateKPIRequest{}
	}
	return in
}
