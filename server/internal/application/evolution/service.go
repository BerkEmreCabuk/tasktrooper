package evolution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type SkillRuleManager interface {
	CreateSkillForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error)
	UpdateSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID, req domain.UpdateSkillRequest) (domain.Skill, error)
	DeleteSkillForAgent(ctx context.Context, agentID, skillID uuid.UUID) error
	CreateRuleForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateOrchestratorRuleRequest) (domain.OrchestratorRule, error)
	UpdateRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID, req domain.UpdateOrchestratorRuleRequest) (domain.OrchestratorRule, error)
	DeleteRuleForAgent(ctx context.Context, agentID, ruleID uuid.UUID) error
}

type Deps struct {
	Store     port.AgentEvolutionStore
	Catalog   port.CatalogStore
	Manager   SkillRuleManager
	Memories  *memory.Service
	KPIs      *kpi.Service
	Perf      port.AgentPerformanceStore
	Sessions  port.SessionStore
	Runs      port.TaskAgentRunStore
	Comments  port.TaskCommentStore
	Board     port.BoardConfigStore
	Golden    port.GoldenTaskStore
	LLM       port.LLMClient
	AgentLoop agent.Runner
	Config    domain.EvolutionConfig
}

type Service struct {
	store     port.AgentEvolutionStore
	catalog   port.CatalogStore
	manager   SkillRuleManager
	memories  *memory.Service
	kpis      *kpi.Service
	perf      port.AgentPerformanceStore
	sessions  port.SessionStore
	runs      port.TaskAgentRunStore
	comments  port.TaskCommentStore
	board     port.BoardConfigStore
	golden    port.GoldenTaskStore
	llm       port.LLMClient
	agentLoop agent.Runner
	cfg       domain.EvolutionConfig

	mu       sync.Mutex
	inflight map[string]bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewService(deps Deps) *Service {
	return &Service{
		store:     deps.Store,
		catalog:   deps.Catalog,
		manager:   deps.Manager,
		memories:  deps.Memories,
		kpis:      deps.KPIs,
		perf:      deps.Perf,
		sessions:  deps.Sessions,
		runs:      deps.Runs,
		comments:  deps.Comments,
		board:     deps.Board,
		golden:    deps.Golden,
		llm:       deps.LLM,
		agentLoop: deps.AgentLoop,
		cfg:       deps.Config,
		inflight:  make(map[string]bool),
	}
}

func (s *Service) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		log.Info().Msg("evolution service disabled")
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.cfg.TickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.tick(runCtx)
			}
		}
	}()
	log.Info().Dur("tick", s.cfg.TickInterval).Dur("reflect_interval", s.cfg.ReflectInterval).Msg("evolution service started")
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Service) tick(ctx context.Context) {
	// A background maintenance loop must never take the process down with it.
	defer func() {
		if r := recover(); r != nil {
			log.Error().Any("panic", r).Msg("evolution tick panicked")
		}
	}()
	if s.board == nil {
		return
	}
	s.evaluateImpacts(ctx)
	members, err := s.board.ListMembers(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("evolution tick: list board members failed")
		return
	}
	now := time.Now()
	for _, m := range members {
		agentRec, err := s.catalog.GetAgent(ctx, m.AgentID)
		if err != nil || !agentRec.Enabled {
			continue
		}
		if s.kpis != nil {
			if _, err := s.kpis.EvaluateAgent(ctx, m.AgentID, now); err != nil {
				log.Warn().Err(err).Str("agent", agentRec.Name).Msg("kpi sweep failed")
			}
		}
		if s.reflectionDue(ctx, agentRec, now) {
			if _, err := s.StartReflection(ctx, m.AgentID, domain.ReflectionTriggerPeriodic); err != nil {
				log.Debug().Err(err).Str("agent", agentRec.Name).Msg("periodic reflection not started")
			}
		}
	}
}

func (s *Service) reflectionDue(ctx context.Context, agentRec domain.Agent, now time.Time) bool {
	last, err := s.store.LatestReflectionByTrigger(ctx, agentRec.ID, domain.ReflectionTriggerPeriodic)
	if err != nil {
		return false
	}
	windowStart := now.Add(-s.cfg.ReflectInterval)
	if last != nil {
		if now.Sub(last.CreatedAt) < s.cfg.ReflectInterval {
			return false
		}
		windowStart = last.WindowEnd
	}
	if events, err := s.perf.EventsInWindow(ctx, agentRec.ID, windowStart, now); err == nil && len(events) > 0 {
		return true
	}
	if runs, err := s.runs.ListRecent(ctx, 100); err == nil {
		for _, r := range runs {
			if r.AgentID == agentRec.ID && r.CreatedAt.After(windowStart) {
				return true
			}
		}
	}
	if sessions, err := s.sessions.ListByAgent(ctx, agentRec.ID, 5, 0); err == nil {
		for _, sess := range sessions {
			if sess.UpdatedAt.After(windowStart) {
				return true
			}
		}
	}
	return false
}

// The reflection outlives the board move that triggered it, so the request's cancellation is stripped (context.WithoutCancel) and replaced with its own timeout.
func (s *Service) NotifyRevision(ctx context.Context, task domain.BoardTask) {
	if !s.cfg.Enabled || task.AssigneeAgentID == nil {
		return
	}
	agentID := *task.AssigneeAgentID
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		last, err := s.store.LatestReflectionByTrigger(ctx, agentID, domain.ReflectionTriggerRevision)
		if err == nil && last != nil && time.Since(last.CreatedAt) < s.cfg.RevisionDebounce {
			return
		}
		if _, err := s.StartReflection(ctx, agentID, domain.ReflectionTriggerRevision); err != nil {
			log.Debug().Err(err).Msg("revision reflection not started")
		}
	}()
}

func (s *Service) ReflectNow(ctx context.Context, agentID uuid.UUID) (domain.AgentReflection, error) {
	return s.StartReflection(ctx, agentID, domain.ReflectionTriggerManual)
}

var ErrReflectionInFlight = fmt.Errorf("a reflection is already running for this agent")

func (s *Service) StartReflection(ctx context.Context, agentID uuid.UUID, trigger string) (domain.AgentReflection, error) {
	key := agentID.String()
	s.mu.Lock()
	if s.inflight[key] {
		s.mu.Unlock()
		return domain.AgentReflection{}, ErrReflectionInFlight
	}
	s.inflight[key] = true
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
	}

	now := time.Now()
	windowStart := now.Add(-7 * 24 * time.Hour)
	if last, err := s.store.LatestCompletedReflection(ctx, agentID); err == nil && last != nil && last.WindowEnd.After(windowStart) {
		windowStart = last.WindowEnd
	}
	reflection, err := s.store.CreateReflection(ctx, domain.AgentReflection{
		AgentID: agentID, Trigger: trigger,
		Status: domain.ReflectionStatusRunning, WindowStart: windowStart, WindowEnd: now,
	})
	if err != nil {
		release()
		return domain.AgentReflection{}, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()
		procCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		defer cancel()
		s.processReflection(procCtx, reflection)
	}()
	return reflection, nil
}

func (s *Service) ListReflections(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentReflection, error) {
	if limit <= 0 {
		limit = 20
	}
	reflections, err := s.store.ListReflections(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	deriveLegacyDecisions(reflections)
	return reflections, nil
}

// Completed reflections written before Decision held only RawOutput, which the fixed parser now reads — nothing is written back, and Legacy tags the derivation so the UI can tell it from the actual apply record. The baseline backfill below relies on ListReflections being created_at DESC: an older completed snapshot for row i sits at index > i.
func deriveLegacyDecisions(reflections []domain.AgentReflection) {
	for i := range reflections {
		r := &reflections[i]
		if r.Status != domain.ReflectionStatusCompleted {
			continue
		}
		if r.Decision == nil && r.RawOutput != "" {
			if output, analysis, err := parseReflectionOutput(r.RawOutput); err == nil {
				r.Decision = &domain.ReflectionDecision{
					Legacy:         true,
					Analysis:       analysis,
					SelfAssessment: strings.TrimSpace(output.SelfAssessment),
					Changes:        legacyChangeOutcomes(output),
				}
			}
		}
		if r.Decision == nil {
			continue
		}
		if r.Summary == "" {
			switch {
			case r.Decision.SelfAssessment != "":
				r.Summary = r.Decision.SelfAssessment
			case r.Decision.Analysis != "":
				r.Summary = truncate(r.Decision.Analysis, 600)
			}
		}
	}
	for i := range reflections {
		r := &reflections[i]
		if r.Decision == nil || r.Decision.Baseline != nil {
			continue
		}
		for j := i + 1; j < len(reflections); j++ {
			older := reflections[j]
			if older.Status == domain.ReflectionStatusCompleted && older.PerformanceSnapshot != nil {
				r.Decision.Baseline = older.PerformanceSnapshot
				break
			}
		}
	}
}

// A legacy row never got to apply, so every change lands as not_applied.
func legacyChangeOutcomes(output domain.ReflectionOutput) []domain.ReflectionChangeOutcome {
	var out []domain.ReflectionChangeOutcome
	for _, ch := range output.Skills {
		out = append(out, domain.ReflectionChangeOutcome{
			Kind: "skill", Action: ch.Action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
			Outcome: domain.ReflectionOutcomeNotApplied,
		})
	}
	for _, ch := range output.Rules {
		out = append(out, domain.ReflectionChangeOutcome{
			Kind: "rule", Action: ch.Action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
			Outcome: domain.ReflectionOutcomeNotApplied,
		})
	}
	for _, ch := range output.Memories {
		out = append(out, domain.ReflectionChangeOutcome{
			Kind: "memory", Action: ch.Action, Name: truncate(ch.Content, 60), TargetID: ch.MemoryID, Reason: ch.Reason,
			Outcome: domain.ReflectionOutcomeNotApplied,
		})
	}
	for _, rv := range output.Reverts {
		out = append(out, domain.ReflectionChangeOutcome{
			Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
			Outcome: domain.ReflectionOutcomeNotApplied,
		})
	}
	return out
}

func (s *Service) ListEvents(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentEvolutionEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListEvents(ctx, agentID, limit)
}
