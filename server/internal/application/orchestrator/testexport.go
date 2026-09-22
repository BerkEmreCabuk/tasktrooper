package orchestrator

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func ParsePlannerOutputForTest(content string) (domain.PlannerOutput, error) {
	return parsePlannerOutput(content)
}

func ParseRepairPlanOutputForTest(content string) (domain.PlannerOutput, error) {
	return parseRepairPlanOutput(content)
}

func ValidatePlannerOutputForTest(output domain.PlannerOutput, agents []domain.Agent, skillOwnership map[string]map[string]bool, maxTasks int) error {
	return validatePlannerOutput(output, agents, skillOwnership, maxTasks, nil)
}

// Checks a repair plan the way the replanner does, against the run's existing subtasks.
func ValidateRepairPlanForTest(output domain.PlannerOutput, agents []domain.Agent, skillOwnership map[string]map[string]bool, maxTasks int, priorTasks []domain.PlannerTask) error {
	return validatePlannerOutput(output, agents, skillOwnership, maxTasks, priorTasks)
}

func TaskContextForTest(pt domain.PlannerTask) taskContext {
	return taskContext{
		intake:      domain.GoalIntake{Purpose: "test purpose", Goal: "test goal"},
		plannerTask: pt,
	}
}

func BuildTaskMessagesForTest(
	history []domain.Message,
	tc taskContext,
	skills []domain.Skill,
	stacks []domain.TechStack,
	agentRec domain.Agent,
	priorResults map[string]string,
	mu *sync.Mutex,
	lastErr error,
	attempt int,
	subtaskWorkspace, lang string,
	cfg domain.OrchestrationConfig,
) []domain.Message {
	if cfg.SubtaskHistoryMode == "" {
		cfg.SubtaskHistoryMode = domain.SubtaskHistoryModeIsolated
	}
	if cfg.DependencyOutputMaxChars <= 0 {
		cfg.DependencyOutputMaxChars = 4000
	}
	e := &Executor{cfg: cfg}
	return e.buildTaskMessages(context.Background(), uuid.Nil, history, tc, skills, stacks, agentRec, priorResults, mu, priorAttempt{Number: attempt, Err: lastErr}, subtaskWorkspace, lang)
}

// Resolves a subtask's skill index the way runTask does.
func EnabledAgentSkillsForTest(ctx context.Context, catalog port.CatalogStore, agentID uuid.UUID) ([]domain.Skill, error) {
	e := &Executor{catalog: catalog}
	return e.enabledAgentSkills(ctx, agentID)
}

func ResolveSubtaskWorkspaceForTest(sessionWorkspace, taskKey string) (string, error) {
	return resolveSubtaskWorkspace(sessionWorkspace, taskKey)
}

func PlannedSkillFocusForTest(skillIDs []string, skills []domain.Skill) string {
	return plannedSkillFocus(skillIDs, skills)
}

func IsolatedSubtaskHistoryForTest(history []domain.Message) []domain.Message {
	return isolatedSubtaskHistory(history)
}

func StartedNotFinishedReasonForTest(delta map[string]int, effectiveTools []string) string {
	return startedNotFinishedReason(delta, effectiveTools)
}

func BoardWriteNotLandedReasonForTest(delta map[string]int, effectiveTools []string) string {
	return boardWriteNotLandedReason(delta, effectiveTools)
}

func PipelineConversationHistoryForTest(history []domain.Message) []domain.Message {
	return pipelineConversationHistory(history)
}

func AppendPipelineUserMessageForTest(messages []domain.Message, userMessage string) []domain.Message {
	return appendPipelineUserMessage(messages, userMessage)
}

func TruncateDependencyOutputForTest(content string, maxChars int) string {
	return truncateDependencyOutput(content, maxChars)
}

// Records a subtask that stopped on a clarification the way runTask does.
func MarkTaskBlockedForTest(ctx context.Context, catalog port.CatalogStore, planTask domain.PlanTask, req domain.ClarificationRequest, partial string) {
	e := &Executor{catalog: catalog}
	e.markTaskBlocked(ctx, planTask, req, partial)
}

func BuildReplannerSystemPromptForTest() string {
	return buildReplannerSystemPrompt(nil, nil)
}

func BuildPlannerPMSoloPromptForTest(lang string) string {
	pmID := uuid.New()
	intake := domain.GoalIntake{Ready: true, Purpose: "test purpose", Goal: "test goal", Constraints: []string{}, Questions: []domain.ClarificationQuestion{}}
	catalogs := []agentCatalogEntry{{
		Agent: domain.Agent{ID: pmID, Name: "product-manager", Description: "PM", SubagentType: "generalPurpose"},
		Skills: []domain.Skill{{
			ID: uuid.New(), AgentID: pmID, Name: "task-decomposition", Enabled: true,
			Description: "Break the request into board tasks",
		}},
	}}
	return buildPlannerSystemPrompt(intake, catalogs, nil, PlannerOptions{ConstrainedAgentID: &pmID, Lang: lang})
}
