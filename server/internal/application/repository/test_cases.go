package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SetTestCaseStore wires the task's test-round list. Nil leaves the board
// exactly as it was before it existed: criteria, and no record of what was
// actually exercised to satisfy them.
func (s *Service) SetTestCaseStore(store port.TaskTestCaseStore) {
	s.testCases = store
}

func (s *Service) ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error) {
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	items, err := s.testCases.ListByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.TaskTestCase{}
	}
	return items, nil
}

// RecordTestCases is the agent path: write the derived cases, then come back
// and write their results. Matching is by title, so the same call shape serves
// both moments.
func (s *Service) RecordTestCases(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	normalized, err := normalizeTestCases(items)
	if err != nil {
		return nil, err
	}
	if err := s.validateCriterionLinks(ctx, taskID, normalized); err != nil {
		return nil, err
	}
	return s.testCases.UpsertForTask(ctx, taskID, normalized)
}

// ReplaceTestCases is the human path: the drawer sends the list it shows, and
// what it does not send is deleted.
func (s *Service) ReplaceTestCases(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.testCases == nil {
		return nil, fmt.Errorf("test cases not enabled")
	}
	normalized, err := normalizeTestCases(items)
	if err != nil {
		return nil, err
	}
	if err := s.validateCriterionLinks(ctx, taskID, normalized); err != nil {
		return nil, err
	}
	return s.testCases.ReplaceForTask(ctx, taskID, normalized)
}

func (s *Service) UpdateTestCase(ctx context.Context, repositoryID, taskID, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskTestCase{}, err
	}
	if s.testCases == nil {
		return domain.TaskTestCase{}, fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	if existing.TaskID != taskID {
		return domain.TaskTestCase{}, fmt.Errorf("test case %s does not belong to task %s", testCaseID, taskID)
	}
	// A partial update carries only what changed, so the merge happens before
	// validation: "failed" with no `actual` in the payload is legal when the
	// stored row already has one.
	normalized, err := mergeTestCase(existing, item).Normalize()
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	return s.testCases.Update(ctx, testCaseID, normalized)
}

// SetTestCaseResult is the agent's single-case update: one executed case, its
// verdict and the evidence behind it.
func (s *Service) SetTestCaseResult(ctx context.Context, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	if s.testCases == nil {
		return domain.TaskTestCase{}, fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	normalized, err := mergeTestCase(existing, item).Normalize()
	if err != nil {
		return domain.TaskTestCase{}, err
	}
	return s.testCases.Update(ctx, testCaseID, normalized)
}

func (s *Service) DeleteTestCase(ctx context.Context, repositoryID, taskID, testCaseID uuid.UUID) error {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return err
	}
	if s.testCases == nil {
		return fmt.Errorf("test cases not enabled")
	}
	existing, err := s.testCases.Get(ctx, testCaseID)
	if err != nil {
		return err
	}
	if existing.TaskID != taskID {
		return fmt.Errorf("test case %s does not belong to task %s", testCaseID, taskID)
	}
	return s.testCases.Delete(ctx, testCaseID)
}

func normalizeTestCases(items []domain.TaskTestCaseInput) ([]domain.TaskTestCaseInput, error) {
	out := make([]domain.TaskTestCaseInput, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		normalized, err := item.Normalize()
		if err != nil {
			return nil, err
		}
		// The store matches by title, so two cases with the same title in ONE
		// batch would silently collapse into the last one. Refusing here says
		// so instead of losing a case.
		key := strings.ToLower(normalized.Title)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("two test cases share the title %q; titles identify a case, so give them distinct ones", normalized.Title)
		}
		seen[key] = struct{}{}
		if normalized.Position == 0 {
			normalized.Position = i + 1
		}
		out = append(out, normalized)
	}
	return out, nil
}

// validateCriterionLinks refuses a case pointed at a criterion of another task.
// The link is what lets the drawer group cases under the criterion they prove,
// and a cross-task id there would attribute one task's evidence to another.
func (s *Service) validateCriterionLinks(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) error {
	if s.criteria == nil {
		return nil
	}
	var needed bool
	for _, item := range items {
		if item.CriterionID != nil {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	criteria, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil {
		return err
	}
	own := make(map[uuid.UUID]struct{}, len(criteria))
	for _, c := range criteria {
		own[c.ID] = struct{}{}
	}
	for _, item := range items {
		if item.CriterionID == nil {
			continue
		}
		if _, ok := own[*item.CriterionID]; !ok {
			return fmt.Errorf("criterion %s is not on this task; leave criterion_id empty for a case no criterion states", *item.CriterionID)
		}
	}
	return nil
}

func mergeTestCase(existing domain.TaskTestCase, in domain.TaskTestCaseInput) domain.TaskTestCaseInput {
	out := domain.TaskTestCaseInput{
		CriterionID: existing.CriterionID,
		Title:       existing.Title,
		Category:    existing.Category,
		Status:      existing.Status,
		Expected:    existing.Expected,
		Actual:      existing.Actual,
		Evidence:    existing.Evidence,
		Notes:       existing.Notes,
		Position:    existing.Position,
	}
	if in.CriterionID != nil {
		out.CriterionID = in.CriterionID
	}
	if strings.TrimSpace(in.Title) != "" {
		out.Title = in.Title
	}
	if in.Category != "" {
		out.Category = in.Category
	}
	if in.Status != "" {
		out.Status = in.Status
	}
	if strings.TrimSpace(in.Expected) != "" {
		out.Expected = in.Expected
	}
	if strings.TrimSpace(in.Actual) != "" {
		out.Actual = in.Actual
	}
	if strings.TrimSpace(in.Evidence) != "" {
		out.Evidence = in.Evidence
	}
	if strings.TrimSpace(in.Notes) != "" {
		out.Notes = in.Notes
	}
	if in.Position != 0 {
		out.Position = in.Position
	}
	return out
}

// testCaseGate holds a QA phase open until its round is on the card.
//
// It is the enforcement half of "record the cases you ran": QA's verdict used
// to be a set of criterion checks, and the list of what was actually exercised
// lived only in the run transcript — so a round that tried three obvious things
// and a round that worked through boundaries, auth and regression left the same
// trace. The gate refuses a forward exit from the QA columns when the task has
// no test cases at all, and when some case is still `planned`: a case that was
// written down and never executed is the one thing a passing round must not
// carry.
//
// A `failed` case is deliberately not refused here. Failing is a legitimate
// outcome and its exit is need_revision, which is not a forward move; blocking
// on it would strand the round that found the bug.
func (s *Service) testCaseGate(ctx context.Context, taskID uuid.UUID, taskType domain.TaskType, prev, target domain.TaskColumn) error {
	if !s.requireCriteria || s.testCases == nil {
		return nil
	}
	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return fmt.Errorf("test case gate: workflow unavailable for %s (%w)", target, err)
	}
	if !wf.Has(prev, domain.BehaviourRequireTestCases) {
		return nil
	}
	if !wf.Has(target, domain.BehaviourForwardExit) {
		return nil
	}
	items, err := s.testCases.ListByTask(ctx, taskID)
	if err != nil {
		return nil
	}
	if len(items) == 0 {
		return fmt.Errorf("cannot move to %s: this task carries no test cases — "+
			"record the cases you derived and executed with record_test_cases (title, category, status, expected/actual, evidence), "+
			"including the ones you considered and rejected as status=invalid with the reason in notes", target)
	}
	var planned []string
	for _, c := range items {
		if c.Status == domain.TestCaseStatusPlanned {
			planned = append(planned, c.Title)
		}
	}
	if len(planned) > 0 {
		return fmt.Errorf("cannot move to %s: %d test case(s) are still planned and were never executed: %s — "+
			"run each one and record its result (passed/failed), or mark it skipped with what blocked it, or invalid with why it is not a valid case",
			target, len(planned), strings.Join(planned, "; "))
	}
	return nil
}
