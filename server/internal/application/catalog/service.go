package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store          port.CatalogStore
	llm            port.LLMClient
	embeddingModel string
	templates      port.AgentTemplateStore
	kpis           port.AgentKPIStore
	boardConfig    port.BoardConfigStore
	versions       port.CatalogVersionStore
	seeding        atomic.Bool
	// hostExecutor answers "can a run on this provider actually be executed
	// here". Nil means no runner is attached, which is the correct answer on
	// every host that has no agent CLI installed — including the cloud pod.
	hostExecutor HostExecutorProbe
}

// HostExecutorProbe reports whether this host has a runner attached for a
// provider that is a local process rather than an HTTP endpoint.
type HostExecutorProbe func(domain.LLMProviderType) bool

// ErrNoHostRunner marks the save-time refusal as the caller's to fix rather
// than a server fault, so the transport can answer 400 with the sentence
// instead of 500 with it.
var ErrNoHostRunner = errors.New("no host runner for this provider")

// ErrInvalidInput marks a refusal caused by the REQUEST BODY rather than by
// anything on this server: a missing name, a skill with no content, an effort
// level that is not one of the five, a negative turn cap.
//
// It exists because every one of them left through the generic error path as a
// 500 — `PUT /admin/agents/:id` with no name answered `500 "name is required"`
// — which tells a client and a monitor that the server broke and the same body
// is worth sending again. Nothing here is retriable and nothing here is ours to
// fix; the only thing that resolves it is a different request.
var ErrInvalidInput = errors.New("invalid input")

// invalidInputError carries the sentence WITHOUT the sentinel's own text in
// front of it. These messages are already the whole explanation and are
// rendered verbatim in the UI, so the usual `fmt.Errorf("%w: …")` wrap would
// only make a reader step past a label to reach the sentence.
type invalidInputError struct{ msg string }

func (e invalidInputError) Error() string        { return e.msg }
func (e invalidInputError) Is(target error) bool { return target == ErrInvalidInput }

func invalidInput(format string, a ...any) error {
	return invalidInputError{msg: fmt.Sprintf(format, a...)}
}

// SeedingInProgress reports whether EnsureRoleTemplates is still upserting the
// built-in agent templates. Agents themselves are no longer created at boot —
// only the user creates one, from the template gallery — so this is brief and
// touches no skill-embedding calls; it is kept, rather than hardcoded false,
// so the /admin/agents handler's existing "seeding" response field stays
// truthful instead of becoming a constant the frontend's poll can no longer
// learn anything from.
func (s *Service) SeedingInProgress() bool {
	return s.seeding.Load()
}

func NewService(store port.CatalogStore, llm port.LLMClient, embeddingModel string) *Service {
	return &Service{store: store, llm: llm, embeddingModel: embeddingModel}
}

func (s *Service) SetTemplateStore(store port.AgentTemplateStore) {
	s.templates = store
}

func (s *Service) SetKPIStore(store port.AgentKPIStore) {
	s.kpis = store
}

func (s *Service) SetBoardConfigStore(store port.BoardConfigStore) {
	s.boardConfig = store
}

// SetHostExecutorProbe wires the check that decides whether an agent may be
// saved onto a host-executed provider. Set after construction, because whether
// a runner exists is discovered at boot by probing the host rather than read
// from configuration.
func (s *Service) SetHostExecutorProbe(p HostExecutorProbe) {
	s.hostExecutor = p
}

// checkHostExecutor refuses to persist an agent whose engine does not exist
// here.
//
// The provider is only half of a working configuration: the other half is a
// runner process on the machine this server runs on, and nothing about the
// agent record says whether one is attached. Saved without it, the same
// misconfiguration surfaced as about ten different runtime failures — a board
// run that fails with a sentence about a missing binary, a chat turn that
// refuses, a verify-fix round that silently never happens, a golden suite that
// evaluates zero tasks — each one describing its own symptom and none of them
// pointing at the choice that caused it. Refusing the save is one sentence, at
// the moment the choice is made, addressed to the person making it.
func (s *Service) checkHostExecutor(providerType domain.LLMProviderType) error {
	if !domain.RequiresHostExecutor(providerType) {
		return nil
	}
	if s.hostExecutor != nil && s.hostExecutor(providerType) {
		return nil
	}
	label := string(providerType)
	if def, ok := domain.LLMProviderDefinitionFor(providerType); ok && def.Label != "" {
		label = def.Label
	}
	return fmt.Errorf("%w: this agent cannot run on %s here — this workspace has no runner attached for it; "+
		"start a local runner on a machine with that CLI installed, or pick another provider for this agent",
		ErrNoHostRunner, label)
}

// checkProviderAvailable refuses to persist an agent onto a provider that is
// declared but has no executor behind it.
//
// This is the guard the "coming soon" badge in the UI is a courtesy for, and it
// is on the server because the badge cannot be. A client that predates the
// availability flag renders the provider like any other; the mobile app and the
// API are reachable without any client at all. Any one of those could save an
// agent on cursor_agent, and the agent would look completely healthy — a row in
// the catalog, a provider the board accepts — right up to the point where a
// task assigned to it finds no executor and the run fails somewhere far from
// this choice. The refusal belongs where the choice is made.
//
// It runs BEFORE checkHostExecutor because the two failures are different and
// only one of them is fixable: "no runner attached" is answered by attaching a
// runner, and telling somebody to go do that for a provider whose executor was
// never written would send them off to fix the wrong thing.
//
// A provider ref that is not a declared type — the empty string, or an
// endpoint uuid — is not this guard's business and passes through.
func (s *Service) checkProviderAvailable(providerType domain.LLMProviderType) error {
	if providerType == "" || !domain.ValidLLMProviderType(string(providerType)) {
		return nil
	}
	if domain.ProviderAvailable(providerType) {
		return nil
	}
	return domain.ErrUnavailableProvider(providerType)
}

func (s *Service) CreateSkillForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error) {
	if req.Name == "" {
		return domain.Skill{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.Skill{}, invalidInput("content is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.Skill{}, fmt.Errorf("agent not found: %w", err)
	}
	stackID, err := s.resolveTechStack(ctx, agentID, req.TechStackID)
	if err != nil {
		return domain.Skill{}, err
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	emb, err := s.llm.Embed(ctx, req.Name+"\n"+req.Description+"\n"+req.Content, s.embeddingModel)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("embed skill: %w", err)
	}
	created, err := s.store.CreateSkill(ctx, domain.Skill{
		AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Embedding: emb, Enabled: req.Enabled, TechStackID: stackID,
	})
	if err != nil {
		return domain.Skill{}, err
	}
	s.recordSkillVersion(ctx, domain.CatalogVersionActionCreate, created)
	return created, nil
}

func (s *Service) GetSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID) (domain.Skill, error) {
	skill, err := s.store.GetSkill(ctx, skillID)
	if err != nil {
		return domain.Skill{}, err
	}
	if skill.AgentID != agentID {
		return domain.Skill{}, fmt.Errorf("skill not found for agent")
	}
	return skill, nil
}

func (s *Service) ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListSkillsByAgent(ctx, agentID)
}

func (s *Service) UpdateSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID, req domain.UpdateSkillRequest) (domain.Skill, error) {
	if req.Name == "" {
		return domain.Skill{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.Skill{}, invalidInput("content is required")
	}
	if _, err := s.GetSkillForAgent(ctx, agentID, skillID); err != nil {
		return domain.Skill{}, err
	}
	stackID, err := s.resolveTechStack(ctx, agentID, req.TechStackID)
	if err != nil {
		return domain.Skill{}, err
	}
	emb, err := s.llm.Embed(ctx, req.Name+"\n"+req.Description+"\n"+req.Content, s.embeddingModel)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("embed skill: %w", err)
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	updated, err := s.store.UpdateSkill(ctx, domain.Skill{
		ID: skillID, AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Embedding: emb, Enabled: req.Enabled, TechStackID: stackID,
	})
	if err != nil {
		return domain.Skill{}, err
	}
	s.recordSkillVersion(ctx, updateAction(ctx), updated)
	return updated, nil
}

func (s *Service) DeleteSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID) error {
	existing, err := s.GetSkillForAgent(ctx, agentID, skillID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSkill(ctx, skillID); err != nil {
		return err
	}
	// The deleted content is the last thing written to history: without it the
	// skill could never be brought back.
	s.recordSkillVersion(ctx, domain.CatalogVersionActionDelete, existing)
	return nil
}

func (s *Service) SearchSkills(ctx context.Context, query string, topK int) ([]domain.Skill, error) {
	emb, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return s.store.SearchSkills(ctx, emb, topK, nil)
}

// resolveTechStack refuses a skill filed under a stack that is not this
// agent's. The foreign key cannot catch it: any real stack id is valid to
// Postgres regardless of which agent it belongs to, so the reference is valid
// to Postgres and wrong to everybody else — the skill would show up under an
// agent that has no such stack to render it.
// The nil uuid is read as "general", so a client that sends an empty id gets
// the same answer as one that sends none.
func (s *Service) resolveTechStack(ctx context.Context, agentID uuid.UUID, stackID *uuid.UUID) (*uuid.UUID, error) {
	if stackID == nil || *stackID == uuid.Nil {
		return nil, nil
	}
	stack, err := s.store.GetTechStack(ctx, *stackID)
	if err != nil {
		return nil, invalidInput("tech stack not found")
	}
	if stack.AgentID != agentID {
		return nil, invalidInput("tech stack belongs to another agent")
	}
	return stackID, nil
}

func (s *Service) CreateTechStackForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateTechStackRequest) (domain.TechStack, error) {
	if req.Name == "" {
		return domain.TechStack{}, invalidInput("name is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.TechStack{}, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.CreateTechStack(ctx, domain.TechStack{
		AgentID: agentID, Name: req.Name, Description: req.Description, Position: req.Position,
	})
}

func (s *Service) GetTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID) (domain.TechStack, error) {
	stack, err := s.store.GetTechStack(ctx, stackID)
	if err != nil {
		return domain.TechStack{}, err
	}
	if stack.AgentID != agentID {
		return domain.TechStack{}, fmt.Errorf("tech stack not found for agent")
	}
	return stack, nil
}

func (s *Service) ListTechStacksForAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListTechStacksByAgent(ctx, agentID)
}

func (s *Service) UpdateTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID, req domain.UpdateTechStackRequest) (domain.TechStack, error) {
	existing, err := s.GetTechStackForAgent(ctx, agentID, stackID)
	if err != nil {
		return domain.TechStack{}, err
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Position != nil {
		existing.Position = *req.Position
	}
	if existing.Name == "" {
		return domain.TechStack{}, invalidInput("name is required")
	}
	return s.store.UpdateTechStack(ctx, existing)
}

// DeleteTechStackForAgent leaves the stack's skills in place: the foreign key
// nulls their tech_stack_id, which files them back as general skills. Deleting
// a way of organising skills is not deleting the skills.
func (s *Service) DeleteTechStackForAgent(ctx context.Context, agentID, stackID uuid.UUID) error {
	if _, err := s.GetTechStackForAgent(ctx, agentID, stackID); err != nil {
		return err
	}
	return s.store.DeleteTechStack(ctx, stackID)
}

func (s *Service) CreateAgent(ctx context.Context, req domain.CreateAgentRequest) (domain.Agent, error) {
	if req.Name == "" {
		return domain.Agent{}, invalidInput("name is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "generalPurpose"
	}
	if !domain.ValidEffort(req.Effort) {
		return domain.Agent{}, invalidInput("effort must be one of %v, or empty for the executor's default", domain.EffortLevels)
	}
	if req.MaxTurns < 0 {
		return domain.Agent{}, invalidInput("max_turns cannot be negative; use 0 for the executor's default")
	}
	if err := s.checkProviderAvailable(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	if err := s.checkHostExecutor(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	return s.store.CreateAgent(ctx, domain.Agent{
		Name: req.Name, Description: req.Description, SubagentType: req.SubagentType,
		SystemPrompt: req.SystemPrompt, ProviderType: req.ProviderType, Model: req.Model, ModelHeavy: req.ModelHeavy, MaxTurns: req.MaxTurns, Effort: req.Effort, ToolPolicy: req.ToolPolicy, Enabled: req.Enabled,
		SelfEvolutionEnabled: req.SelfEvolutionEnabled,
	})
}

func (s *Service) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	return s.store.GetAgent(ctx, id)
}

func (s *Service) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	return s.store.ListAgents(ctx)
}

func (s *Service) UpdateAgent(ctx context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error) {
	if req.Name == "" {
		return domain.Agent{}, invalidInput("name is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "generalPurpose"
	}
	if !domain.ValidEffort(req.Effort) {
		return domain.Agent{}, invalidInput("effort must be one of %v, or empty for the executor's default", domain.EffortLevels)
	}
	if req.MaxTurns < 0 {
		return domain.Agent{}, invalidInput("max_turns cannot be negative; use 0 for the executor's default")
	}
	if err := s.checkProviderAvailable(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	if err := s.checkHostExecutor(req.ProviderType); err != nil {
		return domain.Agent{}, err
	}
	req = dropStaleModels(ctx, s.store, id, req)
	return s.store.UpdateAgent(ctx, domain.Agent{
		ID: id, Name: req.Name, Description: req.Description, SubagentType: req.SubagentType,
		SystemPrompt: req.SystemPrompt, ProviderType: req.ProviderType, Model: req.Model, ModelHeavy: req.ModelHeavy, MaxTurns: req.MaxTurns, Effort: req.Effort, ToolPolicy: req.ToolPolicy, Enabled: req.Enabled,
		SelfEvolutionEnabled: req.SelfEvolutionEnabled,
	})
}

// dropStaleModels clears model names that belong to the provider the agent is
// being moved off. A model name only means something to the provider it was
// picked from, but the agent record keeps one provider and two free-form model
// names, so a provider switch that carried the old names over produced an agent
// whose provider was one vendor and whose (heavy) model was another's — every
// run on it died with the provider's "invalid model" 400. A name the caller
// actually changed is left alone: that one was picked for the new provider.
func dropStaleModels(ctx context.Context, store port.CatalogStore, id uuid.UUID, req domain.UpdateAgentRequest) domain.UpdateAgentRequest {
	existing, err := store.GetAgent(ctx, id)
	if err != nil || existing.ProviderType == "" || req.ProviderType == "" || existing.ProviderType == req.ProviderType {
		return req
	}
	if req.Model == existing.Model {
		req.Model = ""
	}
	if req.ModelHeavy == existing.ModelHeavy {
		req.ModelHeavy = ""
	}
	return req
}

func (s *Service) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteAgent(ctx, id)
}

func (s *Service) CreateRuleForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	if req.Name == "" {
		return domain.OrchestratorRule{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.OrchestratorRule{}, invalidInput("content is required")
	}
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return domain.OrchestratorRule{}, fmt.Errorf("agent not found: %w", err)
	}
	created, err := s.store.CreateRule(ctx, domain.OrchestratorRule{
		AgentID: agentID, Name: req.Name, Content: req.Content, Priority: req.Priority, Enabled: req.Enabled,
	})
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	s.recordRuleVersion(ctx, domain.CatalogVersionActionCreate, created)
	return created, nil
}

func (s *Service) GetRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID) (domain.OrchestratorRule, error) {
	rule, err := s.store.GetRule(ctx, ruleID)
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	if rule.AgentID != agentID {
		return domain.OrchestratorRule{}, fmt.Errorf("rule not found for agent")
	}
	return rule, nil
}

func (s *Service) ListRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return s.store.ListRulesByAgent(ctx, agentID)
}

func (s *Service) UpdateRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID, req domain.UpdateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	if req.Name == "" {
		return domain.OrchestratorRule{}, invalidInput("name is required")
	}
	if req.Content == "" {
		return domain.OrchestratorRule{}, invalidInput("content is required")
	}
	if _, err := s.GetRuleForAgent(ctx, agentID, ruleID); err != nil {
		return domain.OrchestratorRule{}, err
	}
	updated, err := s.store.UpdateRule(ctx, domain.OrchestratorRule{
		ID: ruleID, AgentID: agentID, Name: req.Name, Content: req.Content, Priority: req.Priority, Enabled: req.Enabled,
	})
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	s.recordRuleVersion(ctx, updateAction(ctx), updated)
	return updated, nil
}

func (s *Service) DeleteRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID) error {
	existing, err := s.GetRuleForAgent(ctx, agentID, ruleID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRule(ctx, ruleID); err != nil {
		return err
	}
	s.recordRuleVersion(ctx, domain.CatalogVersionActionDelete, existing)
	return nil
}

func (s *Service) GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error) {
	return s.store.GetPlanByRunID(ctx, runID)
}
