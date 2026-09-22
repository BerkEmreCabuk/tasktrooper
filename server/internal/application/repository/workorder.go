package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const relationWalkLimit = 512

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

func (s *Service) resolveRelationTargets(ctx context.Context, inputs []domain.TaskRelationInput, relType domain.TaskRelationType) ([]domain.TaskRelationInput, error) {
	typed := make([]domain.TaskRelationInput, 0, len(inputs))
	for _, in := range inputs {
		in.RelationType = relType
		typed = append(typed, in)
	}
	return s.resolveRelations(ctx, typed)
}

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

	task.BeforeDeploy = stored.BeforeDeploy
	task.UpdatedAt = stored.UpdatedAt
	return task
}

func withBeforeDeploy(task domain.BoardTask, before *string) domain.BoardTask {
	task.BeforeDeploy = before
	return task
}

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
