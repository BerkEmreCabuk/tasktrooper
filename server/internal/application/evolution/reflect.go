package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// skillSnapshot / ruleSnapshot are the compact before/after payloads stored on
// evolution events (embeddings intentionally excluded).
type skillSnapshot struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags,omitempty"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id,omitempty"`
	SourceURLs  []string   `json:"source_urls,omitempty"`
}

type ruleSnapshot struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Content  string    `json:"content"`
	Priority int       `json:"priority"`
	Enabled  bool      `json:"enabled"`
}

func snapshotSkill(sk domain.Skill, sources []string) json.RawMessage {
	raw, _ := json.Marshal(skillSnapshot{
		ID: sk.ID, Name: sk.Name, Description: sk.Description, Category: sk.Category,
		Tags: sk.Tags, Content: sk.Content, Enabled: sk.Enabled, TechStackID: sk.TechStackID,
		SourceURLs: sources,
	})
	return raw
}

func snapshotRule(r domain.OrchestratorRule) json.RawMessage {
	raw, _ := json.Marshal(ruleSnapshot{ID: r.ID, Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled})
	return raw
}

func (s *Service) processReflection(ctx context.Context, reflection domain.AgentReflection) {
	agentRec, err := s.catalog.GetAgent(ctx, reflection.AgentID)
	if err != nil {
		s.failReflection(ctx, reflection, "", fmt.Sprintf("agent not found: %v", err))
		return
	}

	evidence, skills, rules, baseline, err := s.gatherEvidence(ctx, agentRec, reflection)
	if err != nil {
		s.failReflection(ctx, reflection, "", fmt.Sprintf("evidence gathering failed: %v", err))
		return
	}

	output, rawOutput, analysis, err := s.runLLM(ctx, agentRec, evidence)
	if err != nil {
		s.failReflection(ctx, reflection, rawOutput, err.Error())
		return
	}

	catalogBefore := domain.ReflectionCatalogCounts{
		Skills: len(skills), Rules: len(rules), Memories: s.countMemories(ctx, agentRec.ID),
		SkillBudget: s.cfg.MaxSkillsPerAgent, RuleBudget: s.cfg.MaxRulesPerAgent,
	}

	// The gate needs a baseline measured against the pre-change catalog, so it
	// runs before anything is applied — but only when the reflection actually
	// proposes catalog changes, otherwise it is a suite run for nothing.
	gate := s.cfg.GoldenGate && agentRec.SelfEvolutionEnabled && proposesCatalogChange(output)
	var beforeRun goldenRun
	if gate {
		beforeRun = s.runGoldenEval(ctx, agentRec, reflection.ID, goldenPhaseBefore)
	}

	applyLog, events, outcomes := s.applyOutput(ctx, agentRec, reflection, output, skills, rules)

	selfAssessment := strings.TrimSpace(output.SelfAssessment)
	summary := selfAssessment
	if summary == "" && analysis != "" {
		summary = truncate(analysis, 600)
	}
	if len(applyLog) > 0 {
		summary = summary + "\n\nApplied changes:\n- " + strings.Join(applyLog, "\n- ")
	}

	snapshot := s.captureSnapshot(ctx, reflection.AgentID)
	var gateResult *domain.ReflectionGateResult
	if len(applyLog) > 0 && agentRec.SelfEvolutionEnabled {
		afterRun := s.runGoldenEval(ctx, agentRec, reflection.ID, goldenPhaseAfter)
		if afterRun.ran() {
			rate := afterRun.Rate
			snapshot.GoldenPassRate = &rate
			summary += "\n\n" + goldenSummaryLine(rate, afterRun.Evaluated)
		}
		if gate && beforeRun.ran() && afterRun.ran() {
			beforeRate := beforeRun.Rate
			snapshot.GoldenPassRateBefore = &beforeRate
			var line string
			line, gateResult = s.enforceGoldenGate(ctx, agentRec, reflection, beforeRun, afterRun, applyLog, events, outcomes)
			summary += line
		}
	}
	if len(summary) > 4000 {
		summary = summary[:4000]
	}

	catalogAfter := domain.ReflectionCatalogCounts{
		Skills: catalogBefore.Skills, Rules: catalogBefore.Rules,
		SkillBudget: s.cfg.MaxSkillsPerAgent, RuleBudget: s.cfg.MaxRulesPerAgent,
	}
	if after, err := s.catalog.ListSkillsByAgent(ctx, agentRec.ID); err == nil {
		catalogAfter.Skills = len(after)
	}
	if after, err := s.catalog.ListRulesByAgent(ctx, agentRec.ID); err == nil {
		catalogAfter.Rules = len(after)
	}
	catalogAfter.Memories = s.countMemories(ctx, agentRec.ID)

	reflection.Status = domain.ReflectionStatusCompleted
	reflection.Summary = summary
	reflection.RawOutput = rawOutput
	reflection.PerformanceSnapshot = snapshot
	reflection.Decision = &domain.ReflectionDecision{
		Analysis:       analysis,
		SelfAssessment: selfAssessment,
		Baseline:       baseline,
		CatalogBefore:  catalogBefore,
		CatalogAfter:   catalogAfter,
		Changes:        outcomes,
		Gate:           gateResult,
	}
	if err := s.store.CompleteReflection(ctx, reflection); err != nil {
		log.Warn().Err(err).Msg("complete reflection failed")
	}
	log.Info().Str("agent", agentRec.Name).Str("trigger", reflection.Trigger).Int("changes", len(applyLog)).Msg("reflection completed")
}

// countMemories is the catalog-count half of ReflectionDecision's before/after
// bookkeeping; the query mirrors gatherEvidence's own listing (every scope, one
// agent) but with a high limit since this is a count, not evidence to show.
func (s *Service) countMemories(ctx context.Context, agentID uuid.UUID) int {
	if s.memories == nil {
		return 0
	}
	mems, err := s.memories.List(ctx, domain.MemoryQuery{
		AgentID: agentID, Owner: domain.MemoryOwnerAgent, Repo: domain.MemoryRepoScopeAny, Limit: 1000,
	})
	if err != nil {
		return 0
	}
	return len(mems)
}

func (s *Service) failReflection(ctx context.Context, reflection domain.AgentReflection, rawOutput, errMsg string) {
	reflection.Status = domain.ReflectionStatusFailed
	reflection.RawOutput = rawOutput
	reflection.Error = errMsg
	if err := s.store.CompleteReflection(ctx, reflection); err != nil {
		log.Warn().Err(err).Msg("fail reflection persist failed")
	}
	log.Warn().Str("reflection_id", reflection.ID.String()).Str("error", errMsg).Msg("reflection failed")
}

func (s *Service) captureSnapshot(ctx context.Context, agentID uuid.UUID) *domain.PerformanceSnapshot {
	snap := &domain.PerformanceSnapshot{CapturedAt: time.Now(), KPIs: map[string]float64{}}
	if score, err := s.perf.GetScore(ctx, agentID); err == nil {
		snap.Score = score.Score
	}
	if s.kpis != nil {
		kpis, err := s.kpis.ListKPIs(ctx, agentID)
		if err == nil {
			results, _ := s.kpis.LatestResults(ctx, agentID)
			snap.KPIComposite = kpiComposite(kpis, results)
			byID := map[uuid.UUID]string{}
			for _, k := range kpis {
				byID[k.ID] = k.MetricKey
			}
			for _, r := range results {
				if key, ok := byID[r.KPIID]; ok {
					snap.KPIs[key] = r.Attainment
				}
			}
		}
	}
	return snap
}

func (s *Service) runLLM(ctx context.Context, agentRec domain.Agent, evidence string) (domain.ReflectionOutput, string, string, error) {
	system := reflectionSystemPrompt(agentRec, s.cfg)
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: system},
		{Role: domain.RoleUser, Content: evidence},
	}

	// Model routing: reflection rewrites the agent's own instructions, which is
	// the hardest kind of work the agent owns — so it runs on the agent's
	// ModelHeavy when one is set, exactly like a "hard" orchestrator subtask.
	// evolution.model / evolution.provider_type stay the operator override.
	model := agentRec.Model
	if agentRec.ModelHeavy != "" {
		model = agentRec.ModelHeavy
	}
	provider := agentRec.ProviderType
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}

	callOnce := func(msgs []domain.Message) (string, error) {
		if s.cfg.AllowWebResearch && s.agentLoop != nil {
			policy := domain.ToolPolicy{AllowTools: []string{"web_search", "fetch_url"}}
			// s.agentLoop is the router, so an agent on a host-executed provider
			// reflects on its own performance through that host's CLI.
			// WithScratchWorkspace because this is the one agentic path with no
			// repository at all: it is a background cron reading the agent's own
			// history and the web, so a fresh empty directory is the honest place
			// to run it — there is no code for it to be scoped to.
			resp, err := s.agentLoop.Run(ctx, msgs, model, provider, policy,
				agent.WithSessionLimits(agentRec.MaxTurns, agentRec.Effort),
				agent.WithCLILabel("reflect:"+agentRec.Name, "agent self-reflection"),
				agent.WithScratchWorkspace())
			if err != nil {
				return "", err
			}
			return resp.Message.Content, nil
		}
		resp, err := s.llm.Chat(ctx, domain.AgentRequest{
			Messages: msgs, ProviderType: provider, Model: model,
			ResponseFormat: domain.JSONSchemaResponseFormat("agent_reflection", reflectionOutputSchema()),
		})
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}

	raw, err := callOnce(messages)
	if err != nil {
		return domain.ReflectionOutput{}, "", "", fmt.Errorf("reflection llm call failed: %w", err)
	}
	output, analysis, parseErr := parseReflectionOutput(raw)
	if parseErr == nil {
		return output, raw, analysis, nil
	}

	retryMsgs := append(messages,
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: "Your previous output was not valid JSON (" + parseErr.Error() + "). Respond again with ONLY the JSON object, no prose, no code fences."},
	)
	raw2, err := callOnce(retryMsgs)
	if err != nil {
		return domain.ReflectionOutput{}, raw, "", fmt.Errorf("reflection retry failed: %w", err)
	}
	output, analysis, parseErr = parseReflectionOutput(raw2)
	if parseErr != nil {
		return domain.ReflectionOutput{}, raw2, "", fmt.Errorf("reflection output unparseable after retry: %w", parseErr)
	}
	return output, raw2, analysis, nil
}

func (s *Service) applyOutput(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	output domain.ReflectionOutput,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
) ([]string, []domain.AgentEvolutionEvent, []domain.ReflectionChangeOutcome) {
	var applied []string
	var events []domain.AgentEvolutionEvent
	var outcomes []domain.ReflectionChangeOutcome
	scoreAt := 100.0
	if score, err := s.perf.GetScore(ctx, agentRec.ID); err == nil {
		scoreAt = score.Score
	}
	// Everything the reflection writes into the catalog is attributed to this
	// reflection in the version history, so a human reading a skill's history
	// can tell a self-evolution rewrite from their own edit.
	ctx = catalog.WithVersionSource(ctx, catalog.VersionSource{
		Source: domain.CatalogVersionSourceEvolution, ReflectionID: &reflection.ID,
	})
	// recordEvent returns the persisted event (and any store error) so callers
	// can attach the event's id to the ReflectionChangeOutcome they record for
	// the same change — the two records need to point at each other so the
	// golden gate can later mark an outcome rolled_back by matching event ids.
	recordEvent := func(e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
		e.ReflectionID = &reflection.ID
		e.AgentID = agentRec.ID
		e.ScoreAtChange = scoreAt
		created, err := s.store.CreateEvent(ctx, e)
		if err != nil {
			log.Warn().Err(err).Str("change", e.ChangeType).Msg("evolution event persist failed")
			return domain.AgentEvolutionEvent{}, err
		}
		events = append(events, created)
		return created, nil
	}
	recordOutcome := func(o domain.ReflectionChangeOutcome) {
		outcomes = append(outcomes, o)
	}
	skillByID := map[string]domain.Skill{}
	for _, sk := range skills {
		skillByID[sk.ID.String()] = sk
	}
	ruleByID := map[string]domain.OrchestratorRule{}
	for _, r := range rules {
		ruleByID[r.ID.String()] = r
	}

	if agentRec.SelfEvolutionEnabled {
		applied = append(applied, s.applySkillChanges(ctx, agentRec, output.Skills, skillByID, recordEvent, recordOutcome)...)
		applied = append(applied, s.applyRuleChanges(ctx, agentRec, output.Rules, ruleByID, recordEvent, recordOutcome)...)
		applied = append(applied, s.applyReverts(ctx, agentRec, output.Reverts, recordEvent, recordOutcome)...)
	} else {
		if len(output.Skills) > 0 || len(output.Rules) > 0 || len(output.Reverts) > 0 {
			log.Info().Str("agent", agentRec.Name).Msg("skill/rule changes proposed but self_evolution_enabled=false; skipped")
		}
		for _, ch := range output.Skills {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: ch.Action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
		for _, ch := range output.Rules {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: ch.Action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
		for _, rv := range output.Reverts {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
	}

	maxMem := s.cfg.MaxMemoryChanges
	for i, mc := range output.Memories {
		if i >= maxMem {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "memory", Action: mc.Action, Name: truncate(mc.Content, 60), TargetID: mc.MemoryID, Reason: mc.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "memory change cap reached",
			})
			continue
		}
		switch mc.Action {
		case "create":
			if s.memories == nil {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "memory service unavailable",
				})
				continue
			}
			// The evidence this reads is a pile of run transcripts, so the
			// easiest thing for it to produce is a summary of one of them.
			// Same bar as save_memory: a note tied to one card is a run log,
			// and the card already holds it. Skipped rather than failed —
			// the rest of the reflection is still worth applying.
			if reason := domain.MemoryRunLogReason(mc.Content); reason != "" {
				log.Info().Str("agent", agentRec.Name).Str("reason", reason).
					Str("content", truncate(mc.Content, 80)).Msg("reflection memory skipped: run log, not a durable lesson")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: reason,
				})
				continue
			}
			// Reflection reasons over runs from every repository at once, so it
			// has no single project to bind a lesson to: what it saves is
			// global. Repository-specific lessons come from save_memory during
			// the run itself, where the repository is known.
			mem, err := s.memories.Save(ctx, agentRec.ID, nil, mc.Content, mc.Category, domain.MemorySourceReflection)
			if err != nil {
				log.Warn().Err(err).Msg("reflection memory save failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			after, _ := json.Marshal(map[string]string{"content": mc.Content, "category": mc.Category})
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeMemoryCreated, TargetKind: domain.EvolutionTargetMemory,
				TargetID: &mem.ID, TargetName: truncate(mc.Content, 80), After: after,
				Impact: domain.EvolutionImpactNeutral,
			})
			applied = append(applied, "memory saved: "+truncate(mc.Content, 60))
			outcome := domain.ReflectionChangeOutcome{
				Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), TargetID: mem.ID.String(),
				Reason: mc.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			if s.memories == nil || mc.MemoryID == "" {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "missing memory id",
				})
				continue
			}
			memID, err := uuid.Parse(mc.MemoryID)
			if err != nil {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "invalid memory id",
				})
				continue
			}
			if err := s.memories.Delete(ctx, agentRec.ID, memID); err != nil {
				log.Warn().Err(err).Msg("reflection memory delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeMemoryDeleted, TargetKind: domain.EvolutionTargetMemory,
				TargetID: &memID, TargetName: mc.MemoryID, Impact: domain.EvolutionImpactNeutral,
			})
			applied = append(applied, "memory deleted: "+mc.MemoryID)
			outcome := domain.ReflectionChangeOutcome{
				Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
				Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied, events, outcomes
}

func (s *Service) applySkillChanges(
	ctx context.Context,
	agentRec domain.Agent,
	changes []domain.ReflectionSkillChange,
	skillByID map[string]domain.Skill,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	count := 0
	// budget is the standing catalog size, tracked across this batch so a
	// reflection cannot slip three creates past a cap it was already at.
	budget := len(skillByID)
	byName := map[string]domain.Skill{}
	for _, sk := range skillByID {
		byName[strings.ToLower(strings.TrimSpace(sk.Name))] = sk
	}
	for _, ch := range changes {
		if count >= s.cfg.MaxSkillChanges {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: ch.Action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "max skill changes per reflection reached",
			})
			continue
		}
		// A create that names an existing skill is the model trying to replace
		// it; treat it as the update it meant, instead of adding a second
		// skill with the same name whose content contradicts the first.
		if ch.Action == "create" {
			if existing, ok := byName[strings.ToLower(strings.TrimSpace(ch.Name))]; ok {
				ch.Action = "update"
				ch.SkillID = existing.ID.String()
			}
		}
		if ch.Action == "create" && s.cfg.MaxSkillsPerAgent > 0 && budget >= s.cfg.MaxSkillsPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("skill", ch.Name).Int("budget", s.cfg.MaxSkillsPerAgent).
				Msg("skill create rejected: agent is at its skill budget")
			applied = append(applied, "skill create rejected (budget full, merge into an existing skill instead): "+ch.Name)
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "create", Name: ch.Name, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeRejected, Detail: "skill budget full",
			})
			continue
		}
		switch ch.Action {
		case "create":
			created, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, domain.CreateSkillRequest{
				Name: ch.Name, Description: ch.Description, Category: ch.Category, Content: ch.Content, Enabled: true,
			})
			if err != nil {
				log.Warn().Err(err).Str("skill", ch.Name).Msg("skill create failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "create", Name: ch.Name, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillCreated, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &created.ID, TargetName: created.Name, After: snapshotSkill(created, ch.SourceURLs),
			})
			applied = append(applied, "skill created: "+created.Name)
			count++
			budget++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "create", Name: created.Name, TargetID: created.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "update":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "update", Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown skill id",
				})
				continue
			}
			before := snapshotSkill(existing, nil)
			updated, err := s.manager.UpdateSkillForAgent(ctx, agentRec.ID, existing.ID, domain.UpdateSkillRequest{
				Name: orDefault(ch.Name, existing.Name), Description: orDefault(ch.Description, existing.Description),
				Category: orDefault(ch.Category, existing.Category), Tags: existing.Tags,
				Content: orDefault(ch.Content, existing.Content), Enabled: existing.Enabled,
				TechStackID: existing.TechStackID,
			})
			if err != nil {
				log.Warn().Err(err).Str("skill", existing.Name).Msg("skill update failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "update", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillUpdated, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &updated.ID, TargetName: updated.Name, Before: before, After: snapshotSkill(updated, ch.SourceURLs),
			})
			applied = append(applied, "skill updated: "+updated.Name)
			count++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "update", Name: updated.Name, TargetID: updated.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "delete", TargetID: ch.SkillID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown skill id",
				})
				continue
			}
			before := snapshotSkill(existing, nil)
			if err := s.manager.DeleteSkillForAgent(ctx, agentRec.ID, existing.ID); err != nil {
				log.Warn().Err(err).Str("skill", existing.Name).Msg("skill delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillDeleted, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &existing.ID, TargetName: existing.Name, Before: before,
			})
			applied = append(applied, "skill deleted: "+existing.Name)
			count++
			budget--
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied
}

func (s *Service) applyRuleChanges(
	ctx context.Context,
	agentRec domain.Agent,
	changes []domain.ReflectionRuleChange,
	ruleByID map[string]domain.OrchestratorRule,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	count := 0
	budget := len(ruleByID)
	byName := map[string]domain.OrchestratorRule{}
	for _, r := range ruleByID {
		byName[strings.ToLower(strings.TrimSpace(r.Name))] = r
	}
	for _, ch := range changes {
		if count >= s.cfg.MaxRuleChanges {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: ch.Action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "max rule changes per reflection reached",
			})
			continue
		}
		if ch.Action == "create" {
			if existing, ok := byName[strings.ToLower(strings.TrimSpace(ch.Name))]; ok {
				ch.Action = "update"
				ch.RuleID = existing.ID.String()
			}
		}
		if ch.Action == "create" && s.cfg.MaxRulesPerAgent > 0 && budget >= s.cfg.MaxRulesPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("rule", ch.Name).Int("budget", s.cfg.MaxRulesPerAgent).
				Msg("rule create rejected: agent is at its rule budget")
			applied = append(applied, "rule create rejected (budget full, tighten an existing rule instead): "+ch.Name)
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "create", Name: ch.Name, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeRejected, Detail: "rule budget full",
			})
			continue
		}
		switch ch.Action {
		case "create":
			created, err := s.manager.CreateRuleForAgent(ctx, agentRec.ID, domain.CreateOrchestratorRuleRequest{
				Name: ch.Name, Content: ch.Content, Priority: ch.Priority, Enabled: true,
			})
			if err != nil {
				log.Warn().Err(err).Str("rule", ch.Name).Msg("rule create failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "create", Name: ch.Name, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleCreated, TargetKind: domain.EvolutionTargetRule,
				TargetID: &created.ID, TargetName: created.Name, After: snapshotRule(created),
			})
			applied = append(applied, "rule created: "+created.Name)
			count++
			budget++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "create", Name: created.Name, TargetID: created.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "update":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "update", Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown rule id",
				})
				continue
			}
			before := snapshotRule(existing)
			priority := ch.Priority
			if priority == 0 {
				priority = existing.Priority
			}
			updated, err := s.manager.UpdateRuleForAgent(ctx, agentRec.ID, existing.ID, domain.UpdateOrchestratorRuleRequest{
				Name: orDefault(ch.Name, existing.Name), Content: orDefault(ch.Content, existing.Content),
				Priority: priority, Enabled: existing.Enabled,
			})
			if err != nil {
				log.Warn().Err(err).Str("rule", existing.Name).Msg("rule update failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "update", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleUpdated, TargetKind: domain.EvolutionTargetRule,
				TargetID: &updated.ID, TargetName: updated.Name, Before: before, After: snapshotRule(updated),
			})
			applied = append(applied, "rule updated: "+updated.Name)
			count++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "update", Name: updated.Name, TargetID: updated.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "delete", TargetID: ch.RuleID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown rule id",
				})
				continue
			}
			before := snapshotRule(existing)
			if err := s.manager.DeleteRuleForAgent(ctx, agentRec.ID, existing.ID); err != nil {
				log.Warn().Err(err).Str("rule", existing.Name).Msg("rule delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleDeleted, TargetKind: domain.EvolutionTargetRule,
				TargetID: &existing.ID, TargetName: existing.Name, Before: before,
			})
			applied = append(applied, "rule deleted: "+existing.Name)
			count++
			budget--
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied
}

// applyReverts restores the before-state of a previous evolution event.
func (s *Service) applyReverts(
	ctx context.Context,
	agentRec domain.Agent,
	reverts []domain.ReflectionRevert,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	for _, rv := range reverts {
		eventID, err := uuid.Parse(rv.EvolutionEventID)
		if err != nil {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "invalid event id",
			})
			continue
		}
		original, err := s.store.GetEvent(ctx, eventID)
		if err != nil || original.AgentID != agentRec.ID {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown evolution event",
			})
			continue
		}
		restored, err := s.revertEvent(ctx, agentRec, original)
		if err != nil {
			log.Warn().Err(err).Str("event", eventID.String()).Msg("revert failed")
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", Name: original.TargetName, TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
			})
			continue
		}
		ev, evErr := recordEvent(domain.AgentEvolutionEvent{
			ChangeType: domain.EvolutionChangeRevert, TargetKind: original.TargetKind,
			TargetID: original.TargetID, TargetName: original.TargetName,
			Before: original.After, After: original.Before, RevertedEventID: &original.ID,
		})
		applied = append(applied, "reverted: "+original.ChangeType+" "+original.TargetName+" ("+restored+")")
		outcome := domain.ReflectionChangeOutcome{
			Kind: "revert", Action: "revert", Name: original.TargetName, TargetID: rv.EvolutionEventID,
			Reason: rv.Reason, Outcome: domain.ReflectionOutcomeApplied, Detail: restored,
		}
		if evErr == nil {
			id := ev.ID
			outcome.EventID = &id
		}
		recordOutcome(outcome)
	}
	return applied
}

func (s *Service) revertEvent(ctx context.Context, agentRec domain.Agent, original domain.AgentEvolutionEvent) (string, error) {
	switch original.ChangeType {
	case domain.EvolutionChangeSkillCreated:
		if original.TargetID == nil {
			return "", fmt.Errorf("missing target id")
		}
		return "skill removed", s.manager.DeleteSkillForAgent(ctx, agentRec.ID, *original.TargetID)
	case domain.EvolutionChangeSkillUpdated:
		var snap skillSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.UpdateSkillForAgent(ctx, agentRec.ID, snap.ID, domain.UpdateSkillRequest{
			Name: snap.Name, Description: snap.Description, Category: snap.Category,
			Tags: snap.Tags, Content: snap.Content, Enabled: snap.Enabled,
			TechStackID: snap.TechStackID,
		})
		return "skill restored", err
	case domain.EvolutionChangeSkillDeleted:
		var snap skillSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, domain.CreateSkillRequest{
			Name: snap.Name, Description: snap.Description, Category: snap.Category,
			Tags: snap.Tags, Content: snap.Content, Enabled: snap.Enabled,
			TechStackID: snap.TechStackID,
		})
		return "skill recreated", err
	case domain.EvolutionChangeRuleCreated:
		if original.TargetID == nil {
			return "", fmt.Errorf("missing target id")
		}
		return "rule removed", s.manager.DeleteRuleForAgent(ctx, agentRec.ID, *original.TargetID)
	case domain.EvolutionChangeRuleUpdated:
		var snap ruleSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.UpdateRuleForAgent(ctx, agentRec.ID, snap.ID, domain.UpdateOrchestratorRuleRequest{
			Name: snap.Name, Content: snap.Content, Priority: snap.Priority, Enabled: snap.Enabled,
		})
		return "rule restored", err
	case domain.EvolutionChangeRuleDeleted:
		var snap ruleSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.CreateRuleForAgent(ctx, agentRec.ID, domain.CreateOrchestratorRuleRequest{
			Name: snap.Name, Content: snap.Content, Priority: snap.Priority, Enabled: snap.Enabled,
		})
		return "rule recreated", err
	default:
		return "", fmt.Errorf("change type %s is not revertible", original.ChangeType)
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
