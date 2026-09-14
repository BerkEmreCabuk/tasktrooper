package orchestrator

import (
	"context"
	"fmt"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type Replanner struct {
	llm      port.LLMClient
	catalog  port.CatalogStore
	maxTasks int
}

func NewReplanner(llm port.LLMClient, catalog port.CatalogStore, maxTasks int) *Replanner {
	if maxTasks <= 0 {
		maxTasks = 10
	}
	return &Replanner{llm: llm, catalog: catalog, maxTasks: maxTasks}
}

func (r *Replanner) Generate(
	ctx context.Context,
	intake domain.GoalIntake,
	issues []string,
	existingPlan domain.PlannerOutput,
	taskResults map[string]string,
	userMessage, model string,
	opts PlannerOptions,
) (domain.PlannerOutput, error) {
	agents, err := r.catalog.ListAgents(ctx)
	if err != nil {
		return domain.PlannerOutput{}, fmt.Errorf("list agents: %w", err)
	}
	enabledAgents := filterEnabledAgents(agents)
	if opts.ConstrainedAgentID != nil {
		filtered := make([]domain.Agent, 0, 1)
		for _, a := range enabledAgents {
			if a.ID == *opts.ConstrainedAgentID {
				filtered = append(filtered, a)
			}
		}
		enabledAgents = filtered
	}
	if len(enabledAgents) == 0 {
		return domain.PlannerOutput{}, fmt.Errorf("no enabled agents available for replanning")
	}

	catalogs := make([]agentCatalogEntry, 0, len(enabledAgents))
	skillOwnership := make(map[string]map[string]bool)
	for _, a := range enabledAgents {
		agentSkills, err := r.catalog.ListSkillsByAgent(ctx, a.ID)
		if err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("list skills for agent %s: %w", a.Name, err)
		}
		agentRules, err := r.catalog.ListEnabledRulesByAgent(ctx, a.ID)
		if err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("list rules for agent %s: %w", a.Name, err)
		}
		catalogs = append(catalogs, agentCatalogEntry{Agent: a, Skills: agentSkills, Rules: agentRules})
		owned := make(map[string]bool, len(agentSkills))
		for _, sk := range agentSkills {
			owned[sk.ID.String()] = true
		}
		skillOwnership[a.ID.String()] = owned
	}

	plannerModel := model

	var resultsSB strings.Builder
	for taskID, result := range taskResults {
		resultsSB.WriteString("## ")
		resultsSB.WriteString(taskID)
		resultsSB.WriteString("\n")
		resultsSB.WriteString(result)
		resultsSB.WriteString("\n\n")
	}

	existingJSON, _ := goccyjson.Marshal(existingPlan)
	systemPrompt := buildReplannerSystemPrompt(catalogs, opts.ConstrainedAgentID)

	userContent := strings.Join([]string{
		"User request: " + userMessage,
		"Purpose: " + intake.Purpose,
		"Goal: " + intake.Goal,
		"Verification issues:\n- " + strings.Join(issues, "\n- "),
		"Existing plan:\n" + string(existingJSON),
		"Task results:\n" + resultsSB.String(),
	}, "\n\n")

	var lastErr error
	var corrections []domain.Message
	for attempt := range maxPlannerRetries + 1 {
		messages := make([]domain.Message, 0, 2+len(corrections))
		messages = append(messages,
			domain.Message{Role: domain.RoleSystem, Content: systemPrompt},
			domain.Message{Role: domain.RoleUser, Content: userContent},
		)
		messages = append(messages, corrections...)

		resp, err := r.llm.Chat(ctx, domain.AgentRequest{
			Messages:       messages,
			Model:          plannerModel,
			ProviderType:   opts.ProviderType,
			ResponseFormat: domain.JSONSchemaResponseFormat("replan_output", replannerOutputSchema()),
		})
		if err != nil {
			lastErr = err
			corrections = nil
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("replanner llm call failed")
			if giveUp := llmretry.Await(ctx, err, attempt, maxPlannerRetries); giveUp != nil {
				return domain.PlannerOutput{}, pipelineStepError("replanner", attempt+1, giveUp)
			}
			continue
		}

		output, err := parseRepairPlanOutput(resp.Message.Content)
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("replanner parse failed")
			corrections = pipelineCorrection(resp.Message.Content, err)
			continue
		}
		output.Purpose = intake.Purpose
		output.Goal = intake.Goal

		if err := validatePlannerOutput(output, enabledAgents, skillOwnership, r.maxTasks, existingPlan.Tasks); err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("replanner validation failed")
			corrections = pipelineRejection(resp.Message.Content, err)
			continue
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("replan_created", map[string]int{"repair_task_count": len(output.Tasks)})
		}
		return output, nil
	}
	return domain.PlannerOutput{}, pipelineStepError("replanner", maxPlannerRetries+1, lastErr)
}

func buildReplannerSystemPrompt(catalogs []agentCatalogEntry, constrainedAgentID *uuid.UUID) string {
	var sb strings.Builder
	sb.WriteString(`You are an orchestration replanner. Create repair-only tasks to fix verification issues. Do not repeat completed work.

Respond with a single JSON object matching the provided schema.

Rules:
- Return only new repair tasks with unique ids not present in the existing plan.
- Each task must address specific verification issues.
- Every task must include tool_names (array, may be empty).
- skill_ids must be UUIDs from that task's agent skills only (may be empty).
- depends_on may reference existing task ids from the prior plan.
- Tasks sharing a parallel_group run CONCURRENTLY and cannot see each other's work; depends_on is the only way to order them.
- HARD CONSTRAINT, machine-checked before the repair plan runs: within one parallel_group AT MOST ONE task may list a board-write tool (create_board_task, move_board_task, update_board_task) in tool_names. A second one rejects the whole repair plan — chain the extra writers with depends_on instead.
- A verification issue that is already covered by an existing board task is fixed by updating that task, never by creating a second one for the same work.
- HARD CONSTRAINT, machine-checked: create_board_task is counted across the original plan AND this repair plan together, and at most one task in that combined set may list it. The original plan already opened whatever records this request needs — repair by updating or commenting on them (update_board_task, add_task_comment, move_board_task), not by opening more.
- Do not repair "the feature is not live yet", "the code has not changed", "QA has not run". Those resolve when the assigned agent works the board task; there is nothing for a repair task to do.
- HARD CONSTRAINT, machine-checked: a repair task may not reuse the TITLE of a task in the existing plan. Repeating a finished subtask verbatim runs it a second time — the board then shows the same step twice, one copy "completed" and one still working. Name what is still MISSING, in its own words, and put the finished task in depends_on.
- HARD CONSTRAINT, machine-checked: the same applies to the DESCRIPTION. A repair task may not carry the description of a task that already ran, and no two repair tasks may share one description — renaming a finished instruction does not make it a new one. Each repair task describes only the specific gap it closes.
- A repair task that changes code follows the same description shape as any implementation subtask — ordered phases inside the one task: (1) Scope: the specific fix, named concretely (files, endpoints, screens); (2) Out of scope: what it must NOT touch — the finished work around it, unrelated bugs, refactors; (3) Verify: the build/test commands to run and what output counts as passing; (4) Close: tick the criteria the verified fix satisfies and report what changed with the command output that proved it.
- HARD CONSTRAINT, machine-checked: no repair task may consist of board bookkeeping alone (tool_names only claim_board_task / move_board_task / add_task_comment / ask_user). A column move is not a repair: the control plane performs the hand-off move to code_review itself once the implementing run finishes. "The task was not moved to code_review" is therefore never a repairable issue.
`)
	if constrainedAgentID != nil {
		sb.WriteString(fmt.Sprintf("- All tasks must use agent_id=%s only.\n", constrainedAgentID.String()))
	}
	sb.WriteString(`
Available agents:
`)
	for _, entry := range catalogs {
		a := entry.Agent
		sb.WriteString(fmt.Sprintf("\n## Agent id=%s name=%s type=%s\n", a.ID, a.Name, a.SubagentType))
		if len(entry.Skills) > 0 {
			sb.WriteString("Skills:\n")
			for _, sk := range entry.Skills {
				if sk.Enabled {
					sb.WriteString(fmt.Sprintf("- id=%s name=%s\n", sk.ID, sk.Name))
				}
			}
		}
	}
	return sb.String()
}
