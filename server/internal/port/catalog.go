package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CatalogStore interface {
	CreateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error)
	GetSkill(ctx context.Context, id uuid.UUID) (domain.Skill, error)
	ListSkills(ctx context.Context) ([]domain.Skill, error)
	ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error)
	UpdateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error)
	DeleteSkill(ctx context.Context, id uuid.UUID) error
	SearchSkills(ctx context.Context, queryEmbedding []float32, topK int, agentID *uuid.UUID) ([]domain.Skill, error)

	CreateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error)
	GetTechStack(ctx context.Context, id uuid.UUID) (domain.TechStack, error)
	ListTechStacksByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error)
	UpdateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error)
	DeleteTechStack(ctx context.Context, id uuid.UUID) error

	CreateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error)
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	UpdateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error)
	DeleteAgent(ctx context.Context, id uuid.UUID) error

	CreateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error)
	GetRule(ctx context.Context, id uuid.UUID) (domain.OrchestratorRule, error)
	ListRules(ctx context.Context) ([]domain.OrchestratorRule, error)
	ListRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error)
	UpdateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error)
	DeleteRule(ctx context.Context, id uuid.UUID) error
	ListEnabledRules(ctx context.Context) ([]domain.OrchestratorRule, error)
	ListEnabledRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error)

	CreatePlan(ctx context.Context, plan domain.OrchestrationPlan, tasks []domain.PlanTask) (domain.OrchestrationPlan, error)
	GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error)
	UpdatePlanStatus(ctx context.Context, planID uuid.UUID, status string) error
	UpdatePlanJSON(ctx context.Context, planID uuid.UUID, planJSON []byte) error
	AppendPlanTasks(ctx context.Context, planID uuid.UUID, tasks []domain.PlanTask) ([]domain.PlanTask, error)
	UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error
	ListPlanTasks(ctx context.Context, planID uuid.UUID) ([]domain.PlanTask, error)
}
