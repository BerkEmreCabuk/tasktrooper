package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// Serializes boot/interval/manual syncs: concurrent runs would each reconcile the other's results back.
var syncMu sync.Mutex

// Mirror of evolution's max_skills_per_agent so the two writers agree on "full".
func (s *Service) SetSkillBudget(max int) { s.skillBudget = max }

// New upstream agents and skills always apply; per-agent auto_pull_agent_updates and keep_skills_updated gate the rest, and what the rules cannot apply lands in catalog_pending.
func (s *Service) SyncFromCatalog(ctx context.Context, reader port.CatalogRepoReader, syncStore port.CatalogSyncStore) (*domain.CatalogSyncResult, error) {
	syncMu.Lock()
	defer syncMu.Unlock()

	defs, ref, err := reader.ReadCatalog(ctx)
	if err != nil {
		s.recordSync(ctx, syncStore, ref, nil, err.Error())
		return nil, err
	}

	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		s.recordSync(ctx, syncStore, ref, &domain.CatalogSyncResult{RepoRef: ref}, err.Error())
		return nil, err
	}
	bySlug := make(map[string]domain.Agent, len(agents))
	byName := make(map[string]domain.Agent, len(agents))
	for _, a := range agents {
		byName[a.Name] = a
		if a.CatalogSlug != "" {
			bySlug[a.CatalogSlug] = a
		}
	}

	res := &domain.CatalogSyncResult{RepoRef: ref}
	for _, def := range defs {
		existing, ok := bySlug[def.Slug]
		if !ok {
			// Name-based adoption: same-named slug-less agents predate catalog slugs; adopt instead of duplicating.
			if unnamed, found := byName[def.Name]; found && unnamed.CatalogSlug == "" {
				adopted, err := s.adoptByName(ctx, unnamed, def, syncStore, res)
				if err != nil {
					s.recordSync(ctx, syncStore, ref, res, fmt.Sprintf("agent %s: %v", def.Slug, err))
					return nil, err
				}
				bySlug[def.Slug] = adopted
				continue
			}
			if err := s.ingestNewAgent(ctx, def, syncStore, res); err != nil {
				s.recordSync(ctx, syncStore, ref, res, fmt.Sprintf("agent %s: %v", def.Slug, err))
				return nil, err
			}
			continue
		}
		if existing.CatalogEtag == def.Etag {
			continue
		}
		if existing.AutoPullAgentUpdates {
			if _, err := s.applyUpstreamAgent(ctx, existing, def, syncStore, res); err != nil {
				s.recordSync(ctx, syncStore, ref, res, fmt.Sprintf("agent %s: %v", def.Slug, err))
				return nil, err
			}
		} else {
			s.park(ctx, syncStore, domain.CatalogPending{
				AgentSlug: def.Slug, AgentName: existing.Name,
				Kind: domain.CatalogPendingKindAgent, Name: def.Slug,
				Action: domain.CatalogPendingActionUpdate,
				Reason: "auto_pull_agent_updates kapali; prompt/rollar guncellenmedi",
			})
			res.Skipped++
		}
		stackIDs, err := s.ensureTechStacks(ctx, existing.ID, def)
		if err != nil {
			s.recordSync(ctx, syncStore, ref, res, fmt.Sprintf("agent %s: %v", def.Slug, err))
			return nil, err
		}
		if err := s.ensureKPIs(ctx, existing.ID, def); err != nil {
			s.recordSync(ctx, syncStore, ref, res, fmt.Sprintf("agent %s: %v", def.Slug, err))
			return nil, err
		}
		if err := s.reconcileSkills(ctx, existing, def, stackIDs, syncStore, res); err != nil {
			s.recordSync(ctx, syncStore, ref, res, err.Error())
			return nil, err
		}
	}

	s.recordSync(ctx, syncStore, ref, res, "")
	return res, nil
}

func (s *Service) ingestNewAgent(ctx context.Context, def domain.UpstreamAgent, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) error {
	agent, err := s.CreateAgent(ctx, domain.CreateAgentRequest{
		Name:                 def.Name,
		Description:          def.Description,
		SubagentType:         def.SubagentType,
		SystemPrompt:         def.SystemPrompt,
		ProviderType:         def.ProviderType,
		Model:                def.Model,
		ModelHeavy:           def.ModelHeavy,
		Effort:               def.Effort,
		MaxTurns:             def.MaxTurns,
		ToolPolicy:           def.ToolPolicy,
		Enabled:              def.Enabled,
		SelfEvolutionEnabled: def.SelfEvolution,
	})
	if err != nil {
		// One agent's refusal must not take the whole catalog down with it.
		s.park(ctx, syncStore, domain.CatalogPending{
			AgentSlug: def.Slug, AgentName: def.Name,
			Kind: domain.CatalogPendingKindAgent, Name: def.Slug,
			Action: domain.CatalogPendingActionCreate,
			Reason: truncateReason(err.Error()),
		})
		res.Skipped++
		return nil
	}
	agent.CatalogSlug = def.Slug
	agent.CatalogEtag = def.Etag
	if _, err := s.store.UpdateAgent(ctx, agent); err != nil {
		return fmt.Errorf("stamp catalog identity on %s: %w", def.Name, err)
	}
	if err := s.applySuggestedSubscriptions(ctx, agent, def.Subscriptions); err != nil {
		return err
	}
	if err := s.applySuggestedRoles(ctx, agent, def.Roles); err != nil {
		return err
	}
	if err := s.reconcileColumnInstructions(ctx, agent.ID, def); err != nil {
		return err
	}
	for _, r := range def.Rules {
		if _, err := s.CreateRuleForAgent(ctx, agent.ID, domain.CreateOrchestratorRuleRequest{
			Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		}); err != nil {
			return fmt.Errorf("create rule %s: %w", r.Name, err)
		}
	}
	stackIDs, err := s.ensureTechStacks(ctx, agent.ID, def)
	if err != nil {
		return err
	}
	if err := s.ensureKPIs(ctx, agent.ID, def); err != nil {
		return err
	}
	if err := s.reconcileSkills(ctx, agent, def, stackIDs, syncStore, res); err != nil {
		return err
	}
	res.Created++
	log.Info().Str("agent", def.Name).Str("slug", def.Slug).Msg("catalog: new agent ingested")
	return nil
}

// Overwrites an existing agent from the catalog, preserving identity columns and the user-owned toggles.
func (s *Service) applyUpstreamAgent(ctx context.Context, existing domain.Agent, def domain.UpstreamAgent, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) (domain.Agent, error) {
	req := domain.UpdateAgentRequest{
		Name: def.Name, Description: def.Description, SubagentType: def.SubagentType,
		SystemPrompt: def.SystemPrompt, ProviderType: def.ProviderType,
		Model: def.Model, ModelHeavy: def.ModelHeavy, Effort: def.Effort,
		MaxTurns: def.MaxTurns, ToolPolicy: def.ToolPolicy,
		Enabled: def.Enabled, SelfEvolutionEnabled: def.SelfEvolution,
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = existing.Name
	}
	// The manifest leaves provider/model/model_heavy empty on purpose: empty means "preserve", not "clear".
	if def.ProviderType == "" {
		req.ProviderType = existing.ProviderType
	}
	if def.Model == "" {
		req.Model = existing.Model
	}
	if def.ModelHeavy == "" {
		req.ModelHeavy = existing.ModelHeavy
	}
	autoPull := existing.AutoPullAgentUpdates
	keepUpdated := existing.KeepSkillsUpdated
	req.AutoPullAgentUpdates = &autoPull
	req.KeepSkillsUpdated = &keepUpdated
	agent, err := s.UpdateAgent(ctx, existing.ID, req)
	if err != nil {
		return domain.Agent{}, err
	}
	agent.CatalogSlug = existing.CatalogSlug
	agent.CatalogEtag = def.Etag
	if _, err := s.store.UpdateAgent(ctx, agent); err != nil {
		return domain.Agent{}, err
	}
	if err := s.applySuggestedSubscriptions(ctx, agent, def.Subscriptions); err != nil {
		return domain.Agent{}, err
	}
	if err := s.applySuggestedRoles(ctx, agent, def.Roles); err != nil {
		return domain.Agent{}, err
	}
	if err := s.reconcileColumnInstructions(ctx, agent.ID, def); err != nil {
		return domain.Agent{}, err
	}
	if err := s.upsertRules(ctx, agent, def.Rules); err != nil {
		return domain.Agent{}, err
	}
	res.Updated++
	log.Info().Str("agent", agent.Name).Str("slug", def.Slug).Msg("catalog: agent definition updated")
	return agent, nil
}

func (s *Service) upsertRules(ctx context.Context, agent domain.Agent, rules []domain.UpstreamRule) error {
	existing, err := s.store.ListRulesByAgent(ctx, agent.ID)
	if err != nil {
		return err
	}
	byName := make(map[string]domain.OrchestratorRule, len(existing))
	for _, r := range existing {
		byName[r.Name] = r
	}
	for _, r := range rules {
		if prev, ok := byName[r.Name]; ok {
			if _, err := s.UpdateRuleForAgent(ctx, agent.ID, prev.ID, domain.UpdateOrchestratorRuleRequest{
				Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
			}); err != nil {
				return err
			}
			continue
		}
		if _, err := s.CreateRuleForAgent(ctx, agent.ID, domain.CreateOrchestratorRuleRequest{
			Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		}); err != nil {
			return err
		}
	}
	return nil
}

// stackIDs maps a skill's front-matter tech_stack name to this agent's stack; only called when the agent's definition changed.
func (s *Service) reconcileSkills(ctx context.Context, agent domain.Agent, def domain.UpstreamAgent, stackIDs map[string]uuid.UUID, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) error {
	dbSkills, err := s.store.ListSkillsByAgent(ctx, agent.ID)
	if err != nil {
		return err
	}
	byName := make(map[string]domain.Skill, len(dbSkills))
	for _, sk := range dbSkills {
		byName[sk.Name] = sk
	}

	for _, usk := range def.Skills {
		local, ok := byName[usk.Name]
		if !ok {
			if len(dbSkills) >= s.skillBudget {
				s.park(ctx, syncStore, domain.CatalogPending{
					AgentSlug: def.Slug, AgentName: agent.Name,
					Kind: domain.CatalogPendingKindSkill, Name: usk.Name,
					Action: domain.CatalogPendingActionCreate,
					Reason: fmt.Sprintf("skill budget dolu (%d)", s.skillBudget),
				})
				res.Skipped++
				continue
			}
			stackID := stackIDs[stackKey(usk.TechStack)]
			if err := s.ingestSkill(ctx, agent.ID, usk, &stackID); err != nil {
				return fmt.Errorf("create skill %s: %w", usk.Name, err)
			}
			res.Created++
			log.Info().Str("agent", agent.Name).Str("skill", usk.Name).Msg("catalog: skill ingested")
			continue
		}

		switch {
		case local.CatalogSha == usk.Sha:
			if err := s.syncSkillEnabled(ctx, local, usk); err != nil {
				return err
			}
			continue
		case hashContent(local.Content) == usk.Sha:
			// Same content under a re-applied repo's new sha: adopt.
			if err := s.stampSkillSHA(ctx, local, usk.Sha); err != nil {
				return err
			}
			if err := s.syncSkillEnabled(ctx, local, usk); err != nil {
				return err
			}
			continue
		case local.CatalogSha == "" || hashContent(local.Content) != local.CatalogSha:
			// Never applied from the catalog or edited locally since: repo and agent diverged.
			if err := s.mergeOrPark(ctx, agent, local, usk, def.Slug, syncStore, res); err != nil {
				return err
			}
		default:
			if err := s.applyUpstreamSkill(ctx, agent, local, usk); err != nil {
				return err
			}
			res.Updated++
			log.Info().Str("agent", agent.Name).Str("skill", usk.Name).Msg("catalog: skill updated from upstream")
		}
	}

	// Deletions: a local edit or keep_skills_updated off keeps a catalog-born skill the repo no longer has.
	for name, local := range byName {
		if local.CatalogSha == "" {
			continue
		}
		if _, still := skillByName(def.Skills, name); still {
			continue
		}
		if hashContent(local.Content) != local.CatalogSha {
			s.park(ctx, syncStore, domain.CatalogPending{
				AgentSlug: def.Slug, AgentName: agent.Name,
				Kind: domain.CatalogPendingKindSkill, Name: name,
				Action: domain.CatalogPendingActionDelete,
				Reason: "localde degistirildi; silinmedi",
			})
			res.Skipped++
			continue
		}
		if !agent.KeepSkillsUpdated {
			s.park(ctx, syncStore, domain.CatalogPending{
				AgentSlug: def.Slug, AgentName: agent.Name,
				Kind: domain.CatalogPendingKindSkill, Name: name,
				Action: domain.CatalogPendingActionDelete,
				Reason: "keep_skills_updated kapali; korundu",
			})
			res.Skipped++
			continue
		}
		if err := s.DeleteSkillForAgent(ctx, agent.ID, local.ID); err != nil {
			return err
		}
		res.Updated++
		log.Info().Str("agent", agent.Name).Str("skill", name).Msg("catalog: skill removed upstream, deleted")
	}
	return nil
}

func (s *Service) mergeOrPark(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill, slug string, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) error {
	if !agent.KeepSkillsUpdated {
		s.park(ctx, syncStore, domain.CatalogPending{
			AgentSlug: slug, AgentName: agent.Name,
			Kind: domain.CatalogPendingKindSkill, Name: usk.Name,
			Action: domain.CatalogPendingActionMerge,
			Reason: "keep_skills_updated kapali; local kopya korundu",
		})
		res.Skipped++
		return nil
	}
	if err := s.mergeSkill(ctx, agent, local, usk); err != nil {
		s.park(ctx, syncStore, domain.CatalogPending{
			AgentSlug: slug, AgentName: agent.Name,
			Kind: domain.CatalogPendingKindSkill, Name: usk.Name,
			Action: domain.CatalogPendingActionMerge,
			Reason: "LLM merge basarisiz: " + truncateReason(err.Error()),
		})
		res.Skipped++
		return nil
	}
	res.Merged++
	log.Info().Str("agent", agent.Name).Str("skill", usk.Name).Msg("catalog: skill merged via LLM")
	return nil
}

func (s *Service) ingestSkill(ctx context.Context, agentID uuid.UUID, usk domain.UpstreamSkill, stackID *uuid.UUID) error {
	var techStackID *uuid.UUID
	if stackID != nil && *stackID != uuid.Nil {
		techStackID = stackID
	}
	created, err := s.CreateSkillForAgent(ctx, agentID, domain.CreateSkillRequest{
		Name: usk.Name, Description: usk.Description, Category: usk.Category,
		Tags: []string{}, Content: usk.Content, Enabled: usk.Enabled, TechStackID: techStackID,
	})
	if err != nil {
		return err
	}
	return s.stampSkillSHA(ctx, created, usk.Sha)
}

// Keeps the skill's current tech stack (filing is the user's organisation) but carries the upstream enable flag — a deferred skill must not come back on.
func (s *Service) applyUpstreamSkill(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill) error {
	_, err := s.UpdateSkillForAgent(ctx, agent.ID, local.ID, domain.UpdateSkillRequest{
		Name: usk.Name, Description: usk.Description, Category: usk.Category,
		Tags: local.Tags, Content: usk.Content, Enabled: usk.Enabled,
		TechStackID: local.TechStackID,
	})
	if err != nil {
		return err
	}
	updated, err := s.store.GetSkill(ctx, local.ID)
	if err != nil {
		return err
	}
	return s.stampSkillSHA(ctx, updated, usk.Sha)
}

// Flips only the enable flag so a deferred capability does not force an embedding pass.
func (s *Service) syncSkillEnabled(ctx context.Context, local domain.Skill, usk domain.UpstreamSkill) error {
	if local.Enabled == usk.Enabled {
		return nil
	}
	local.Enabled = usk.Enabled
	_, err := s.store.UpdateSkill(ctx, local)
	return err
}

// Creates any stack the upstream names that this agent lacks; existing stacks are the user's, left alone.
func (s *Service) ensureTechStacks(ctx context.Context, agentID uuid.UUID, def domain.UpstreamAgent) (map[string]uuid.UUID, error) {
	existing, err := s.store.ListTechStacksByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]uuid.UUID, len(existing)+len(def.TechStacks))
	for _, st := range existing {
		ids[stackKey(st.Name)] = st.ID
	}
	wanted := make([]domain.CreateTechStackRequest, 0, len(def.TechStacks))
	seen := make(map[string]bool, len(def.TechStacks))
	add := func(req domain.CreateTechStackRequest) {
		key := stackKey(req.Name)
		if key == "" || seen[key] || ids[key] != uuid.Nil {
			return
		}
		seen[key] = true
		wanted = append(wanted, req)
	}
	for _, st := range def.TechStacks {
		add(st)
	}
	// A stack a skill names but the manifest omits still has to exist for the skill to be filed under it.
	for _, sk := range def.Skills {
		add(domain.CreateTechStackRequest{Name: strings.TrimSpace(sk.TechStack), Position: len(wanted) + 1})
	}
	for _, req := range wanted {
		created, err := s.CreateTechStackForAgent(ctx, agentID, req)
		if err != nil {
			return nil, fmt.Errorf("create tech stack %s: %w", req.Name, err)
		}
		ids[stackKey(created.Name)] = created.ID
	}
	return ids, nil
}

// Creates any KPI the upstream names that this agent lacks, matched by metric_key; existing KPIs and their targets/weights are the user's.
func (s *Service) ensureKPIs(ctx context.Context, agentID uuid.UUID, def domain.UpstreamAgent) error {
	if s.kpis == nil || len(def.KPIs) == 0 {
		return nil
	}
	existing, err := s.kpis.ListByAgent(ctx, agentID)
	if err != nil {
		return err
	}
	have := make(map[string]bool, len(existing))
	for _, k := range existing {
		have[k.MetricKey] = true
	}
	for _, kpiReq := range def.KPIs {
		if have[kpiReq.MetricKey] {
			continue
		}
		if _, err := s.kpis.CreateKPI(ctx, domain.AgentKPI{
			AgentID: agentID, MetricKey: kpiReq.MetricKey, Name: kpiReq.Name,
			Description: kpiReq.Description, Period: kpiReq.Period,
			TargetFull: kpiReq.TargetFull, TargetHalf: kpiReq.TargetHalf,
			Weight: kpiReq.Weight, Enabled: kpiReq.Enabled,
		}); err != nil {
			return fmt.Errorf("create kpi %s: %w", kpiReq.MetricKey, err)
		}
	}
	return nil
}

// Rewrites only the revision marker, so an already-embedded write is not re-embedded.
func (s *Service) stampSkillSHA(ctx context.Context, skill domain.Skill, sha string) error {
	skill.CatalogSha = sha
	_, err := s.store.UpdateSkill(ctx, skill)
	return err
}

// Seeds catalog defaults into agent_column_instructions without clobbering operator prompts; clearing one back to empty resets to the default on next sync.
func (s *Service) reconcileColumnInstructions(ctx context.Context, agentID uuid.UUID, def domain.UpstreamAgent) error {
	if s.boardConfig == nil || len(def.ColumnInstructions) == 0 {
		return nil
	}
	stored, err := s.boardConfig.ListAgentColumnInstructions(ctx, agentID)
	if err != nil {
		return fmt.Errorf("list column instructions for %s: %w", def.Name, err)
	}
	current := make(map[string]string, len(stored))
	for _, ins := range stored {
		current[ins.ColumnSlug] = ins.Instruction
	}
	for _, ci := range def.ColumnInstructions {
		if have := current[string(ci.Column)]; have != "" && have != ci.Instruction {
			continue
		}
		if err := s.boardConfig.SetAgentColumnInstruction(ctx, agentID, string(ci.Column), ci.Instruction); err != nil {
			return fmt.Errorf("set column instruction %s/%s: %w", def.Name, ci.Column, err)
		}
	}
	return nil
}

func (s *Service) park(ctx context.Context, syncStore port.CatalogSyncStore, pending domain.CatalogPending) {
	if syncStore == nil {
		return
	}
	if err := syncStore.AppendCatalogPending(ctx, pending); err != nil {
		log.Warn().Err(err).Msg("catalog pending record failed")
	}
}

// The pending count is re-read: it is the sum of every sync that parked something, not just this one's.
func (s *Service) recordSync(ctx context.Context, syncStore port.CatalogSyncStore, ref string, res *domain.CatalogSyncResult, errStr string) {
	if syncStore == nil {
		return
	}
	pending := 0
	if items, err := syncStore.ListCatalogPending(ctx); err == nil {
		pending = len(items)
	}
	if err := syncStore.SaveCatalogSyncState(ctx, domain.CatalogSyncState{
		RepoRef:      ref,
		LastSyncAt:   time.Now(),
		LastError:    errStr,
		LastSummary:  res,
		PendingCount: pending,
	}); err != nil {
		log.Warn().Err(err).Msg("catalog sync state save failed")
	}
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func truncateReason(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func skillByName(skills []domain.UpstreamSkill, name string) (domain.UpstreamSkill, bool) {
	for _, sk := range skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return domain.UpstreamSkill{}, false
}

// Gives a pre-slug install's same-named agent the catalog identity instead of duplicating it; a hand-edited agent keeps auto_pull off so the diff surfaces as a parked update.
func (s *Service) adoptByName(ctx context.Context, existing domain.Agent, def domain.UpstreamAgent, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) (domain.Agent, error) {
	identical, err := s.agentContentMatches(ctx, existing, def)
	if err != nil {
		return domain.Agent{}, err
	}
	adopted := existing
	adopted.CatalogSlug = def.Slug
	adopted.CatalogEtag = def.Etag
	if !identical {
		adopted.CatalogEtag = ""
		adopted.AutoPullAgentUpdates = false
		s.park(ctx, syncStore, domain.CatalogPending{
			AgentSlug: def.Slug, AgentName: existing.Name,
			Kind: domain.CatalogPendingKindAgent, Name: def.Slug,
			Action: domain.CatalogPendingActionUpdate,
			Reason: "mevcut agent catalog ile ayni degil; slug atandi, auto_pull kapali",
		})
	}
	if _, err := s.store.UpdateAgent(ctx, adopted); err != nil {
		return domain.Agent{}, err
	}
	stackIDs, err := s.ensureTechStacks(ctx, adopted.ID, def)
	if err != nil {
		return domain.Agent{}, err
	}
	if err := s.ensureKPIs(ctx, adopted.ID, def); err != nil {
		return domain.Agent{}, err
	}
	if err := s.reconcileSkills(ctx, adopted, def, stackIDs, syncStore, res); err != nil {
		return domain.Agent{}, err
	}
	if identical {
		res.Updated++
	} else {
		res.Skipped++
	}
	log.Info().Str("agent", existing.Name).Str("slug", def.Slug).Bool("identical", identical).
		Msg("catalog: existing agent adopted by name")
	return adopted, nil
}

// Compares every field a hand-edit could change, deliberately excluding operational runtime fields (provider, models, effort, max_turns) so an install's choices survive.
func (s *Service) agentContentMatches(ctx context.Context, existing domain.Agent, def domain.UpstreamAgent) (bool, error) {
	if existing.Description != def.Description ||
		existing.SubagentType != def.SubagentType ||
		existing.SystemPrompt != def.SystemPrompt ||
		existing.Enabled != def.Enabled ||
		existing.SelfEvolutionEnabled != def.SelfEvolution {
		return false, nil
	}
	if !toolPolicyEqual(existing.ToolPolicy, def.ToolPolicy) {
		return false, nil
	}
	skills, err := s.store.ListSkillsByAgent(ctx, existing.ID)
	if err != nil {
		return false, err
	}
	byName := make(map[string]domain.Skill, len(skills))
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	if len(skills) != len(def.Skills) {
		return false, nil
	}
	for _, usk := range def.Skills {
		local, ok := byName[usk.Name]
		if !ok || hashContent(local.Content) != usk.Sha || local.Enabled != usk.Enabled {
			return false, nil
		}
	}
	rules, err := s.store.ListRulesByAgent(ctx, existing.ID)
	if err != nil {
		return false, err
	}
	if len(rules) != len(def.Rules) {
		return false, nil
	}
	ruleByName := make(map[string]domain.OrchestratorRule, len(rules))
	for _, r := range rules {
		ruleByName[r.Name] = r
	}
	for _, ur := range def.Rules {
		local, ok := ruleByName[ur.Name]
		if !ok || local.Content != ur.Content || local.Priority != ur.Priority || local.Enabled != ur.Enabled {
			return false, nil
		}
	}
	return true, nil
}
