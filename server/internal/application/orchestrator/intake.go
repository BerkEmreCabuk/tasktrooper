package orchestrator

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type IntakeExtractor struct {
	llm port.LLMClient
}

func NewIntakeExtractor(llm port.LLMClient) *IntakeExtractor {
	return &IntakeExtractor{llm: llm}
}

type IntakeOptions struct {
	SoloAgentName        string
	SoloAgentDescription string
	Lang                 string
	ProviderType         domain.LLMProviderType
	// Rendered projects/repositories snapshot; the only thing stopping an untooled
	// intake from asking whether the team has codebase access.
	Workspace string
	// Carry the constrained agent's rules and skills into the prompt, or intake asks what the rules forbid.
	SoloAgentRules  []domain.OrchestratorRule
	SoloAgentSkills []domain.Skill
}

func (e *IntakeExtractor) Extract(ctx context.Context, userMessage string, history []domain.Message, model string, opts IntakeOptions) (domain.GoalIntake, error) {
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("goal_intake_start", map[string]string{"model": model})
	}

	systemPrompt := buildIntakeSystemPrompt(opts)

	var lastErr error
	var corrections []domain.Message
	for attempt := range maxPlannerRetries + 1 {
		resp, err := e.llm.Chat(ctx, domain.AgentRequest{
			Messages:       buildPipelineLLMMessages(systemPrompt, history, userMessage, nil, corrections),
			Model:          model,
			ProviderType:   opts.ProviderType,
			ResponseFormat: domain.JSONSchemaResponseFormat("goal_intake", intakeOutputSchema()),
		})
		if err != nil {
			lastErr = err
			corrections = nil
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("intake llm call failed")
			if giveUp := llmretry.Await(ctx, err, attempt, maxPlannerRetries); giveUp != nil {
				return domain.GoalIntake{}, pipelineStepError("goal intake", attempt+1, giveUp)
			}
			continue
		}

		intake, err := parseGoalIntake(resp.Message.Content)
		if err != nil {
			lastErr = err
			// Weak local models return prose first; the self-correction retry recovers.
			if attempt < maxPlannerRetries {
				log.Debug().Err(err).Int("attempt", attempt+1).Msg("intake parse failed, retrying")
			} else {
				log.Warn().Err(err).Int("attempt", attempt+1).Msg("intake parse failed")
			}
			corrections = pipelineCorrection(resp.Message.Content, err)
			continue
		}

		if rec := activity.FromContext(ctx); rec != nil {
			payload := map[string]any{"ready": intake.Ready, "purpose": intake.Purpose, "goal": intake.Goal}
			if !intake.Ready {
				payload["question_count"] = len(intake.Questions)
			}
			rec.Step("goal_intake_complete", payload)
		}
		return intake, nil
	}
	return domain.GoalIntake{}, pipelineStepError("goal intake", maxPlannerRetries+1, lastErr)
}

func parseGoalIntake(content string) (domain.GoalIntake, error) {
	trimmed := stripJSONWrapper(content)

	var raw struct {
		Ready       bool                           `json:"ready"`
		Purpose     string                         `json:"purpose"`
		Goal        string                         `json:"goal"`
		Constraints []string                       `json:"constraints"`
		Questions   []domain.ClarificationQuestion `json:"questions"`
	}
	if err := parseLLMJSON(trimmed, &raw); err != nil {
		return domain.GoalIntake{}, fmt.Errorf("invalid intake json: %w", err)
	}
	if raw.Constraints == nil {
		return domain.GoalIntake{}, fmt.Errorf("intake missing constraints field")
	}
	if raw.Questions == nil {
		return domain.GoalIntake{}, fmt.Errorf("intake missing questions field")
	}

	intake := domain.GoalIntake{
		Ready:       raw.Ready,
		Purpose:     raw.Purpose,
		Goal:        raw.Goal,
		Constraints: raw.Constraints,
		Questions:   domain.NormalizeClarificationQuestions(raw.Questions),
	}

	if intake.Ready {
		if intake.Purpose == "" {
			return domain.GoalIntake{}, fmt.Errorf("intake missing purpose")
		}
		if intake.Goal == "" {
			return domain.GoalIntake{}, fmt.Errorf("intake missing goal")
		}
		if len(intake.Questions) > 0 {
			return domain.GoalIntake{}, fmt.Errorf("intake ready=true requires empty questions")
		}
		return intake, nil
	}

	if err := validateClarificationQuestions(intake.Questions); err != nil {
		return domain.GoalIntake{}, err
	}
	return intake, nil
}

func ParseGoalIntakeForTest(content string) (domain.GoalIntake, error) {
	return parseGoalIntake(content)
}

func BuildIntakeSystemPromptForTest(opts IntakeOptions) string {
	return buildIntakeSystemPrompt(opts)
}

type intakeRuleData struct{ Name, Content string }
type intakeSkillData struct{ Name, Description string }

type intakeSystemData struct {
	Workspace            string
	SoloAgentName        string
	SoloAgentDescription string
	SoloAgentRules       []intakeRuleData
	EnabledSkills        []intakeSkillData
	LanguageRule         string
}

var intakeSystemKey = prompt.Define("orchestrator.intake_system", intakeSystemData{
	Workspace: "", SoloAgentName: "", SoloAgentDescription: "",
	SoloAgentRules: []intakeRuleData{{Name: "r", Content: "c"}},
	EnabledSkills:  []intakeSkillData{{Name: "s", Description: "d"}},
	LanguageRule:   "lang",
})

func buildIntakeSystemPrompt(opts IntakeOptions) string {
	data := intakeSystemData{
		Workspace:            opts.Workspace,
		SoloAgentName:        opts.SoloAgentName,
		SoloAgentDescription: opts.SoloAgentDescription,
		LanguageRule:         prompt.UserFacingLanguageRule(opts.Lang),
	}
	for _, r := range opts.SoloAgentRules {
		data.SoloAgentRules = append(data.SoloAgentRules, intakeRuleData{Name: r.Name, Content: r.Content})
	}
	for _, sk := range opts.SoloAgentSkills {
		if sk.Enabled {
			data.EnabledSkills = append(data.EnabledSkills, intakeSkillData{Name: sk.Name, Description: sk.Description})
		}
	}
	return intakeSystemKey.Render(data)
}
