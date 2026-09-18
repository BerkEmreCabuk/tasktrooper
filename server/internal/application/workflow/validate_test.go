package workflow_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestValidateStages_UnknownBehaviour(t *testing.T) {
	stages := []domain.WorkflowStage{{
		Column: "todo", Kind: domain.StageKindQueue,
		Behaviours: []domain.BehaviourRef{{Key: "not_a_real_behaviour"}},
	}}
	problems := workflow.ValidateStages(stages, nil)
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0].Message, "unknown behaviour")
}

func TestValidateStages_WrongScope(t *testing.T) {
	// document_deliverable is type-scoped (domain.BehaviourScopeType); attaching
	// it to a stage must be refused.
	stages := []domain.WorkflowStage{{
		Column: "in_progress", Kind: domain.StageKindWork,
		Behaviours: []domain.BehaviourRef{{Key: domain.BehaviourDocumentDeliverable}},
	}}
	problems := workflow.ValidateStages(stages, nil)
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0].Message, "scoped")
}

func TestValidateStages_ParamColumnDoesNotExist(t *testing.T) {
	stages := []domain.WorkflowStage{{
		Column: "in_progress", Kind: domain.StageKindWork,
		Behaviours: []domain.BehaviourRef{{Key: domain.BehaviourAdvanceOnDiff, Params: map[string]string{"to": "not_a_column"}}},
	}}
	validColumns := map[string]bool{"in_progress": true, "code_review": true}
	problems := workflow.ValidateStages(stages, validColumns)
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0].Message, "unknown column")

	// The same behaviour with a real column must pass clean.
	stages[0].Behaviours[0].Params["to"] = "code_review"
	assert.Empty(t, workflow.ValidateStages(stages, validColumns))
}

func TestValidateStages_TooManyWorkersOrApprovers(t *testing.T) {
	stages := []domain.WorkflowStage{{
		Column: "in_progress", Kind: domain.StageKindWork,
		Participants: []domain.StageParticipant{
			{RoleID: uuid.New(), Mode: domain.ParticipantModeWorker},
			{RoleID: uuid.New(), Mode: domain.ParticipantModeWorker},
		},
	}}
	problems := workflow.ValidateStages(stages, nil)
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0].Message, "one worker")

	stages = []domain.WorkflowStage{{
		Column: "code_review", Kind: domain.StageKindReview,
		Participants: []domain.StageParticipant{
			{RoleID: uuid.New(), Mode: domain.ParticipantModeApprover},
			{RoleID: uuid.New(), Mode: domain.ParticipantModeApprover},
		},
	}}
	problems = workflow.ValidateStages(stages, nil)
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0].Message, "one approver")
}

func TestValidateStages_RequiredParamMissing(t *testing.T) {
	stages := []domain.WorkflowStage{{
		Column: "code_review", Kind: domain.StageKindReview,
		Behaviours: []domain.BehaviourRef{{Key: domain.BehaviourReviewChainStage}},
	}}
	problems := workflow.ValidateStages(stages, nil)
	assert.NotEmpty(t, problems)
}

func TestValidateStages_CleanStagePasses(t *testing.T) {
	stages := []domain.WorkflowStage{{
		Column: "code_review", Kind: domain.StageKindReview,
		Behaviours: []domain.BehaviourRef{
			{Key: domain.BehaviourRouteToSubscribers},
			{Key: domain.BehaviourReviewVerdictSweep, Params: map[string]string{"pass_to": "ready_for_qa"}},
		},
		Participants: []domain.StageParticipant{{RoleID: uuid.New(), Mode: domain.ParticipantModeApprover}},
	}}
	validColumns := map[string]bool{"code_review": true, "ready_for_qa": true}
	assert.Empty(t, workflow.ValidateStages(stages, validColumns))
}
