package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type reviewAnnotationReader interface {
	ListAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, documentID *uuid.UUID) ([]domain.TaskDocumentAnnotation, error)
	ListDocuments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskDocument, error)
}

const (
	reviewAnnotationsLimit = 20000
	reviewQuoteLimit       = 600
)

// reviewAnnotationsMessage hands a revision run every comment the human sent
// back with the review, verbatim. revisionCommentsMessage keeps only the last
// five task comments and cuts each at 2000 bytes, which is fine for a
// conversation and wrong for a review: comment 6 of 9 is exactly as much of
// the verdict as comment 1. So this message is capped as a whole, and if the
// cap is reached it says how many were left out and where to read them —
// a silently shortened list reads to the agent as the complete one.
func (r *Runner) reviewAnnotationsMessage(ctx context.Context, job RunJob) string {
	reader, ok := r.taskUpdater.(reviewAnnotationReader)
	if !ok {
		return ""
	}
	all, err := reader.ListAnnotations(ctx, job.RepositoryID, job.Task.ID, nil)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review annotations for run context failed")
		return ""
	}
	var submitted []domain.TaskDocumentAnnotation
	for _, a := range all {
		if a.Status == domain.AnnotationStatusSubmitted {
			submitted = append(submitted, a)
		}
	}
	if len(submitted) == 0 {
		return ""
	}
	titles := map[uuid.UUID]string{}
	if docs, derr := reader.ListDocuments(ctx, job.RepositoryID, job.Task.ID); derr == nil {
		for _, d := range docs {
			titles[d.ID] = d.Title
		}
	}
	return renderReviewAnnotations(job.Task, submitted, titles)
}

func renderReviewAnnotations(task domain.BoardTask, items []domain.TaskDocumentAnnotation, titles map[uuid.UUID]string) string {
	taskRef := task.Key
	if taskRef == "" {
		taskRef = task.ID.String()
	}
	docIDs := make([]string, 0, 1)
	seen := map[uuid.UUID]bool{}
	for _, a := range items {
		if !seen[a.DocumentID] {
			seen[a.DocumentID] = true
			docIDs = append(docIDs, a.DocumentID.String())
		}
	}

	var sb strings.Builder
	sb.WriteString(renderReviewAnnotationsHeader(len(items), docIDs, taskRef))
	shown := 0
	for i, a := range items {
		entry := reviewEntry(i+1, a, titles[a.DocumentID])
		if sb.Len()+len(entry) > reviewAnnotationsLimit && shown > 0 {
			break
		}
		sb.WriteString(entry)
		shown++
	}
	if omitted := len(items) - shown; omitted > 0 {
		sb.WriteString(reviewAnnotationsOmittedNote(omitted, taskRef))
	}
	return sb.String()
}

// renderReviewAnnotationsHeader tells the revision run what this message is
// (the revision request itself, not a side note) and the exact sequence to
// follow: read the current document, revise it in place, then resolve every
// comment below — before any per-comment entry is appended.
func renderReviewAnnotationsHeader(count int, docIDs []string, taskRef string) string {
	rendered := reviewAnnotationsHeaderKey.Render(reviewAnnotationsHeaderData{
		Count:   count,
		DocIDs:  strings.Join(docIDs, ", "),
		TaskRef: taskRef,
	})
	return rendered + "\n"
}

// reviewAnnotationsOmittedNote tells the run how many comments the byte cap
// left out and how to fetch the rest — a silently shortened list would read
// as the complete one.
func reviewAnnotationsOmittedNote(omitted int, taskRef string) string {
	return reviewAnnotationsOmittedKey.Render(reviewAnnotationsOmittedData{
		Omitted: omitted,
		Limit:   reviewAnnotationsLimit,
		TaskRef: taskRef,
	})
}

func reviewEntry(n int, a domain.TaskDocumentAnnotation, title string) string {
	quote := strings.Join(strings.Fields(a.Quote), " ")
	if len(quote) > reviewQuoteLimit {
		quote = truncateHead(quote, reviewQuoteLimit) + "…"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n### Comment %d — id %s", n, a.ID)
	if title != "" {
		fmt.Fprintf(&sb, " on %q", title)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Passage: \"%s\"\n", quote)
	fmt.Fprintf(&sb, "Comment: %s\n", strings.TrimSpace(a.Body))
	return sb.String()
}
