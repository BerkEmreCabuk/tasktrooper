package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) SeedDefaultsIfEmpty(ctx context.Context) error {
	return s.EnsureRoleAgents(ctx)
}

func (s *Service) SeedRoleAgentsIfEmpty(ctx context.Context) error {
	return s.EnsureRoleAgents(ctx)
}

func (s *Service) EnsureRoleAgents(ctx context.Context) error {
	s.seeding.Store(true)
	defer s.seeding.Store(false)

	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	byName := make(map[string]domain.Agent, len(agents))
	for _, a := range agents {
		byName[a.Name] = a
	}

	var seedErrs []error
	for _, def := range roleAgentDefinitions() {
		if err := s.ensureRoleAgent(ctx, def, byName); err != nil {
			seedErrs = append(seedErrs, fmt.Errorf("%s: %w", def.agent.Name, err))
		}
	}
	if err := s.EnsureRoleTemplates(ctx); err != nil {
		seedErrs = append(seedErrs, err)
	}
	if err := s.ensureRoleSubscriptions(ctx); err != nil {
		seedErrs = append(seedErrs, err)
	}
	return errors.Join(seedErrs...)
}

// ensureRoleSubscriptions subscribes each reviewing role to the hand-off column
// it owns, so tasks flow to the next stage's agent automatically instead of
// back to the implementer-assignee:
//
//	system-architect -> code_review                    (reviews the diff)
//	qa-agent         -> ready_for_qa, in_qa, done      (picks it up, tests it, merges its PR)
//	product-manager  -> pm_uat                         (reviews against acceptance criteria)
//
// (analiz_review and human_uat have no subscriber by design — they are the
// human approval gates.)
//
// QA owns both of its testing columns: ready_for_qa is the queue it is handed,
// in_qa is where it tests. Subscribing it to ready_for_qa alone left in_qa
// unowned, so a task moved there resolved back to the implementer-assignee.
//
// done is the third, and it is not a testing column: it is where the task's
// pull request gets merged. It had no subscriber and dispatched nobody, so a
// signed-off task's change sat on a branch until a human pressed Merge. The
// dispatcher wakes this subscription ONLY for a task whose PR is still
// unmerged and only on a move into done (Dispatcher.doneMergeWake), so the
// column cannot go back to what it did before — dispatching the implementer
// onto its own finished task, which is how done tasks drifted into released.
//
// It is additive and idempotent: it only sets a subscription when the agent
// currently has none, so an admin's later customization survives restarts.
// Existing installs are backfilled by migration 070 instead.
func (s *Service) ensureRoleSubscriptions(ctx context.Context) error {
	if s.boardConfig == nil {
		return nil
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]uuid.UUID, len(agents))
	for _, a := range agents {
		byName[a.Name] = a.ID
	}
	roleColumns := []struct {
		agent   string
		columns []domain.TaskColumn
	}{
		{"system-architect", []domain.TaskColumn{domain.TaskColumnCodeReview}},
		{"qa-agent", []domain.TaskColumn{domain.TaskColumnReadyForQA, domain.TaskColumnInQA, domain.TaskColumnDone}},
		{"product-manager", []domain.TaskColumn{domain.TaskColumnPMUAT}},
	}
	var errs []error
	for _, rc := range roleColumns {
		agentID, ok := byName[rc.agent]
		if !ok {
			continue
		}
		subs, err := s.boardConfig.ListAgentSubscriptions(ctx, agentID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(subs) > 0 {
			continue
		}
		slugs := make([]string, 0, len(rc.columns))
		for _, col := range rc.columns {
			slugs = append(slugs, string(col))
		}
		if err := s.boardConfig.SetAgentSubscriptions(ctx, agentID, slugs); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) ensureRoleAgent(ctx context.Context, def roleAgentDef, byName map[string]domain.Agent) error {
	agent, ok := byName[def.agent.Name]
	if !ok {
		req := def.agent
		if s.claudeCodeRunnable() {
			req.ProviderType, req.Model, req.ModelHeavy = roleAgentProvider, roleAgentModel, roleAgentModelHeavy
		}
		created, err := s.CreateAgent(ctx, req)
		if err != nil {
			return err
		}
		if err := s.ensureRoleAgentCatalog(ctx, created.ID, def); err != nil {
			_ = s.store.DeleteAgent(ctx, created.ID)
			return err
		}
		byName[def.agent.Name] = created
		return nil
	}
	needsAgentUpdate := false
	if agent.SystemPrompt != def.agent.SystemPrompt {
		agent.SystemPrompt = def.agent.SystemPrompt
		needsAgentUpdate = true
	}
	if agent.Description != def.agent.Description {
		agent.Description = def.agent.Description
		needsAgentUpdate = true
	}
	if agent.SubagentType != def.agent.SubagentType {
		agent.SubagentType = def.agent.SubagentType
		needsAgentUpdate = true
	}
	if s.fillRoleAgentModels(&agent) {
		needsAgentUpdate = true
	}
	// ToolPolicy is intentionally NOT reconciled here: an admin may customize an
	// agent's tools via the API/UI and that must survive restarts
	// (TestEnsureRoleAgents_PreservesCustomToolPolicy). The code seed sets the
	// policy on CREATE only; the admin tool picker is responsible for exposing
	// every registered tool so a grant never silently drops one.
	//
	// The one exception is additive and paired: a tool introduced after this
	// tenant was seeded, granted only where the capability it completes is
	// already granted. See roleToolGrants — it is how a new tool reaches an
	// existing install now that a migration cannot write tenant rows.
	if grantMissingRoleTools(&agent) {
		needsAgentUpdate = true
	}
	if needsAgentUpdate {
		updated, err := s.store.UpdateAgent(ctx, agent)
		if err != nil {
			return fmt.Errorf("sync role agent: %w", err)
		}
		byName[def.agent.Name] = updated
	}
	return s.ensureRoleAgentCatalog(ctx, agent.ID, def)
}

// fillRoleAgentModels puts the seeded provider and model pair on an agent that
// has none, and reports whether it changed anything.
//
// This one IS reconciled, unlike ToolPolicy and Effort above, because a tenant
// seeded before the defaults existed has agents whose models are empty for no
// reason anyone chose — and the escalation to ModelHeavy that a "hard" subtask
// wants cannot be reached until something fills it in. The rule that makes that
// safe is the one ensureRoleSubscriptions already follows: fill what is empty,
// never replace what a person picked.
//
// The pair is filled as a pair. An agent whose Model somebody deliberately set
// to `haiku` does not get an `opus` escalation bolted onto that choice behind
// their back, so EITHER name being set leaves both alone.
//
// Backfilled here rather than by a migration because what to write depends on
// whether this host can run the CLI at all, which is discovered by probing the
// host at boot and is not a question SQL can ask.
func (s *Service) fillRoleAgentModels(agent *domain.Agent) bool {
	if agent.Model != "" || agent.ModelHeavy != "" {
		return false
	}
	switch agent.ProviderType {
	case roleAgentProvider:
		// Already where the names belong; only the names are missing.
	case "":
		// Never chosen, so it is still the seed's to set. Set together with the
		// names for the reason roleAgentProvider gives: shipping `sonnet` to
		// whichever HTTP provider the tenant happens to have activated is the
		// exact failure the constant exists to prevent.
		if !s.claudeCodeRunnable() {
			return false
		}
		agent.ProviderType = roleAgentProvider
	default:
		// Somebody moved this agent to another provider. These names mean
		// nothing there.
		return false
	}
	agent.Model, agent.ModelHeavy = roleAgentModel, roleAgentModelHeavy
	return true
}

// claudeCodeRunnable asks the same question checkHostExecutor does — may an
// agent be saved onto the CLI provider here — and asks it first, so the seed
// never proposes a configuration that guard would refuse. Without it a host
// with no CLI attached would lose all six role agents to a create that fails.
func (s *Service) claudeCodeRunnable() bool {
	return s.hostExecutor != nil && s.hostExecutor(roleAgentProvider)
}

func (s *Service) ensureRoleAgentCatalog(ctx context.Context, agentID uuid.UUID, def roleAgentDef) error {
	stackIDs, err := s.ensureRoleTechStacks(ctx, agentID, def.techStacks)
	if err != nil {
		return err
	}
	skills, err := s.store.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return err
	}
	for _, sk := range def.skills {
		skillReq := sk.req
		techStackID, err := resolveSeedTechStack(stackIDs, sk.techStack)
		if err != nil {
			return fmt.Errorf("skill %s: %w", skillReq.Name, err)
		}
		skillReq.TechStackID = techStackID
		if existing, ok := existingSkillByName(skills, skillReq.Name); ok {
			// Enabled is compared like the rest: a capability the code parks
			// (mdSkillDisabled) has to reach installs that were seeded while it
			// was still on, and one that comes back has to reach them again.
			// It was already seed-owned in practice — every content edit below
			// overwrites it — so this only closes the case where nothing but
			// the flag moved. TechStackID joins that same seed-owned set: a
			// skill's stack is a fact about the skill's content (which
			// language it applies to), not an admin customization the way
			// ToolPolicy below is.
			if existing.Description != skillReq.Description ||
				existing.Content != skillReq.Content ||
				existing.Category != skillReq.Category ||
				existing.Enabled != skillReq.Enabled ||
				!techStackIDsEqual(existing.TechStackID, skillReq.TechStackID) {
				existing.Description = skillReq.Description
				existing.Content = skillReq.Content
				existing.Category = skillReq.Category
				existing.Enabled = skillReq.Enabled
				existing.TechStackID = skillReq.TechStackID
				if _, err := s.store.UpdateSkill(ctx, existing); err != nil {
					return fmt.Errorf("skill %s: %w", skillReq.Name, err)
				}
			}
			continue
		}
		if err := s.seedSkill(ctx, agentID, skillReq); err != nil {
			return fmt.Errorf("skill %s: %w", skillReq.Name, err)
		}
	}

	rules, err := s.store.ListRulesByAgent(ctx, agentID)
	if err != nil {
		return err
	}
	for _, ruleReq := range def.rules {
		if existing, ok := existingRuleByName(rules, ruleReq.Name); ok {
			// Same reason as the skills above: disabledRule must be able to
			// park a rule on an install that already has it enabled.
			if existing.Content != ruleReq.Content ||
				existing.Priority != ruleReq.Priority ||
				existing.Enabled != ruleReq.Enabled {
				existing.Content = ruleReq.Content
				existing.Priority = ruleReq.Priority
				existing.Enabled = ruleReq.Enabled
				if _, err := s.store.UpdateRule(ctx, existing); err != nil {
					return fmt.Errorf("rule %s: %w", ruleReq.Name, err)
				}
			}
			continue
		}
		if _, err := s.CreateRuleForAgent(ctx, agentID, ruleReq); err != nil {
			return fmt.Errorf("rule %s: %w", ruleReq.Name, err)
		}
	}
	for _, deprecated := range deprecatedRoleRules(def.agent.Name) {
		if existing, ok := existingRuleByName(rules, deprecated); ok {
			if err := s.store.DeleteRule(ctx, existing.ID); err != nil {
				return fmt.Errorf("delete deprecated rule %s: %w", deprecated, err)
			}
		}
	}
	for _, deprecated := range deprecatedRoleSkills(def.agent.Name) {
		if existing, ok := existingSkillByName(skills, deprecated); ok {
			if err := s.store.DeleteSkill(ctx, existing.ID); err != nil {
				return fmt.Errorf("delete deprecated skill %s: %w", deprecated, err)
			}
		}
	}
	if s.kpis != nil && len(def.kpis) > 0 {
		existing, err := s.kpis.ListByAgent(ctx, agentID)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			for _, kpiReq := range def.kpis {
				if _, err := s.kpis.CreateKPI(ctx, domain.AgentKPI{
					AgentID: agentID, MetricKey: kpiReq.MetricKey, Name: kpiReq.Name,
					Description: kpiReq.Description, Period: kpiReq.Period,
					TargetFull: kpiReq.TargetFull, TargetHalf: kpiReq.TargetHalf,
					Weight: kpiReq.Weight, Enabled: kpiReq.Enabled,
				}); err != nil {
					return fmt.Errorf("kpi %s: %w", kpiReq.MetricKey, err)
				}
			}
		}
	}
	return nil
}

// ensureRoleTechStacks makes sure every stack a role definition declares
// exists on its agent, and returns their ids keyed by stackKey(name). It is
// additive and idempotent like ensureRoleSubscriptions: a stack that already
// exists by name is left exactly as it is, so an admin's rename, re-describe,
// or reorder survives a restart, and running this twice creates nothing twice.
func (s *Service) ensureRoleTechStacks(ctx context.Context, agentID uuid.UUID, wanted []domain.CreateTechStackRequest) (map[string]uuid.UUID, error) {
	existing, err := s.ListTechStacksForAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list tech stacks: %w", err)
	}
	ids := make(map[string]uuid.UUID, len(existing)+len(wanted))
	for _, st := range existing {
		ids[stackKey(st.Name)] = st.ID
	}
	for _, req := range wanted {
		key := stackKey(req.Name)
		if key == "" {
			continue
		}
		if _, ok := ids[key]; ok {
			continue
		}
		created, err := s.CreateTechStackForAgent(ctx, agentID, req)
		if err != nil {
			return nil, fmt.Errorf("tech stack %s: %w", req.Name, err)
		}
		ids[key] = created.ID
	}
	return ids, nil
}

// resolveSeedTechStack turns a SKILL.md's `tech_stack:` front-matter into the
// id ensureRoleTechStacks created for it. An empty name is the general skill
// (nil, matching domain.Skill.TechStackID's convention). A non-empty name that
// is not one of the role's declared stacks fails loudly instead of silently
// filing the skill as general — a typo'd or forgotten stack in role_seed.go
// would otherwise be invisible.
func resolveSeedTechStack(stackIDs map[string]uuid.UUID, techStack string) (*uuid.UUID, error) {
	key := stackKey(techStack)
	if key == "" {
		return nil, nil
	}
	id, ok := stackIDs[key]
	if !ok {
		return nil, fmt.Errorf("tech stack %q is not declared for this role", techStack)
	}
	return &id, nil
}

func techStackIDsEqual(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deprecatedRoleRules(agentName string) []string {
	switch agentName {
	case "qa-agent":
		return []string{
			"read-only-code", "test-code-only", "board-qa-columns",
			// 2026-08-12: manual-before-automation'ın yerini manual-only-testing
			// aldı, o yüzden bu gerçekten kaldırıldı. Otomasyon kuralları
			// (e2e-automation-project, deterministic-test-env) buraya girmez —
			// role_seed.go'da disabledRule ile kapalı bekliyorlar.
			"manual-before-automation",
		}
	case "product-manager":
		return []string{"project-context-required"}
	case "system-architect":
		return []string{"close-analiz-on-handoff"}
	case "mobile-developer":
		return []string{"react-native-patterns", "wails-desktop-integration"}
	default:
		return nil
	}
}

func deprecatedRoleSkills(agentName string) []string {
	switch agentName {
	case "qa-agent":
		return []string{
			"test-plan-design",
			"api-manual-testing",
			"bug-report-standards",
			"qa-column-workflow",
			"e2e-automation",
			"edge-case-analysis",
			// backend-manual-testing + frontend-manual-testing ikilisine bölündü.
			"live-app-verification",
			// Otomasyon skill'leri BURAYA GİRMEZ: ertelendiler, kaldırılmadılar.
			// role_seed.go'da mdSkillDisabled ile kapalı duruyorlar.
		}
	case "product-manager":
		return []string{
			"pm-orchestration-solo-policy",
			"pm-delegation-workflow",
			"pm-analiz-first-strategy",
			"local-llm-agent-team",
			"local-llm-system-overview",
			"pm-clarification-policy",
			"user-story-writing",
			"acceptance-criteria",
			"task-decomposition-for-agents",
			"task-assignment-by-agent-role",
			"scope-definition",
			"project-board-workflow",
			"pm-uat-and-release",
			"requirements-documents",
			"team-capabilities",
		}
	default:
		return nil
	}
}

func existingRuleByName(rules []domain.OrchestratorRule, name string) (domain.OrchestratorRule, bool) {
	for _, r := range rules {
		if r.Name == name {
			return r, true
		}
	}
	return domain.OrchestratorRule{}, false
}

func existingSkillByName(skills []domain.Skill, name string) (domain.Skill, bool) {
	for _, sk := range skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return domain.Skill{}, false
}

// seedSkill stores the skill without a vector. Embedding here made the seed as
// slow as the embedding provider: the requests-per-minute pacing alone put the
// role catalog past the boot step's deadline, and a failed catalog deletes its
// half-built agent. BackfillSkillEmbeddings embeds the skills afterwards.
func (s *Service) seedSkill(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) error {
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := s.store.CreateSkill(ctx, domain.Skill{
		AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Enabled: req.Enabled,
		TechStackID: req.TechStackID,
	})
	return err
}

// BackfillSkillEmbeddings embeds every skill stored without a vector, with the
// client's normal pacing and retries, and returns how many it updated. The role
// agent seed stores every skill without one, so this is what fills them in.
func (s *Service) BackfillSkillEmbeddings(ctx context.Context) (int, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	updated := 0
	for _, agent := range agents {
		skills, err := s.store.ListSkillsByAgent(ctx, agent.ID)
		if err != nil {
			return updated, fmt.Errorf("list skills of %s: %w", agent.Name, err)
		}
		for _, sk := range skills {
			if len(sk.Embedding) > 0 {
				continue
			}
			emb, err := s.llm.Embed(ctx, sk.Name+"\n"+sk.Description+"\n"+sk.Content, s.embeddingModel)
			if err != nil {
				return updated, fmt.Errorf("embed skill %s: %w", sk.Name, err)
			}
			if len(emb) == 0 {
				continue
			}
			sk.Embedding = emb
			if _, err := s.store.UpdateSkill(ctx, sk); err != nil {
				return updated, fmt.Errorf("update skill %s: %w", sk.Name, err)
			}
			updated++
		}
	}
	return updated, nil
}

type roleAgentDef struct {
	agent domain.CreateAgentRequest
	// techStacks are the tech stacks this role's own skills are filed under,
	// created on the agent before its skills are (ensureRoleAgentCatalog) so
	// every skill's front-matter tech_stack name has something to resolve
	// against. A role with no stack-specific skills (product-manager, qa-agent,
	// system-architect today) leaves this nil.
	techStacks []domain.CreateTechStackRequest
	skills     []skillSeed
	rules      []domain.CreateOrchestratorRuleRequest
	kpis       []domain.CreateKPIRequest
}

func roleAgentDefinitions() []roleAgentDef {
	defs := []roleAgentDef{
		systemArchitectAgent(),
		backendDeveloperAgent(),
		frontendDeveloperAgent(),
		mobileDeveloperAgent(),
		productManagerAgent(),
		qaAgent(),
	}
	for i := range defs {
		defs[i].kpis = defaultRoleKPIs(defs[i].agent.Name)
	}
	return defs
}

func defaultRoleKPIs(agentName string) []domain.CreateKPIRequest {
	tasksCompleted := domain.CreateKPIRequest{
		MetricKey: "tasks_completed", Name: "Weekly tasks completed", Period: domain.KPIPeriodWeekly,
		TargetFull: 3, TargetHalf: 1, Weight: 1, Enabled: true,
	}
	revisions := domain.CreateKPIRequest{
		MetricKey: "revisions_received", Name: "Weekly revisions", Period: domain.KPIPeriodWeekly,
		TargetFull: 1, TargetHalf: 3, Weight: 1.5, Enabled: true,
	}
	bugs := domain.CreateKPIRequest{
		MetricKey: "bugs_assigned", Name: "Weekly bugs", Period: domain.KPIPeriodWeekly,
		TargetFull: 2, TargetHalf: 5, Weight: 1, Enabled: true,
	}
	// Speed targets carry weight 1 while the quality KPIs beside them carry
	// 1.5 and 2. The composite therefore cannot be raised by trading quality
	// away, which is the same rule the clean-only measurement enforces from the
	// other side.
	devSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_progress", Name: "Clean cycle time (in progress)", Period: domain.KPIPeriodWeekly,
		TargetFull: 6, TargetHalf: 16, Weight: 1, Enabled: true,
	}
	architectSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_code_review", Name: "Clean review time", Period: domain.KPIPeriodWeekly,
		TargetFull: 1, TargetHalf: 4, Weight: 1, Enabled: true,
	}
	// The architect also implements analiz tasks, which sit in in_progress.
	architectAnalysisSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_progress", Name: "Clean analysis time", Period: domain.KPIPeriodWeekly,
		TargetFull: 4, TargetHalf: 12, Weight: 1, Enabled: true,
	}
	reviewEscapes := domain.CreateKPIRequest{
		MetricKey: "review_escapes", Name: "Weekly review escapes", Period: domain.KPIPeriodWeekly,
		TargetFull: 0, TargetHalf: 1, Weight: 2, Enabled: true,
	}
	qaSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_qa", Name: "Clean QA time", Period: domain.KPIPeriodWeekly,
		TargetFull: 3, TargetHalf: 8, Weight: 1, Enabled: true,
	}
	pmSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_pm_uat", Name: "Clean UAT time", Period: domain.KPIPeriodWeekly,
		TargetFull: 2, TargetHalf: 6, Weight: 1, Enabled: true,
	}
	switch agentName {
	case "system-architect":
		return []domain.CreateKPIRequest{tasksCompleted, revisions, architectSpeed, architectAnalysisSpeed, reviewEscapes}
	case "backend-developer", "frontend-developer", "mobile-developer":
		return []domain.CreateKPIRequest{tasksCompleted, revisions, bugs, devSpeed}
	case "qa-agent":
		return []domain.CreateKPIRequest{
			tasksCompleted,
			{MetricKey: "uat_failures", Name: "Weekly UAT escapes", Period: domain.KPIPeriodWeekly,
				TargetFull: 0, TargetHalf: 2, Weight: 1.5, Enabled: true},
			qaSpeed,
		}
	case "product-manager":
		return []domain.CreateKPIRequest{tasksCompleted, pmSpeed}
	default:
		return nil
	}
}
