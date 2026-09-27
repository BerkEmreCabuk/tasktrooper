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

type verifierUserData struct{ UserMessage, Purpose, Goal, TaskResultsBlock string }

var (
	verifierSystemKey = prompt.Define[struct{}]("orchestrator.verifier_system", struct{}{})
	verifierUserKey   = prompt.Define("orchestrator.verifier_user", verifierUserData{UserMessage: "req", Purpose: "p", Goal: "g", TaskResultsBlock: "## Task t1\ndone\n\n"})
)

type Verifier struct {
	llm port.LLMClient
}

func NewVerifier(llm port.LLMClient) *Verifier {
	return &Verifier{llm: llm}
}

func (v *Verifier) Evaluate(ctx context.Context, intake domain.GoalIntake, userMessage string, taskResults map[string]string, model string, providerType domain.LLMProviderType) (domain.VerificationResult, error) {
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("verification_start", nil)
	}

	var resultsSB strings.Builder
	for taskID, result := range taskResults {
		resultsSB.WriteString("## Task ")
		resultsSB.WriteString(taskID)
		resultsSB.WriteString("\n")
		resultsSB.WriteString(result)
		resultsSB.WriteString("\n\n")
	}

	systemPrompt := buildVerifierSystemPrompt()

	userContent := verifierUserKey.Render(verifierUserData{
		UserMessage: userMessage, Purpose: intake.Purpose, Goal: intake.Goal, TaskResultsBlock: resultsSB.String(),
	})

	var lastErr error
	for attempt := range maxPlannerRetries + 1 {
		resp, err := v.llm.Chat(ctx, domain.AgentRequest{
			Messages: []domain.Message{
				{Role: domain.RoleSystem, Content: systemPrompt},
				{Role: domain.RoleUser, Content: userContent},
			},
			Model:          model,
			ProviderType:   providerType,
			ResponseFormat: domain.JSONSchemaResponseFormat("verification_result", verifierOutputSchema()),
		})
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("verifier llm call failed")
			if giveUp := llmretry.Await(ctx, err, attempt, maxPlannerRetries); giveUp != nil {
				return domain.VerificationResult{}, pipelineStepError("verification", attempt+1, giveUp)
			}
			continue
		}

		result, err := parseVerificationResult(resp.Message.Content)
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("verifier parse failed")
			continue
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("verification_complete", map[string]any{"passed": result.Passed, "issue_count": len(result.Issues)})
		}
		return result, nil
	}
	return domain.VerificationResult{}, pipelineStepError("verification", maxPlannerRetries+1, lastErr)
}

// A chat run's only visible product is a record, so "achieved" means the record exists, not the feature shipping.
func buildVerifierSystemPrompt() string {
	return prompt.Text(verifierSystemKey)
}

func parseVerificationResult(content string) (domain.VerificationResult, error) {
	trimmed := stripJSONWrapper(content)

	var raw struct {
		Passed  bool     `json:"passed"`
		Issues  []string `json:"issues"`
		Summary string   `json:"summary"`
	}
	if err := parseLLMJSON(trimmed, &raw); err != nil {
		return domain.VerificationResult{}, fmt.Errorf("invalid verification json: %w", err)
	}
	if raw.Summary == "" {
		return domain.VerificationResult{}, fmt.Errorf("verification missing summary")
	}
	if raw.Issues == nil {
		return domain.VerificationResult{}, fmt.Errorf("verification missing issues field")
	}
	return domain.VerificationResult{
		Passed:  raw.Passed,
		Issues:  raw.Issues,
		Summary: raw.Summary,
	}, nil
}

func ParseVerificationResultForTest(content string) (domain.VerificationResult, error) {
	return parseVerificationResult(content)
}

func BuildVerifierSystemPromptForTest() string {
	return buildVerifierSystemPrompt()
}
