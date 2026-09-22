package orchestrator

import (
	"context"
	"fmt"
	"strings"

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

func buildIntakeSystemPrompt(opts IntakeOptions) string {
	var sb strings.Builder
	sb.WriteString(`You extract the user's purpose and measurable goal from their message.

Respond with a single JSON object matching the provided schema.

Rules:
- Read the full conversation thread in the messages. Prior turns may include clarification questions and user answers.
- If the conversation already answers open questions, set ready to true and fold those answers into purpose, goal, and constraints. Do not ask again for information the user already provided in this thread.
- Clarification card answers arrive as user messages in the form "question prompt: answer". Treat those as answered — never re-ask the same topic.
- Never assume missing product information. If scope, requirements, targets, or constraints are still unclear after reading the thread, set ready to false and ask only for what remains unanswered.
- A system message may list the records this conversation already created (board tasks, projects, comments) with their ids. When the user's message is about one of those records — move it, advance it, update it, chase it — the goal is an action on THAT record: name it and its id in the goal, and set ready to true. It is not a new piece of work and it is not a question.
- When ready is true: purpose (why), goal (measurable outcome), and constraints (empty array if none) are required; questions must be an empty array.
- When ready is false: questions must have at least one item; each question must follow the ask_user tool schema (text mode or choice mode; choice mode must end with id other; set allow_multiple true when multiple selections are valid); purpose and goal may be partial.
- Only set ready to true when you can proceed without guessing.
- Do not include any text outside the JSON object.

## local-llm software team context
The system delivers software through AI agents: backend-developer, frontend-developer, mobile-developer, qa-agent, product-manager.
The human is the product stakeholder — not the implementer. Engineers on the team build the product.

When the request involves creating tasks, planning features, websites, apps, or any software delivery:
- Do NOT ask about the stakeholder's personal coding skills, self-learning plans, DIY builders (WordPress/Wix/Webflow), or which platform they will personally use.
- DO ask about product goals, target users, must-have capabilities, business deadlines, content constraints, integrations, and success criteria.
- Assume the agent team will implement using the project repository stack unless the stakeholder specifies otherwise.
- Never ask the stakeholder for information the system already stores or the team can look up — repository/codebase access, which repos or projects exist, board state, or team members. Read those from the workspace state below.
`)
	if opts.Workspace != "" {
		sb.WriteString("\n")
		sb.WriteString(opts.Workspace)
		sb.WriteString("\n")
	}
	if opts.SoloAgentName != "" {
		sb.WriteString(fmt.Sprintf("\n## Active agent: %s\n", opts.SoloAgentName))
		if opts.SoloAgentDescription != "" {
			sb.WriteString(opts.SoloAgentDescription)
			sb.WriteString("\n")
		}
		// Each section claims to exist only when it is really there.
		if len(opts.SoloAgentRules) > 0 {
			sb.WriteString("This agent's own rules follow. A question any of them forbids must not be asked — drop it and set ready to true, or route it the way the rule says.\n")
			for _, r := range opts.SoloAgentRules {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", r.Name, r.Content))
			}
		}
		enabledSkills := make([]domain.Skill, 0, len(opts.SoloAgentSkills))
		for _, sk := range opts.SoloAgentSkills {
			if sk.Enabled {
				enabledSkills = append(enabledSkills, sk)
			}
		}
		if len(enabledSkills) > 0 {
			// Metadata only — the executor loads the skill bodies when the task runs.
			sb.WriteString("\nSkills this agent already has (never ask the stakeholder for what these cover):\n")
			for _, sk := range enabledSkills {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", sk.Name, sk.Description))
			}
		}
	}
	sb.WriteString("\n")
	sb.WriteString(prompt.UserFacingLanguageRule(opts.Lang))
	sb.WriteString("\n")
	return sb.String()
}
