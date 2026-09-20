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

// syncMu serializes the boot sync, the interval sync and a manual button sync:
// two of them running concurrently would read the same catalog state and write
// each other's reconciliation results back.
var syncMu sync.Mutex

// skillBudget bounds how many skills an agent may get from the external
// catalog, mirroring evolution's max_skills_per_agent so the two writers agree
// on what "full" means.
func (s *Service) SetSkillBudget(max int) { s.skillBudget = max }

// SyncFromCatalog reconciles live agents and skills against the external
// catalog. New upstream agents and new upstream skills are always applied.
// The two per-agent toggles gate the rest, in the user's own words:
//   - auto_pull_agent_updates OFF = "I hand-edited this agent": the agent's own
//     prompt/roles/etc are never overwritten; only its skills still flow.
//   - keep_skills_updated ("guncel tut") OFF = this agent keeps its own copy:
//     an uncontested upstream skill change is still applied, but a skill
//     changed BOTH upstream and locally is kept local and parked, not merged.
//
// A change the rules cannot apply lands in catalog_pending for the user to see.
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
	for _, a := range agents {
		if a.CatalogSlug != "" {
			bySlug[a.CatalogSlug] = a
		}
	}

	res := &domain.CatalogSyncResult{RepoRef: ref}
	for _, def := range defs {
		existing, ok := bySlug[def.Slug]
		if !ok {
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
		if err := s.reconcileSkills(ctx, existing, def, syncStore, res); err != nil {
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
		// One agent's refusal (unknown provider, host without its CLI runner)
		// must not take the whole catalog down with it.
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
	for _, r := range def.Rules {
		if _, err := s.CreateRuleForAgent(ctx, agent.ID, domain.CreateOrchestratorRuleRequest{
			Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		}); err != nil {
			return fmt.Errorf("create rule %s: %w", r.Name, err)
		}
	}
	if err := s.reconcileSkills(ctx, agent, def, syncStore, res); err != nil {
		return err
	}
	res.Created++
	log.Info().Str("agent", def.Name).Str("slug", def.Slug).Msg("catalog: new agent ingested")
	return nil
}

// applyUpstreamAgent overwrites an existing agent's definition from the
// catalog, only while auto_pull_agent_updates is on. The identity columns and
// the two toggles the user owns are preserved across the overwrite.
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

// reconcileSkills applies the skill rules onto one agent and is only called
// when that agent's definition changed (its Etag drives it), so an untouched
// agent is not touched.
func (s *Service) reconcileSkills(ctx context.Context, agent domain.Agent, def domain.UpstreamAgent, syncStore port.CatalogSyncStore, res *domain.CatalogSyncResult) error {
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
			if err := s.ingestSkill(ctx, agent.ID, usk); err != nil {
				return fmt.Errorf("create skill %s: %w", usk.Name, err)
			}
			res.Created++
			log.Info().Str("agent", agent.Name).Str("skill", usk.Name).Msg("catalog: skill ingested")
			continue
		}

		switch {
		case local.CatalogSha == usk.Sha:
			// Already exactly this revision.
			continue
		case hashContent(local.Content) == usk.Sha:
			// Same content under different marker (re-applied repo): adopt.
			if err := s.stampSkillSHA(ctx, local, usk.Sha); err != nil {
				return err
			}
			continue
		case local.CatalogSha == "" || hashContent(local.Content) != local.CatalogSha:
			// Never applied from the catalog, or edited here since the last
			// apply — in both cases the repo and this agent diverged.
			if err := s.mergeOrPark(ctx, agent, local, usk, def.Slug, syncStore, res); err != nil {
				return err
			}
		default:
			// Local untouched since the last apply, upstream changed: apply.
			if err := s.applyUpstreamSkill(ctx, agent, local, usk); err != nil {
				return err
			}
			res.Updated++
			log.Info().Str("agent", agent.Name).Str("skill", usk.Name).Msg("catalog: skill updated from upstream")
		}
	}

	// Deletions: a catalog-born skill the repo no longer has. A local edit
	// keeps it, and so does the keep_skills_updated toggle being off.
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

func (s *Service) ingestSkill(ctx context.Context, agentID uuid.UUID, usk domain.UpstreamSkill) error {
	created, err := s.CreateSkillForAgent(ctx, agentID, domain.CreateSkillRequest{
		Name: usk.Name, Description: usk.Description, Category: usk.Category,
		Tags: []string{}, Content: usk.Content, Enabled: true,
	})
	if err != nil {
		return err
	}
	return s.stampSkillSHA(ctx, created, usk.Sha)
}

func (s *Service) applyUpstreamSkill(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill) error {
	_, err := s.UpdateSkillForAgent(ctx, agent.ID, local.ID, domain.UpdateSkillRequest{
		Name: usk.Name, Description: usk.Description, Category: usk.Category,
		Tags: local.Tags, Content: usk.Content, Enabled: true,
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

// stampSkillSHA rewrites only the revision marker, keeping the embedding
// exactly as the create/update computed it — that is what avoids a second
// embedding pass on top of an already-embedded write.
func (s *Service) stampSkillSHA(ctx context.Context, skill domain.Skill, sha string) error {
	skill.CatalogSha = sha
	_, err := s.store.UpdateSkill(ctx, skill)
	return err
}

func (s *Service) park(ctx context.Context, syncStore port.CatalogSyncStore, pending domain.CatalogPending) {
	if syncStore == nil {
		return
	}
	if err := syncStore.AppendCatalogPending(ctx, pending); err != nil {
		log.Warn().Err(err).Msg("catalog pending record failed")
	}
}

// recordSync writes the sync's outcome into the one-row status table. The
// pending count is re-read each time (it is the sum of every sync that parked
// something, not just this one's).
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
