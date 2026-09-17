package board

import "testing"

// The dropped set is the wording agents actually produced on analiz and
// implementation tasks; the kept set is what must survive, including product
// criteria that legitimately talk about tasks, columns and approvals — this
// product IS a board, so its own features are written in that vocabulary.
func TestBoardActionReason(t *testing.T) {
	dropped := []string{
		"Analiz task is moved to analiz_review for human approval",
		"Given the analysis is complete, When the architect finishes, Then the task is moved to the analiz_review column",
		"Task moved to ready_for_qa once implementation is done",
		"The card sits in analiz_review awaiting human approval",
		"Spec and implementation plan attached to the analiz task with add_task_document",
		"Questions for the stakeholder recorded via add_task_comment",
		"Implementation tasks are created for each unit of work",
		"Create implementation tasks for every repository the change touches",
		"Follow-up tasks opened for the remaining screens",
		"The task is assigned to the system-architect",
	}
	for _, text := range dropped {
		if boardActionReason(text) == "" {
			t.Errorf("expected board action, kept: %q", text)
		}
	}

	kept := []string{
		"Given a create-task form, When I submit a 201-char title, Then I get 422 with message \"title must be at most 200 characters\"",
		"The spec names every file to change and the interface of each new unit",
		"The plan orders the work so the API ships before its client",
		"Given a repository with no CI configured, When a pipeline is triggered, Then its status is skipped with gate_reason no_ci_configured",
		"Given a task in ready_for_qa, When the pipeline fails, Then the card shows a red build icon",
		"Board columns render in position order on the board page",
		"Given an agent without move permission, When it calls the API, Then it gets 403",
		"The migration is done in a single transaction",
		"Given the criteria loop guard fires three times, When no human has commented, Then the task is put into blocked with a park reason",
		"Given a task with an unanswered question, When the agent cannot proceed, Then the task ends up in blocked with the question as the park reason",
		"Given the pipeline fails three times in a row, When no one intervenes, Then the task is moved into blocked with a stated park reason",
	}
	for _, text := range kept {
		if reason := boardActionReason(text); reason != "" {
			t.Errorf("expected keep, dropped %q: %s", text, reason)
		}
	}
}

func TestCriteriaInputsSplitsAndPositions(t *testing.T) {
	items, dropped := criteriaInputs([]string{
		"The endpoint returns 201 with the created task",
		"   ",
		"Task moved to done column",
		"The response body carries the board key",
	})
	if len(items) != 2 {
		t.Fatalf("kept %d criteria, want 2: %+v", len(items), items)
	}
	if items[0].Position != 1 || items[1].Position != 2 {
		t.Errorf("positions not contiguous after a drop: %+v", items)
	}
	if len(dropped) != 1 || dropped[0].Text != "Task moved to done column" {
		t.Errorf("unexpected dropped set: %+v", dropped)
	}
	if dropped[0].Reason == "" {
		t.Error("dropped criterion carries no reason")
	}
}
