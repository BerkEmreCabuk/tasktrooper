package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	listDocumentAnnotationsToolName    = "list_document_annotations"
	resolveDocumentAnnotationsToolName = "resolve_document_annotations"
)

// AnnotationManager is the agent's side of a human's passage-level review of
// a task document, kept optional for the reason DocumentLister is.
type AnnotationManager interface {
	ListAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, documentID *uuid.UUID) ([]domain.TaskDocumentAnnotation, error)
	ResolveAnnotations(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.AnnotationResolution) (domain.AnnotationResolveResult, error)
}

type annotationView struct {
	ID            string     `json:"id"`
	DocumentID    string     `json:"document_id"`
	DocumentTitle string     `json:"document_title"`
	Quote         string     `json:"quote"`
	Prefix        string     `json:"prefix,omitempty"`
	Suffix        string     `json:"suffix,omitempty"`
	Comment       string     `json:"comment"`
	Status        string     `json:"status"`
	Reply         string     `json:"reply,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	SubmittedAt   *time.Time `json:"submitted_at,omitempty"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

func (kit *ToolKit) annotationViews(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.TaskDocumentAnnotation) []annotationView {
	titles := map[uuid.UUID]string{}
	if lister, ok := kit.Tasks.(DocumentLister); ok {
		if docs, err := lister.ListDocuments(ctx, repositoryID, taskID); err == nil {
			for _, d := range docs {
				titles[d.ID] = d.Title
			}
		}
	}
	out := make([]annotationView, 0, len(items))
	for _, a := range items {
		out = append(out, annotationView{
			ID:            a.ID.String(),
			DocumentID:    a.DocumentID.String(),
			DocumentTitle: titles[a.DocumentID],
			Quote:         a.Quote,
			Prefix:        a.Prefix,
			Suffix:        a.Suffix,
			Comment:       a.Body,
			Status:        string(a.Status),
			Reply:         a.Reply,
			CreatedAt:     a.CreatedAt,
			SubmittedAt:   a.SubmittedAt,
			ResolvedAt:    a.ResolvedAt,
		})
	}
	return out
}

type listAnnotationsTool struct{ kit *ToolKit }

type listAnnotationsArgs struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func newListAnnotationsTool(kit *ToolKit) port.ToolExecutor {
	return &listAnnotationsTool{kit: kit}
}

func (t *listAnnotationsTool) Name() string { return listDocumentAnnotationsToolName }

func (t *listAnnotationsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listDocumentAnnotationsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"status": map[string]interface{}{
						"type": "string",
						"enum": []string{string(domain.AnnotationStatusOpen), string(domain.AnnotationStatusSubmitted), string(domain.AnnotationStatusResolved)},
					},
				},
			},
		},
	}
}

func (t *listAnnotationsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listAnnotationsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listDocumentAnnotationsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	status := domain.AnnotationStatus(strings.TrimSpace(args.Status))
	if status != "" && !domain.ValidAnnotationStatus(status) {
		return toolError(listDocumentAnnotationsToolName, fmt.Sprintf("unknown status %q (open, submitted or resolved)", args.Status))
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(listDocumentAnnotationsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(listDocumentAnnotationsToolName, err.Error())
	}
	manager, ok := t.kit.Tasks.(AnnotationManager)
	if !ok {
		return toolError(listDocumentAnnotationsToolName, "this build cannot read document annotations")
	}
	all, err := manager.ListAnnotations(ctx, repositoryID, taskID, nil)
	if err != nil {
		return toolError(listDocumentAnnotationsToolName, err.Error())
	}
	items := make([]domain.TaskDocumentAnnotation, 0, len(all))
	for _, a := range all {
		if status == "" || a.Status == status {
			items = append(items, a)
		}
	}
	return toolJSON(listDocumentAnnotationsToolName, map[string]interface{}{
		"task_id":     taskID.String(),
		"count":       len(items),
		"annotations": t.kit.annotationViews(ctx, repositoryID, taskID, items),
	})
}

type resolveAnnotationsTool struct{ kit *ToolKit }

type resolveAnnotationsArgs struct {
	TaskID string `json:"task_id"`
	Items  []struct {
		ID    string `json:"id"`
		Reply string `json:"reply"`
	} `json:"items"`
}

func newResolveAnnotationsTool(kit *ToolKit) port.ToolExecutor {
	return &resolveAnnotationsTool{kit: kit}
}

func (t *resolveAnnotationsTool) Name() string { return resolveDocumentAnnotationsToolName }

func (t *resolveAnnotationsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: resolveDocumentAnnotationsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"items": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]interface{}{
								"id":    map[string]interface{}{"type": "string"},
								"reply": map[string]interface{}{"type": "string"},
							},
							"required": []string{"id", "reply"},
						},
					},
				},
				"required": []string{"items"},
			},
		},
	}
}

func (t *resolveAnnotationsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args resolveAnnotationsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(resolveDocumentAnnotationsToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.Items) == 0 {
		return toolError(resolveDocumentAnnotationsToolName, "items is required: one {id, reply} per comment you addressed")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(resolveDocumentAnnotationsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(resolveDocumentAnnotationsToolName, err.Error())
	}
	manager, ok := t.kit.Tasks.(AnnotationManager)
	if !ok {
		return toolError(resolveDocumentAnnotationsToolName, "this build cannot resolve document annotations")
	}
	items := make([]domain.AnnotationResolution, 0, len(args.Items))
	var invalid []domain.AnnotationResolveFailure
	for _, item := range args.Items {
		id, perr := uuid.Parse(strings.TrimSpace(item.ID))
		if perr != nil {
			invalid = append(invalid, domain.AnnotationResolveFailure{ID: item.ID, Error: "not a UUID"})
			continue
		}
		items = append(items, domain.AnnotationResolution{ID: id, Reply: item.Reply})
	}
	result, err := manager.ResolveAnnotations(ctx, repositoryID, taskID, items)
	if err != nil {
		return toolError(resolveDocumentAnnotationsToolName, err.Error())
	}
	failed := append(invalid, result.Failed...)
	if len(result.Resolved) == 0 {
		raw, _ := json.Marshal(failed)
		return toolError(resolveDocumentAnnotationsToolName, "no comment was resolved: "+string(raw))
	}
	out := map[string]interface{}{
		"task_id":     taskID.String(),
		"resolved":    len(result.Resolved),
		"annotations": t.kit.annotationViews(ctx, repositoryID, taskID, result.Resolved),
	}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	return toolJSON(resolveDocumentAnnotationsToolName, out)
}
