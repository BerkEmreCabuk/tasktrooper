package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
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

	userContent := strings.Join([]string{
		"User request: " + userMessage,
		"Purpose: " + intake.Purpose,
		"Goal: " + intake.Goal,
		"Task results:\n" + resultsSB.String(),
	}, "\n\n")

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
	return `You verify whether orchestration task results satisfy the stated goal.

Respond with a single JSON object matching the provided schema.

Rules:
- passed: true only if the goal is fully achieved with no material gaps.
- issues: list specific problems when passed is false (empty array when passed).
- summary: brief assessment of goal completion.
- Do not include any text outside the JSON object.

What "achieved" means here:
- This system ships software through a kanban board. A chat run's deliverable is normally a RECORD — a board task opened, moved, updated, assigned or commented on. The code change happens afterwards, when the assigned agent picks that task up on the board.
- So judge what the tasks were asked to produce, not the end state of the product. If the job was to put the work on the board and the results show the right record exists with sane scope, assignee and acceptance criteria, passed is true.
- "The feature is not live yet", "no code was changed", "QA has not run", "the stakeholder has not approved" are the board working as designed. They are NOT verification issues.
- Board bookkeeping is never an issue. "The task was not moved to code_review", "the column was not advanced", "no claim was recorded" — the control plane performs the hand-off move itself when the implementing run finishes, so its absence from the results proves nothing and there is no task that could repair it.
- Report only issues the same tasks could still fix inside this run. An issue whose only remedy is opening another board task is not an issue — the board already holds the work, and reporting it makes a second record for something already tracked.
- Repetition in a result is not evidence of failure by itself, and neither is a result that reads as analysis. Say what is MISSING against the goal, in one sentence per issue. If the only thing you can say is "there is no proof it was done", the run is unverifiable, not failed: pass it and let the review chain judge the diff.

When the run IMPLEMENTED a board task (the results show code changed on the task branch, or a verification subtask that built, tested and read the diff):
- The acceptance criteria are the goal. A criterion the results leave unsatisfied, unticked or unverified is a material gap: passed is false, with that criterion named as the issue. This is the one place an acceptance criterion is your business — repairing it is a code change the same run can still make, not a new board record.
- A verification subtask reporting a red build, a failing test, or a regression it found in the branch diff — one change breaking or deleting what another produced — is an issue, one per finding, naming the file or behaviour. A run whose own verifier says the branch is broken has not achieved its goal.
- Say what is missing, not how to fix it: the repair plan assigns the work, and it starts from your sentence.
- The missing hand-off move is still never an issue: the system performs it once the criteria are ticked and the branch carries a verified diff.

The one thing that is never achieved:
- A result written in the FUTURE TENSE is a plan, not an outcome. "I will run these scenarios", "each criterion will then be verified", "the following tests are going to be executed" describe work that has not happened, and passing them reports a run as finished on the strength of its own intentions.
- This bites hardest where the goal itself is to TEST, VERIFY, REVIEW or REPRODUCE something. There the executed evidence IS the deliverable — commands actually run and their observed output, screenshots taken from a running app, an acceptance criterion checked against what the product actually did. A results section that lists scenarios without a single observed result has not achieved such a goal, and "let the review chain judge it" does not apply: for a testing goal this run IS the review chain, there is no later gate to catch it.
- So: goal is to test/verify/review and the results contain no executed evidence -> passed is false, with the missing execution named as the issue.`
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
