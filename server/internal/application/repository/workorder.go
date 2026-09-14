package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// relationWalkLimit bounds the cycle search. A board that reached this many
// edges from one task has a planning problem no guard can fix, and the bound is
// what stops a graph corrupted by a write that predates this guard from turning
// every task creation into an unbounded walk.
const relationWalkLimit = 512

// SetBlockers declares that every task in blockers must be finished before
// taskID may be worked on, writing them as `blocks` rows with the BLOCKER as
// the source (see domain.TaskRelationBlocks for why that direction is fixed).
//
// Additive rather than replacing: unlike deploy order, a blocker is normally
// declared once at creation by whoever split the work, and a later caller that
// knows about one more dependency should not have to restate the ones it does
// not know about. Dropping a stale blocker is a delete, which is a different
// (and rarer) operation than adding one.
//
// A cycle is refused here rather than discovered later. There is no order that
// satisfies one, so every task in it would park on work_order forever with the
// sweeper dutifully confirming, once a minute, that none of them can start.
func (s *Service) SetBlockers(ctx context.Context, taskID uuid.UUID, blockers []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	if s.relations == nil {
		return nil, fmt.Errorf("relation store unavailable")
	}
	resolved, err := s.resolveRelationTargets(ctx, blockers, domain.TaskRelationBlocks)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(resolved))
	seen := make(map[uuid.UUID]bool, len(resolved))
	for _, rel := range resolved {
		if rel.TargetTaskID == taskID {
			return nil, fmt.Errorf("a task cannot be blocked by itself")
		}
		if seen[rel.TargetTaskID] {
			continue
		}
		seen[rel.TargetTaskID] = true
		ids = append(ids, rel.TargetTaskID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	for _, blockerID := range ids {
		if err := s.guardWorkOrderCycle(ctx, taskID, blockerID); err != nil {
			return nil, err
		}
	}
	return s.relations.AddBlockers(ctx, taskID, ids)
}

// resolveRelationTargets turns relation inputs (id or board key) into inputs
// carrying an id, forcing the relation type. It is resolveRelations with the
// type decided by the caller instead of by the payload, which is what every
// typed entry point (deploy order, work order, the analysis reference) wants.
func (s *Service) resolveRelationTargets(ctx context.Context, inputs []domain.TaskRelationInput, relType domain.TaskRelationType) ([]domain.TaskRelationInput, error) {
	typed := make([]domain.TaskRelationInput, 0, len(inputs))
	for _, in := range inputs {
		in.RelationType = relType
		typed = append(typed, in)
	}
	return s.resolveRelations(ctx, typed)
}

// guardWorkOrderCycle refuses a blocker that the blocked task already blocks,
// directly or through a chain.
//
// The edge about to be written is blocker → task ("blocker first"). It closes a
// cycle exactly when task already reaches blocker by following blocks edges
// forward, so the search runs from the task and looks for the blocker — and the
// path it found is in the error, because "cycle detected" without the chain
// leaves the caller to rediscover it by hand.
func (s *Service) guardWorkOrderCycle(ctx context.Context, taskID, blockerID uuid.UUID) error {
	path, err := s.relationPath(ctx, taskID, blockerID, domain.TaskRelationBlocks)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return nil
	}
	return fmt.Errorf("work-order cycle refused: %s already has to be finished before %s (%s), so it cannot also wait for it",
		s.taskLabelByID(ctx, taskID), s.taskLabelByID(ctx, blockerID), strings.Join(path, " → "))
}

// guardDeployOrderCycle is the same guard for shipping order. The edge is
// task → dependency ("dependency ships first"), so a cycle exists when the
// dependency already depends on the task.
func (s *Service) guardDeployOrderCycle(ctx context.Context, taskID, dependencyID uuid.UUID) error {
	path, err := s.relationPath(ctx, dependencyID, taskID, domain.TaskRelationDeployDependsOn)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return nil
	}
	return fmt.Errorf("deploy-order cycle refused: %s already ships after %s (%s), so it cannot also ship before it",
		s.taskLabelByID(ctx, dependencyID), s.taskLabelByID(ctx, taskID), strings.Join(path, " → "))
}

// relationPath walks relations of one type forward from `from` and returns the
// chain of labels that reaches `to`, or nil when it does not.
//
// Depth-first with an explicit stack and a visited set: the path matters (it is
// what the error quotes), and a diamond in the graph must not be walked twice.
func (s *Service) relationPath(ctx context.Context, from, to uuid.UUID, relType domain.TaskRelationType) ([]string, error) {
	if s.relations == nil || from == uuid.Nil || to == uuid.Nil {
		return nil, nil
	}
	if from == to {
		return []string{s.taskLabelByID(ctx, from)}, nil
	}
	type step struct {
		id   uuid.UUID
		path []string
	}
	visited := map[uuid.UUID]bool{from: true}
	stack := []step{{id: from, path: []string{s.taskLabelByID(ctx, from)}}}
	for visits := 0; len(stack) > 0 && visits < relationWalkLimit; visits++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		rels, err := s.relations.ListBySource(ctx, cur.id)
		if err != nil {
			// Fail closed: an unreadable graph is exactly the case where a
			// silently accepted edge would become a deadlock nobody can see.
			return nil, fmt.Errorf("relation graph could not be read: %w", err)
		}
		for _, rel := range rels {
			if rel.RelationType != relType {
				continue
			}
			label := domain.RelationLabel(rel.TargetKey, rel.TargetTitle, rel.TargetTaskID)
			if rel.TargetTaskID == to {
				return append(append([]string{}, cur.path...), label), nil
			}
			if visited[rel.TargetTaskID] {
				continue
			}
			visited[rel.TargetTaskID] = true
			stack = append(stack, step{id: rel.TargetTaskID, path: append(append([]string{}, cur.path...), label)})
		}
	}
	return nil, nil
}

// taskLabelByID renders a task as "T-12 (API migration)" for an error message.
// A task it cannot read degrades to its UUID rather than failing the guard: the
// guard's verdict is what the caller needs, the pretty name is a courtesy.
func (s *Service) taskLabelByID(ctx context.Context, id uuid.UUID) string {
	if id == uuid.Nil {
		return "(unknown task)"
	}
	repositoryID, err := s.FindTaskRepositoryID(ctx, id)
	if err != nil {
		return id.String()
	}
	task, err := s.tasks.Get(ctx, repositoryID, id)
	if err != nil {
		return id.String()
	}
	return domain.RelationLabel(task.Key, task.Title, id)
}

// syncOrderNote regenerates the fenced ordering block inside the task's
// before_deploy runbook from its relations, and writes it back when it changed.
//
// Generated rather than written by the agent that declared the order, and
// regenerated on every write that could move it, because the alternative drifts:
// an architect that types "ships after T-12" into before_deploy and then changes
// the dependency leaves the field asserting an order the release gate no longer
// enforces. The fence (domain.OrderNote*) is what makes regeneration safe —
// everything outside it is whatever the agent wrote and is copied through
// untouched.
//
// Best-effort by design: it is called after the relations are already committed,
// and a runbook that failed to re-render must not undo the ordering it describes
// or fail the caller's create/update. The release path calls it again before it
// posts the checklist, so a note lost here is regenerated at the moment it
// actually matters.
func (s *Service) syncOrderNote(ctx context.Context, task domain.BoardTask) domain.BoardTask {
	if s.relations == nil {
		return task
	}
	deployAfter, workAfter, err := s.orderLabels(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("order note: reading relations failed")
		return task
	}
	note := domain.OrderNote(deployAfter, workAfter)
	updatedText := domain.ApplyOrderNote(trimmedPtr(task.BeforeDeploy), note)
	if updatedText == trimmedPtr(task.BeforeDeploy) {
		return task
	}
	var before *string
	if updatedText != "" {
		before = &updatedText
	}
	stored, err := s.tasks.Update(ctx, withBeforeDeploy(task, before))
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("order note: writing before_deploy failed")
		return task
	}
	// Only the runbook field is taken from the write. The caller's task carries
	// criteria, relations and documents this store round-trip knows nothing
	// about, and returning the bare row would silently drop them from the
	// response the caller is about to send.
	task.BeforeDeploy = stored.BeforeDeploy
	task.UpdatedAt = stored.UpdatedAt
	return task
}

// withBeforeDeploy copies the task with a different runbook, so the store write
// cannot be handed a struct whose enrichment fields the caller still holds.
func withBeforeDeploy(task domain.BoardTask, before *string) domain.BoardTask {
	task.BeforeDeploy = before
	return task
}

// orderLabels reads the two ordering statements a task carries: what it must
// ship after (its own deploy_depends_on relations) and what it must be built
// after (the blocks relations pointing at it).
func (s *Service) orderLabels(ctx context.Context, taskID uuid.UUID) (deployAfter, workAfter []string, err error) {
	rels, err := s.relations.ListBySource(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDeployDependsOn {
			continue
		}
		deployAfter = append(deployAfter, domain.RelationLabel(rel.TargetKey, rel.TargetTitle, rel.TargetTaskID))
	}
	blockers, err := s.relations.ListBlockedBy(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	for _, rel := range blockers {
		workAfter = append(workAfter, domain.RelationLabel(rel.SourceKey, rel.SourceTitle, rel.SourceTaskID))
	}
	return deployAfter, workAfter, nil
}

// AnalysisReferences returns the analiz tasks this task was opened out of —
// its derived_from relations — each with the documents that analysis produced.
//
// This is the reading path the implementation run is given. It exists as one
// service call rather than as three from the runner (relations, then the target
// task, then its documents) because the runner must not need to know that a
// derived_from relation is how provenance is stored: it asks for the analysis
// behind a task and gets it, or gets nothing and runs without it.
//
// A reference whose target cannot be read is skipped rather than failed. An
// analiz task deleted after its implementation tasks were opened is a normal
// end state — the documents are gone, and there is nothing to say about it that
// is worth failing a run over.
func (s *Service) AnalysisReferences(ctx context.Context, taskID uuid.UUID) ([]domain.AnalysisReference, error) {
	if s.relations == nil || s.documents == nil {
		return nil, nil
	}
	rels, err := s.relations.ListBySource(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []domain.AnalysisReference
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDerivedFrom {
			continue
		}
		ref := domain.AnalysisReference{
			TaskID: rel.TargetTaskID,
			Key:    rel.TargetKey,
			Title:  rel.TargetTitle,
		}
		docs, derr := s.documents.ListByTask(ctx, rel.TargetTaskID)
		if derr != nil {
			log.Warn().Err(derr).Str("analysis_task_id", rel.TargetTaskID.String()).
				Msg("analysis reference: reading documents failed")
			continue
		}
		ref.Documents = docs
		out = append(out, ref)
	}
	return out, nil
}
