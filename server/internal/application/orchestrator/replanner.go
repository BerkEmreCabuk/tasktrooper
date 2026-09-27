package orchestrator

import (
	"context"
	"fmt"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type replannerAgentSkillData struct{ ID, Name string }
type replannerAgentData struct {
	ID, Name, Type string
	HasSkills      bool
	Skills         []replannerAgentSkillData
}
type replannerSystemData struct {
	SoloMode           bool
	ConstrainedAgentID string
	Agents             []replannerAgentData
}
type replannerUserData struct{ UserMessage, Purpose, Goal, IssuesJoined, ExistingPlanJSON, TaskResultsBlock string }

var (
	replannerSystemKey = prompt.Define("orchestrator.replanner_system", replannerSystemData{
		Agents: []replannerAgentData{{ID: "a1", Name: "n", Type: "t", HasSkills: true, Skills: []replannerAgentSkillData{{ID: "s1", Name: "n"}}}},
	})
	replannerUserKey = prompt.Define("orchestrator.replanner_user", replannerUserData{
		UserMessage: "req", Purpose: "p", Goal: "g", IssuesJoined: "issue", ExistingPlanJSON: "{}", TaskResultsBlock: "## t1\ndone\n\n",
	})
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

	userContent := replannerUserKey.Render(replannerUserData{
		UserMessage:      userMessage,
		Purpose:          intake.Purpose,
		Goal:             intake.Goal,
		IssuesJoined:     strings.Join(issues, "\n- "),
		ExistingPlanJSON: string(existingJSON),
		TaskResultsBlock: resultsSB.String(),
	})

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
			ResponseFormat: domain.JSONSchemaResponseFormat(replannerOutputSchemaKey.Name(), replannerOutputSchemaKey.Map()),
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
	data := replannerSystemData{}
	if constrainedAgentID != nil {
		data.SoloMode = true
		data.ConstrainedAgentID = constrainedAgentID.String()
	}
	for _, entry := range catalogs {
		a := entry.Agent
		ad := replannerAgentData{ID: a.ID.String(), Name: a.Name, Type: a.SubagentType, HasSkills: len(entry.Skills) > 0}
		for _, sk := range entry.Skills {
			if sk.Enabled {
				ad.Skills = append(ad.Skills, replannerAgentSkillData{ID: sk.ID.String(), Name: sk.Name})
			}
		}
		data.Agents = append(data.Agents, ad)
	}
	return replannerSystemKey.Render(data)
}
