package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var errAnnotationsDisabled = errors.New("document annotations not enabled")

func (s *Service) ListAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, documentID *uuid.UUID) ([]domain.TaskDocumentAnnotation, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.annotations == nil {
		return []domain.TaskDocumentAnnotation{}, nil
	}
	return s.annotations.ListByTask(ctx, taskID, documentID)
}

func (s *Service) AddAnnotation(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.CreateDocumentAnnotationRequest) (domain.TaskDocumentAnnotation, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	if s.annotations == nil || s.documents == nil {
		return domain.TaskDocumentAnnotation{}, errAnnotationsDisabled
	}
	if _, err := s.documents.Get(ctx, taskID, docID); err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	if err := req.Validate(); err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	return s.annotations.Create(ctx, domain.TaskDocumentAnnotation{
		TaskID:        taskID,
		DocumentID:    docID,
		Quote:         req.Quote,
		Prefix:        req.Prefix,
		Suffix:        req.Suffix,
		Body:          req.Body,
		Status:        domain.AnnotationStatusOpen,
		CreatedByType: "user",
	})
}

func (s *Service) UpdateAnnotation(ctx context.Context, repositoryID, taskID, annotationID uuid.UUID, req domain.UpdateDocumentAnnotationRequest) (domain.TaskDocumentAnnotation, error) {
	current, err := s.annotation(ctx, repositoryID, taskID, annotationID)
	if err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	next, err := domain.ApplyAnnotationUpdate(current, req)
	if err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	return s.annotations.Update(ctx, next)
}

func (s *Service) DeleteAnnotation(ctx context.Context, repositoryID, taskID, annotationID uuid.UUID) error {
	current, err := s.annotation(ctx, repositoryID, taskID, annotationID)
	if err != nil {
		return err
	}
	if current.Status != domain.AnnotationStatusOpen {
		return fmt.Errorf("%w: a %s comment is part of a review and cannot be deleted", domain.ErrAnnotationConflict, current.Status)
	}
	return s.annotations.Delete(ctx, taskID, annotationID)
}

func (s *Service) annotation(ctx context.Context, repositoryID, taskID, annotationID uuid.UUID) (domain.TaskDocumentAnnotation, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskDocumentAnnotation{}, err
	}
	if s.annotations == nil {
		return domain.TaskDocumentAnnotation{}, errAnnotationsDisabled
	}
	return s.annotations.Get(ctx, taskID, annotationID)
}

// SubmitAnnotations sends every open comment on an analysis under review back
// to its author in one go: the comments become submitted, one human comment
// summarizes the review on the card, and the task moves to need_revision
// through UpdateTask as a human move — the same path a drag onto the column
// takes, so the review gate and the evolution notifier see a rejection.
//
// The statuses and the comment are written BEFORE the move because the move
// is what dispatches the revision run, and that run reads both. If the move is
// refused the comments are put back to open so the review can be sent again.
func (s *Service) SubmitAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.SubmitAnnotationsRequest) (int, domain.BoardTask, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return 0, domain.BoardTask{}, err
	}
	if s.annotations == nil {
		return 0, domain.BoardTask{}, errAnnotationsDisabled
	}
	if task.Column != domain.TaskColumnAnalizReview {
		return 0, domain.BoardTask{}, fmt.Errorf("%w: a review can only be submitted while the task is in %s (it is in %s)",
			domain.ErrAnnotationConflict, domain.TaskColumnAnalizReview, task.Column)
	}
	all, err := s.annotations.ListByTask(ctx, taskID, nil)
	if err != nil {
		return 0, domain.BoardTask{}, err
	}
	var open []domain.TaskDocumentAnnotation
	for _, a := range all {
		if a.Status == domain.AnnotationStatusOpen {
			open = append(open, a)
		}
	}
	if len(open) == 0 {
		return 0, domain.BoardTask{}, domain.ErrNoOpenAnnotations
	}
	ids := make([]uuid.UUID, 0, len(open))
	for _, a := range open {
		ids = append(ids, a.ID)
	}
	moved, err := s.annotations.MarkSubmitted(ctx, taskID, ids, time.Now().UTC())
	if err != nil {
		return 0, domain.BoardTask{}, err
	}
	if len(moved) == 0 {
		return 0, domain.BoardTask{}, domain.ErrNoOpenAnnotations
	}
	submitted := keepAnnotations(open, moved)

	if s.comments != nil {
		if _, err := s.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
			AuthorType: "user",
			Content:    reviewSummaryComment(submitted, req.Note),
		}); err != nil {
			s.reopenAnnotations(ctx, taskID, submitted)
			return 0, domain.BoardTask{}, err
		}
	}

	column := domain.TaskColumnNeedRevision
	updated, err := s.UpdateTask(ctx, repositoryID, taskID, domain.UpdateBoardTaskRequest{
		Column: &column,
		Actor:  domain.TaskActorHuman,
	})
	if err != nil {
		s.reopenAnnotations(ctx, taskID, submitted)
		return 0, domain.BoardTask{}, err
	}
	return len(submitted), updated, nil
}

func keepAnnotations(all []domain.TaskDocumentAnnotation, ids []uuid.UUID) []domain.TaskDocumentAnnotation {
	want := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]domain.TaskDocumentAnnotation, 0, len(ids))
	for _, a := range all {
		if want[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

func (s *Service) reopenAnnotations(ctx context.Context, taskID uuid.UUID, items []domain.TaskDocumentAnnotation) {
	for _, a := range items {
		a.Status = domain.AnnotationStatusOpen
		a.SubmittedAt = nil
		if _, err := s.annotations.Update(ctx, a); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Str("annotation_id", a.ID.String()).
				Msg("submit review: putting a comment back to open failed")
		}
	}
}

const (
	summaryQuoteChars   = 80
	summaryCommentChars = 240
)

func reviewSummaryComment(items []domain.TaskDocumentAnnotation, note string) string {
	var sb strings.Builder
	noun := "comments"
	if len(items) == 1 {
		noun = "comment"
	}
	fmt.Fprintf(&sb, "Analysis review: %d %s\n", len(items), noun)
	for i, a := range items {
		fmt.Fprintf(&sb, "\n%d. \"%s\" → %s", i+1,
			clipRunes(oneLine(a.Quote), summaryQuoteChars),
			clipRunes(oneLine(a.Body), summaryCommentChars))
	}
	if note = strings.TrimSpace(note); note != "" {
		sb.WriteString("\n\n")
		sb.WriteString(note)
	}
	return sb.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// ResolveAnnotations is the agent's half of the review: each comment it
// answered while revising is marked resolved with its reply. Items are
// independent — one bad id must not cost the agent the replies it got right.
func (s *Service) ResolveAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.AnnotationResolution) (domain.AnnotationResolveResult, error) {
	result := domain.AnnotationResolveResult{Resolved: []domain.TaskDocumentAnnotation{}}
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return result, err
	}
	if s.annotations == nil {
		return result, errAnnotationsDisabled
	}
	now := time.Now().UTC()
	for _, item := range items {
		reply := strings.TrimSpace(item.Reply)
		fail := func(msg string) {
			result.Failed = append(result.Failed, domain.AnnotationResolveFailure{ID: item.ID.String(), Error: msg})
		}
		if reply == "" {
			fail("reply is required: say in one line what you changed, or why nothing needed to change")
			continue
		}
		if n := utf8.RuneCountInString(reply); n > domain.MaxAnnotationReplyChars {
			fail(fmt.Sprintf("reply is %d characters, the limit is %d", n, domain.MaxAnnotationReplyChars))
			continue
		}
		a, err := s.annotations.Get(ctx, taskID, item.ID)
		if err != nil {
			fail(err.Error())
			continue
		}
		if a.Status == domain.AnnotationStatusResolved {
			fail("already resolved")
			continue
		}
		a.Status = domain.AnnotationStatusResolved
		a.Reply = reply
		a.ResolvedAt = &now
		updated, err := s.annotations.Update(ctx, a)
		if err != nil {
			fail(err.Error())
			continue
		}
		result.Resolved = append(result.Resolved, updated)
	}
	return result, nil
}
