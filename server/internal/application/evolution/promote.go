package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

var ErrPromotionInFlight = fmt.Errorf("a shared-memory promotion is already running")

// promotionInflightKey shares the reflection in-flight map; it can never
// collide with an agent id key because those are UUID strings.
const promotionInflightKey = "__shared_memory_promotion__"

// PlanSharedMemoryPromotion reviews every team memory and returns the ones that
// are really reusable know-how (a method, checklist, convention) as proposed
// skills. Facts, status and preferences stay memories.
//
// It writes NOTHING. The sweep used to create the skills and delete the
// memories in the same call, so the operator pressing the button found out what
// it had decided only afterwards — on a page whose whole content it had just
// rewritten. The decision is an LLM's, and both halves of it (is this really a
// skill, and who should get it) are worth a look before a memory is deleted, so
// the plan comes back for approval and ApplySharedMemoryPromotion carries out
// exactly what was approved.
func (s *Service) PlanSharedMemoryPromotion(ctx context.Context) (domain.MemoryPromotionPlan, error) {
	plan := domain.MemoryPromotionPlan{Candidates: []domain.MemoryPromotionCandidate{}, Agents: []string{}}
	if s.llm == nil || s.memories == nil || s.manager == nil || s.board == nil || s.catalog == nil {
		return plan, fmt.Errorf("memory promotion is not configured")
	}
	release, err := s.holdPromotion()
	if err != nil {
		return plan, err
	}
	defer release()

	memories, err := s.memories.List(ctx, domain.MemoryQuery{
		Owner: domain.MemoryOwnerTeam, Repo: domain.MemoryRepoScopeAny, Limit: 200,
	})
	if err != nil {
		return plan, fmt.Errorf("listing team memories: %w", err)
	}
	plan.Scanned = len(memories)
	if len(memories) == 0 {
		return plan, nil
	}

	// The manual sweep is an operator action, so it may target agents whose
	// self_evolution_enabled is off — the operator is the one deciding.
	agents, err := s.promotionTargets(ctx, false)
	if err != nil {
		return plan, err
	}
	if len(agents) == 0 {
		return plan, fmt.Errorf("no enabled agents to receive skills")
	}
	for _, a := range agents {
		plan.Agents = append(plan.Agents, a.Name)
	}

	model, provider := s.promotionModel()
	var output promotionOutput
	if err := s.promotionChat(ctx,
		teamPromotionSystemPrompt,
		teamPromotionUserMessage(memories, agents),
		model, provider,
		"memory_promotion", promotionOutputSchema(), &output,
	); err != nil {
		return plan, fmt.Errorf("memory promotion llm call failed: %w", err)
	}

	memByID := map[string]domain.AgentMemory{}
	for _, m := range memories {
		memByID[m.ID.String()] = m
	}
	agentByName := map[string]domain.Agent{}
	for _, a := range agents {
		agentByName[strings.ToLower(strings.TrimSpace(a.Name))] = a
	}

	for _, item := range output.Promotions {
		mem, ok := memByID[strings.TrimSpace(item.MemoryID)]
		if !ok {
			continue // model invented an id — never act on it
		}
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		targets := make([]string, 0, len(item.Agents))
		for _, name := range item.Agents {
			if a, ok := agentByName[strings.ToLower(strings.TrimSpace(name))]; ok {
				targets = append(targets, a.Name)
			}
		}
		// No recognizable targets means "fits everyone" — spelled out here so
		// the operator sees the real roster rather than an empty list.
		if len(targets) == 0 {
			targets = append(targets, plan.Agents...)
		}
		plan.Candidates = append(plan.Candidates, domain.MemoryPromotionCandidate{
			MemoryID: mem.ID, MemoryContent: mem.Content,
			SkillName: item.Name, Description: item.Description,
			Category: item.Category, Content: item.Content, Agents: targets,
		})
	}
	log.Info().Int("scanned", plan.Scanned).Int("candidates", len(plan.Candidates)).Msg("shared-memory promotion planned")
	return plan, nil
}

// ApplySharedMemoryPromotion writes the approved candidates: one skill per
// target agent, then the source memory is deleted — it now lives in the
// catalog. A candidate nobody could take (name collision, skill budget) leaves
// its memory alone, because deleting it would lose the lesson entirely.
//
// Everything is re-resolved against the server's own state: the memory must
// still be a team memory and each named agent must still be an enabled target.
// The candidates travel through the browser, so what comes back is a request,
// not a fact.
func (s *Service) ApplySharedMemoryPromotion(ctx context.Context, candidates []domain.MemoryPromotionCandidate) (domain.MemoryPromotionResult, error) {
	result := domain.MemoryPromotionResult{Promoted: []domain.MemoryPromotion{}}
	if s.memories == nil || s.manager == nil || s.board == nil || s.catalog == nil {
		return result, fmt.Errorf("memory promotion is not configured")
	}
	if len(candidates) == 0 {
		return result, nil
	}
	release, err := s.holdPromotion()
	if err != nil {
		return result, err
	}
	defer release()

	memories, err := s.memories.List(ctx, domain.MemoryQuery{
		Owner: domain.MemoryOwnerTeam, Repo: domain.MemoryRepoScopeAny, Limit: 200,
	})
	if err != nil {
		return result, fmt.Errorf("listing team memories: %w", err)
	}
	result.Scanned = len(candidates)
	memByID := map[uuid.UUID]domain.AgentMemory{}
	for _, m := range memories {
		memByID[m.ID] = m
	}

	agents, err := s.promotionTargets(ctx, false)
	if err != nil {
		return result, err
	}
	agentByName := map[string]domain.Agent{}
	for _, a := range agents {
		agentByName[strings.ToLower(strings.TrimSpace(a.Name))] = a
	}

	for _, item := range candidates {
		mem, ok := memByID[item.MemoryID]
		if !ok {
			log.Info().Str("memory", item.MemoryID.String()).Msg("promotion skipped: memory is gone")
			continue
		}
		if strings.TrimSpace(item.SkillName) == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		targets := make([]domain.Agent, 0, len(item.Agents))
		for _, name := range item.Agents {
			if a, ok := agentByName[strings.ToLower(strings.TrimSpace(name))]; ok {
				targets = append(targets, a)
			}
		}
		if len(targets) == 0 {
			targets = agents
		}
		granted := s.createSkillForAgents(ctx, targets, domain.CreateSkillRequest{
			Name: item.SkillName, Description: item.Description, Category: item.Category,
			Content: item.Content, Enabled: true,
		})
		if len(granted) == 0 {
			continue // memory stays: nobody could take the skill (budget/collision)
		}
		if err := s.memories.Delete(ctx, uuid.Nil, mem.ID); err != nil {
			log.Warn().Err(err).Str("memory", mem.ID.String()).Msg("promoted memory delete failed")
		}
		result.Promoted = append(result.Promoted, domain.MemoryPromotion{
			MemoryID: mem.ID, SkillName: item.SkillName, Agents: granted,
		})
	}
	log.Info().Int("approved", len(candidates)).Int("promoted", len(result.Promoted)).Msg("shared-memory promotion applied")
	return result, nil
}

// PromoteSharedMemories is plan-then-apply-everything, kept for callers that
// want the old one-shot behaviour (and as the fallback the UI uses when it
// cannot show a dialog).
func (s *Service) PromoteSharedMemories(ctx context.Context) (domain.MemoryPromotionResult, error) {
	plan, err := s.PlanSharedMemoryPromotion(ctx)
	if err != nil {
		return domain.MemoryPromotionResult{Promoted: []domain.MemoryPromotion{}}, err
	}
	result, err := s.ApplySharedMemoryPromotion(ctx, plan.Candidates)
	if err != nil {
		return result, err
	}
	result.Scanned = plan.Scanned
	return result, nil
}

// holdPromotion takes the single-flight slot both halves share, so a second
// browser tab cannot plan against memories another apply is deleting.
func (s *Service) holdPromotion() (func(), error) {
	s.mu.Lock()
	if s.inflight[promotionInflightKey] {
		s.mu.Unlock()
		return nil, ErrPromotionInFlight
	}
	s.inflight[promotionInflightKey] = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.inflight, promotionInflightKey)
		s.mu.Unlock()
	}, nil
}

// MaybePromoteMemory is the save_memory hook: it decides whether the content
// an agent is about to memorize is really reusable know-how and, when it is,
// writes it into the skill catalog instead. agentID == uuid.Nil means the save
// was aimed at team memory; the skill then goes to every enabled board agent
// that has self-evolution on. Best-effort by design: any error means "let the
// caller save it as a memory".
func (s *Service) MaybePromoteMemory(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, content, category string) (bool, string, error) {
	if !s.cfg.Enabled || s.llm == nil || s.manager == nil || s.catalog == nil {
		return false, "", nil
	}
	var targets []domain.Agent
	if agentID == uuid.Nil {
		if s.board == nil {
			return false, "", nil
		}
		agents, err := s.promotionTargets(ctx, true)
		if err != nil || len(agents) == 0 {
			return false, "", err
		}
		targets = agents
	} else {
		rec, err := s.catalog.GetAgent(ctx, agentID)
		if err != nil {
			return false, "", err
		}
		if !rec.Enabled || !rec.SelfEvolutionEnabled {
			return false, "", nil
		}
		targets = []domain.Agent{rec}
	}

	scopeNote := "global (valid in every repository)"
	if repositoryID != nil {
		scopeNote = "bound to a single repository"
	}
	var out saveClassification
	model, provider := s.promotionModel()
	if err := s.promotionChat(ctx,
		saveClassifierSystemPrompt,
		fmt.Sprintf("Scope: %s\nCategory: %s\nContent:\n%s", scopeNote, category, content),
		model, provider,
		"memory_or_skill", saveClassificationSchema(), &out,
	); err != nil {
		return false, "", err
	}
	if !out.Skill || strings.TrimSpace(out.Name) == "" || strings.TrimSpace(out.Content) == "" {
		return false, "", nil
	}
	granted := s.createSkillForAgents(ctx, targets, domain.CreateSkillRequest{
		Name: out.Name, Description: out.Description, Category: out.Category,
		Content: out.Content, Enabled: true,
	})
	if len(granted) == 0 {
		return false, "", nil
	}
	return true, out.Name, nil
}

// promotionTargets is the enabled board roster; requireSelfEvolution narrows
// it to agents that opted into catalog changes (the save_memory path — an
// agent-initiated write must respect the flag; an operator sweep may not).
func (s *Service) promotionTargets(ctx context.Context, requireSelfEvolution bool) ([]domain.Agent, error) {
	members, err := s.board.ListMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing board members: %w", err)
	}
	agents := make([]domain.Agent, 0, len(members))
	for _, m := range members {
		rec, err := s.catalog.GetAgent(ctx, m.AgentID)
		if err != nil || !rec.Enabled {
			continue
		}
		if requireSelfEvolution && !rec.SelfEvolutionEnabled {
			continue
		}
		agents = append(agents, rec)
	}
	if len(agents) > 0 {
		return agents, nil
	}
	// `board_members` is an explicit opt-in list nothing populates by default,
	// and the board itself never reads it — it renders every enabled agent as a
	// member. So an empty table means "nobody narrowed the roster", not "no
	// agents", and reading it literally is what made this refuse with "no
	// enabled board agents to receive skills" on a workspace whose board was
	// full of working agents.
	all, err := s.catalog.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	for _, rec := range all {
		if !rec.Enabled {
			continue
		}
		if requireSelfEvolution && !rec.SelfEvolutionEnabled {
			continue
		}
		agents = append(agents, rec)
	}
	return agents, nil
}

// createSkillForAgents writes one skill into each target agent's catalog,
// skipping agents that already have a skill with that name or are at their
// skill budget. Returns the names of the agents that received it.
func (s *Service) createSkillForAgents(ctx context.Context, targets []domain.Agent, req domain.CreateSkillRequest) []string {
	ctx = catalog.WithVersionSource(ctx, catalog.VersionSource{Source: domain.CatalogVersionSourceEvolution})
	var granted []string
	for _, agentRec := range targets {
		existing, err := s.catalog.ListSkillsByAgent(ctx, agentRec.ID)
		if err != nil {
			log.Warn().Err(err).Str("agent", agentRec.Name).Msg("memory promotion: list skills failed")
			continue
		}
		collision := false
		for _, sk := range existing {
			if strings.EqualFold(strings.TrimSpace(sk.Name), strings.TrimSpace(req.Name)) {
				collision = true
				break
			}
		}
		if collision {
			continue
		}
		if s.cfg.MaxSkillsPerAgent > 0 && len(existing) >= s.cfg.MaxSkillsPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("skill", req.Name).Msg("memory promotion: agent at skill budget")
			continue
		}
		created, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, req)
		if err != nil {
			log.Warn().Err(err).Str("agent", agentRec.Name).Str("skill", req.Name).Msg("memory promotion: skill create failed")
			continue
		}
		s.recordPromotionEvent(ctx, agentRec, created)
		granted = append(granted, agentRec.Name)
	}
	return granted
}

func (s *Service) recordPromotionEvent(ctx context.Context, agentRec domain.Agent, created domain.Skill) {
	if s.store == nil {
		return
	}
	scoreAt := 100.0
	if s.perf != nil {
		if score, err := s.perf.GetScore(ctx, agentRec.ID); err == nil {
			scoreAt = score.Score
		}
	}
	if _, err := s.store.CreateEvent(ctx, domain.AgentEvolutionEvent{
		AgentID: agentRec.ID, ChangeType: domain.EvolutionChangeSkillCreated,
		TargetKind: domain.EvolutionTargetSkill, TargetID: &created.ID, TargetName: created.Name,
		After: snapshotSkill(created, nil), ScoreAtChange: scoreAt,
		Impact: domain.EvolutionImpactNeutral,
	}); err != nil {
		log.Warn().Err(err).Str("skill", created.Name).Msg("memory promotion: event persist failed")
	}
}

// promotionModel routes the classification calls: judge model first (this is
// judging work), then the evolution override, then the configured default.
//
// The default is expressed as the ZERO value of both — an empty provider
// routes at whatever is configured, and an empty model lets that
// provider use its own configured model. It used to be agents[0]'s model and
// provider instead, which was provider roulette independent of any one
// provider's quirks: the classification is the evolution engine's own work, not
// the work of whichever agent happened to sort first in the list handed in, and
// borrowing that agent's engine made an unrelated catalog edit change where
// these calls were billed. It also happened to be the single worst place to
// pick up a claude_code agent, since these are JSON-schema calls a CLI cannot
// produce at all.
func (s *Service) promotionModel() (string, domain.LLMProviderType) {
	var model string
	var provider domain.LLMProviderType
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}
	if s.cfg.JudgeModel != "" {
		model = s.cfg.JudgeModel
	}
	if s.cfg.JudgeProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.JudgeProviderType)
	}
	return model, provider
}

// promotionChat is one structured-output call with a single JSON
// self-correction retry, mirroring runLLM.
func (s *Service) promotionChat(ctx context.Context, system, user, model string, provider domain.LLMProviderType, schemaName string, schema map[string]interface{}, out any) error {
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: system},
		{Role: domain.RoleUser, Content: user},
	}
	call := func(msgs []domain.Message) (string, error) {
		resp, err := s.llm.Chat(ctx, domain.AgentRequest{
			Messages: msgs, Model: model, ProviderType: provider,
			ResponseFormat: domain.JSONSchemaResponseFormat(schemaName, schema),
		})
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}
	raw, err := call(messages)
	if err != nil {
		return err
	}
	parseErr := parsePromotionJSON(raw, out)
	if parseErr == nil {
		return nil
	}
	retry := append(messages,
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: "Your previous output was not valid JSON (" + parseErr.Error() + "). Respond again with ONLY the JSON object, no prose, no code fences."},
	)
	raw2, err := call(retry)
	if err != nil {
		return err
	}
	return parsePromotionJSON(raw2, out)
}

func parsePromotionJSON(raw string, out any) error {
	cleaned := strings.TrimSpace(raw)
	if strings.Contains(cleaned, "```") {
		cleaned = strings.ReplaceAll(cleaned, "```json", "")
		cleaned = strings.ReplaceAll(cleaned, "```", "")
	}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal([]byte(cleaned[start:end+1]), out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

type promotionOutput struct {
	Promotions []promotionItem `json:"promotions"`
}

type promotionItem struct {
	MemoryID    string   `json:"memory_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	Agents      []string `json:"agents"`
}

type saveClassification struct {
	Skill       bool   `json:"skill"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Content     string `json:"content"`
}

const teamPromotionSystemPrompt = `You curate a software team's shared memory. Some entries are not memories at all — they are reusable know-how that belongs in the skill catalog, where agents load it while working.

Promote an entry ONLY when it teaches a durable, reusable method: a how-to, technique, checklist, convention, or workflow that will keep paying off in future tasks. When promoting, rewrite the content as concise instructional markdown an agent can follow.

Do NOT promote: one-off facts, project status, decisions or events, user/stakeholder preferences, credentials or URLs, and repository-specific trivia that teaches no transferable method. Those stay memories. An empty promotions array is a valid and often correct answer.

For each promotion set agents to the roster names the skill is relevant for; use an empty array when it fits the whole team.

Respond with a single JSON object matching the provided schema.`

func teamPromotionUserMessage(memories []domain.AgentMemory, agents []domain.Agent) string {
	var b strings.Builder
	b.WriteString("## Agent roster\n")
	for _, a := range agents {
		b.WriteString(fmt.Sprintf("- %s: %s\n", a.Name, truncate(a.Description, 160)))
	}
	b.WriteString("\n## Team memories\n")
	for _, m := range memories {
		scope := "global"
		if m.RepositoryID != nil {
			scope = "repository-specific"
		}
		category := m.Category
		if category == "" {
			category = "-"
		}
		b.WriteString(fmt.Sprintf("- id: %s | category: %s | scope: %s\n  %s\n", m.ID, category, scope, strings.ReplaceAll(m.Content, "\n", "\n  ")))
	}
	return b.String()
}

const saveClassifierSystemPrompt = `An agent on a software team is about to save a note to its long-term memory. Decide whether the note is actually reusable know-how that belongs in the skill catalog instead.

Set skill=true ONLY when the note teaches a durable, reusable method: a how-to, technique, checklist, convention, or workflow worth loading in future tasks. Then rewrite it as concise instructional markdown in content and give it a short kebab-case name and a one-line description.

Set skill=false for everything else: one-off facts, status, events, preferences, credentials, and notes tied to a single repository's current state. When in doubt, skill=false — a wrong memory is cheap, a wrong skill pollutes the catalog. Fill unused fields with empty strings.

Respond with a single JSON object matching the provided schema.`

// Both schemas require every property and forbid unknown keys — OpenAI's
// strict json_schema mode rejects optional properties (see
// reflectionOutputSchema for the long version of this story).
func promotionOutputSchema() map[string]interface{} {
	promotion := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"memory_id":   map[string]interface{}{"type": "string"},
			"name":        map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"category":    map[string]interface{}{"type": "string"},
			"content":     map[string]interface{}{"type": "string"},
			"agents":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"memory_id", "name", "description", "category", "content", "agents"},
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"promotions": map[string]interface{}{"type": "array", "items": promotion},
		},
		"required": []string{"promotions"},
	}
}

func saveClassificationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"skill":       map[string]interface{}{"type": "boolean"},
			"name":        map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"category":    map[string]interface{}{"type": "string"},
			"content":     map[string]interface{}{"type": "string"},
		},
		"required": []string{"skill", "name", "description", "category", "content"},
	}
}
