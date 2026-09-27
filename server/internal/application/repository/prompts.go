package repository

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string the repository gates render —
// see catalog/system/guards/** and catalog/system/prompts/repository/**.
// Nothing here is a Go string literal; each Key's sample only has to be
// renderable, not realistic.

type criteriaIncompleteInput struct {
	Target string
	Count  int
	Open   []string
}

var criteriaIncompleteKey = prompt.Define("guard.criteria_incomplete", criteriaIncompleteInput{
	Target: "ready_for_qa", Count: 1, Open: []string{"[id] a"},
})

type criteriaRejectedInput struct {
	Target   string
	Count    int
	Role     string
	Rejected []string
}

var criteriaRejectedKey = prompt.Define("guard.criteria_rejected", criteriaRejectedInput{
	Target: "pm_uat", Count: 1, Role: "qa", Rejected: []string{"[id] a (note)"},
})

type criteriaUncheckedInput struct {
	Target    string
	Count     int
	Role      string
	Unchecked []string
}

var criteriaUncheckedKey = prompt.Define("guard.criteria_unchecked", criteriaUncheckedInput{
	Target: "done", Count: 1, Role: "qa", Unchecked: []string{"[id] a"},
})

type testCasesMissingInput struct{ Target string }

var testCasesMissingKey = prompt.Define("guard.test_cases_missing", testCasesMissingInput{Target: "pm_uat"})

type testCasesPlannedInput struct {
	Target  string
	Count   int
	Planned []string
}

var testCasesPlannedKey = prompt.Define("guard.test_cases_planned", testCasesPlannedInput{
	Target: "pm_uat", Count: 1, Planned: []string{"a case"},
})

var selfMoveNeedRevisionKey = prompt.Define("guard.self_move_need_revision", struct{}{})

var selfMoveTodoKey = prompt.Define("guard.self_move_todo", struct{}{})

type workOrderBlockedInput struct{ Labels []string }

var workOrderBlockedKey = prompt.Define("guard.work_order_blocked", workOrderBlockedInput{Labels: []string{"T-5 (API migration) [in_progress]"}})

type criterionVerdictWrongColumnInput struct{ Key, Column string }

var criterionVerdictWrongColumnKey = prompt.Define("guard.criterion_verdict_wrong_column", criterionVerdictWrongColumnInput{Key: "T-4", Column: "in_progress"})

type workflowSetupTaskBriefInput struct{ Kind string }

var workflowSetupTaskBriefKey = prompt.Define("repository.workflow_setup_task_brief", workflowSetupTaskBriefInput{Kind: "backend"})
