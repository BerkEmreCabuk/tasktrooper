package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) ListAgentTemplates(ctx context.Context) ([]domain.AgentTemplate, error) {
	if s.templates == nil {
		return nil, fmt.Errorf("agent templates not enabled")
	}
	templates, err := s.templates.List(ctx)
	if err != nil {
		return nil, err
	}
	if templates == nil {
		templates = []domain.AgentTemplate{}
	}
	return templates, nil
}

func (s *Service) CreateAgentFromTemplate(ctx context.Context, templateID uuid.UUID, override domain.CreateAgentRequest) (domain.Agent, error) {
	if s.templates == nil {
		return domain.Agent{}, fmt.Errorf("agent templates not enabled")
	}
	tpl, err := s.templates.Get(ctx, templateID)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("template not found: %w", err)
	}
	req := domain.CreateAgentRequest{
		Name:                 tpl.Name,
		Description:          tpl.Description,
		SubagentType:         tpl.SubagentType,
		SystemPrompt:         tpl.SystemPrompt,
		ProviderType:         tpl.ProviderType,
		Model:                tpl.Model,
		ToolPolicy:           tpl.ToolPolicy,
		Enabled:              true,
		SelfEvolutionEnabled: tpl.SelfEvolutionEnabled || override.SelfEvolutionEnabled,
	}
	if strings.TrimSpace(override.Name) != "" {
		req.Name = strings.TrimSpace(override.Name)
	} else {
		req.Name = s.uniqueAgentName(ctx, tpl.Name)
	}
	if strings.TrimSpace(override.Description) != "" {
		req.Description = override.Description
	}
	if strings.TrimSpace(override.SystemPrompt) != "" {
		req.SystemPrompt = override.SystemPrompt
	}
	if strings.TrimSpace(override.Model) != "" {
		req.Model = override.Model
	}
	if override.ProviderType != "" {
		req.ProviderType = override.ProviderType
	}
	agent, err := s.CreateAgent(ctx, req)
	if err != nil {
		return domain.Agent{}, err
	}
	stackIDs, err := s.recreateTechStacks(ctx, agent.ID, tpl)
	if err != nil {
		return domain.Agent{}, err
	}
	for _, tplSkill := range tpl.Skills {
		skillReq := tplSkill.CreateRequest()
		if id, ok := stackIDs[stackKey(tplSkill.TechStack)]; ok {
			skillReq.TechStackID = &id
		}
		if err := s.seedSkill(ctx, agent.ID, skillReq); err != nil {
			return domain.Agent{}, fmt.Errorf("copy skill %s: %w", skillReq.Name, err)
		}
	}
	for _, ruleReq := range tpl.Rules {
		if _, err := s.CreateRuleForAgent(ctx, agent.ID, ruleReq); err != nil {
			return domain.Agent{}, fmt.Errorf("copy rule %s: %w", ruleReq.Name, err)
		}
	}
	if s.kpis != nil {
		for _, kpiReq := range tpl.KPIs {
			if _, err := s.kpis.CreateKPI(ctx, domain.AgentKPI{
				AgentID: agent.ID, MetricKey: kpiReq.MetricKey, Name: kpiReq.Name,
				Description: kpiReq.Description, Period: kpiReq.Period,
				TargetFull: kpiReq.TargetFull, TargetHalf: kpiReq.TargetHalf,
				Weight: kpiReq.Weight, Enabled: kpiReq.Enabled,
			}); err != nil {
				return domain.Agent{}, fmt.Errorf("copy kpi %s: %w", kpiReq.MetricKey, err)
			}
		}
	}
	return s.store.GetAgent(ctx, agent.ID)
}

func (s *Service) SaveAgentAsTemplate(ctx context.Context, agentID uuid.UUID) (domain.AgentTemplate, error) {
	if s.templates == nil {
		return domain.AgentTemplate{}, fmt.Errorf("agent templates not enabled")
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, fmt.Errorf("agent not found: %w", err)
	}
	skills, err := s.store.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	stacks, err := s.store.ListTechStacksByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	stackNames := make(map[uuid.UUID]string, len(stacks))
	for _, st := range stacks {
		stackNames[st.ID] = st.Name
	}
	rules, err := s.store.ListRulesByAgent(ctx, agentID)
	if err != nil {
		return domain.AgentTemplate{}, err
	}
	tpl := domain.AgentTemplate{
		Name:                 agent.Name,
		Description:          agent.Description,
		SubagentType:         agent.SubagentType,
		SystemPrompt:         agent.SystemPrompt,
		ProviderType:         agent.ProviderType,
		Model:                agent.Model,
		ToolPolicy:           agent.ToolPolicy,
		SelfEvolutionEnabled: agent.SelfEvolutionEnabled,
		TechStacks:           make([]domain.CreateTechStackRequest, 0, len(stacks)),
		Skills:               make([]domain.TemplateSkill, 0, len(skills)),
		Rules:                make([]domain.CreateOrchestratorRuleRequest, 0, len(rules)),
	}
	for _, st := range stacks {
		tpl.TechStacks = append(tpl.TechStacks, domain.CreateTechStackRequest{
			Name: st.Name, Description: st.Description, Position: st.Position,
		})
	}
	for _, sk := range skills {
		tplSkill := domain.TemplateSkill{
			Name: sk.Name, Description: sk.Description, Category: sk.Category,
			Tags: sk.Tags, Content: sk.Content, Enabled: sk.Enabled,
		}
		if sk.TechStackID != nil {
			tplSkill.TechStack = stackNames[*sk.TechStackID]
		}
		tpl.Skills = append(tpl.Skills, tplSkill)
	}
	for _, r := range rules {
		tpl.Rules = append(tpl.Rules, domain.CreateOrchestratorRuleRequest{
			Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		})
	}
	if s.kpis != nil {
		kpis, err := s.kpis.ListByAgent(ctx, agentID)
		if err != nil {
			return domain.AgentTemplate{}, err
		}
		tpl.KPIs = make([]domain.CreateKPIRequest, 0, len(kpis))
		for _, k := range kpis {
			tpl.KPIs = append(tpl.KPIs, domain.CreateKPIRequest{
				MetricKey: k.MetricKey, Name: k.Name, Description: k.Description,
				Period: k.Period, TargetFull: k.TargetFull, TargetHalf: k.TargetHalf,
				Weight: k.Weight, Enabled: k.Enabled,
			})
		}
	}
	return s.templates.UpsertByName(ctx, tpl)
}

func (s *Service) EnsureRoleTemplates(ctx context.Context) error {
	if s.templates == nil {
		return nil
	}
	for _, def := range roleAgentDefinitions() {
		tpl := domain.AgentTemplate{
			Name:         def.agent.Name,
			Description:  def.agent.Description,
			SubagentType: def.agent.SubagentType,
			SystemPrompt: def.agent.SystemPrompt,
			ProviderType: def.agent.ProviderType,
			Model:        def.agent.Model,
			ToolPolicy:   def.agent.ToolPolicy,
			TechStacks:   def.techStacks,
			Skills:       templateSkillsFromSeeds(def.skills),
			Rules:        def.rules,
			KPIs:         def.kpis,
			BuiltIn:      true,
		}
		if _, err := s.templates.UpsertByName(ctx, tpl); err != nil {
			return fmt.Errorf("template %s: %w", def.agent.Name, err)
		}
	}
	return nil
}

// templateSkillsFromSeeds is domain.TemplateSkillsFrom for the built-in role
// seeds: it carries each skill's parsed tech_stack name onto the template the
// same way SaveAgentAsTemplate does from a live agent's stacks, so
// CreateAgentFromTemplate can recreate the same stacks and refile the same
// skills under them (recreateTechStacks). domain.TemplateSkillsFrom itself
// stays as it is — it has no stack name to carry because it lifts requests
// that never had one.
func templateSkillsFromSeeds(skills []skillSeed) []domain.TemplateSkill {
	out := make([]domain.TemplateSkill, 0, len(skills))
	for _, sk := range skills {
		out = append(out, domain.TemplateSkill{
			Name: sk.req.Name, Description: sk.req.Description, Category: sk.req.Category,
			Tags: sk.req.Tags, Content: sk.req.Content, Enabled: sk.req.Enabled,
			TechStack: sk.techStack,
		})
	}
	return out
}

// recreateTechStacks gives the new agent its own copy of the template's stacks
// and returns their ids by name, so each skill can be filed under the stack it
// was saved in. A stack named by a skill but missing from the list is created
// too: a hand-written template that only tags its skills still groups them,
// instead of silently filing every one as general.
func (s *Service) recreateTechStacks(ctx context.Context, agentID uuid.UUID, tpl domain.AgentTemplate) (map[string]uuid.UUID, error) {
	wanted := make([]domain.CreateTechStackRequest, 0, len(tpl.TechStacks))
	seen := make(map[string]bool, len(tpl.TechStacks))
	add := func(req domain.CreateTechStackRequest) {
		key := stackKey(req.Name)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		wanted = append(wanted, req)
	}
	for _, st := range tpl.TechStacks {
		add(st)
	}
	for _, sk := range tpl.Skills {
		add(domain.CreateTechStackRequest{Name: strings.TrimSpace(sk.TechStack), Position: len(wanted)})
	}

	ids := make(map[string]uuid.UUID, len(wanted))
	for _, req := range wanted {
		created, err := s.CreateTechStackForAgent(ctx, agentID, req)
		if err != nil {
			return nil, fmt.Errorf("copy tech stack %s: %w", req.Name, err)
		}
		ids[stackKey(created.Name)] = created.ID
	}
	return ids, nil
}

func stackKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *Service) uniqueAgentName(ctx context.Context, base string) string {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return base
	}
	taken := make(map[string]bool, len(agents))
	for _, a := range agents {
		taken[a.Name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
	return base + "-" + uuid.NewString()[:8]
}
