package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const updateTaskDocumentToolName = "update_task_document"

// DocumentUpdater is the write-back half of task documents, kept as its own
// optional interface for the same reason DocumentLister is: a build whose task
// service predates it registers no tool rather than failing to compile.
type DocumentUpdater interface {
	UpdateDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.UpdateTaskDocumentRequest) (domain.TaskDocument, error)
}

// updateDocumentTool exists because a task's spec is a living document, and the
// kit only knew how to append. Asked for a change to a plan it had already
// written, a model had exactly one call available — add_task_document — so the
// card accumulated "Spec", "Spec v2", "Revised spec", each one a fork of the
// last, and the developer picking the task up had no way to tell which one was
// current. Revising in place is the default the board wants; adding a second
// document is for a genuinely second document.
type updateDocumentTool struct {
	kit *ToolKit
}

type updateDocumentArgs struct {
	TaskID     string  `json:"task_id"`
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	NewTitle   *string `json:"new_title"`
	Content    *string `json:"content"`
}

func newUpdateDocumentTool(kit *ToolKit) port.ToolExecutor {
	return &updateDocumentTool{kit: kit}
}

func (t *updateDocumentTool) Name() string { return updateTaskDocumentToolName }

func (t *updateDocumentTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: updateTaskDocumentToolName,
			Description: "REWRITE a document that already exists on a board task, in place. " +
				"This is the tool for every revision of a spec, a plan or a report already attached to the task: whoever reads that task must find one current document, not a pile of near-duplicates, so revise instead of adding a \"v2\". " +
				"Identify the document by document_id or by its exact title; on a task that has exactly one document both may be omitted. " +
				"`content` REPLACES the whole body — read it first with list_task_documents and send the full new text, not a fragment.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"document_id": map[string]interface{}{
						"type":        "string",
						"description": "UUID of the document to rewrite (from list_task_documents). Optional if `title` is given, or if the task has exactly one document.",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Title of the document to rewrite, matched exactly (case-insensitive). Ignored when document_id is given.",
					},
					"new_title": map[string]interface{}{
						"type":        "string",
						"description": "Optional new title. Omit to keep the current one.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The complete new markdown body. Replaces the existing content entirely.",
					},
				},
			},
		},
	}
}

func (t *updateDocumentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args updateDocumentArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(updateTaskDocumentToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	if args.NewTitle == nil && args.Content == nil {
		return toolError(updateTaskDocumentToolName, "nothing to update: pass content, new_title, or both")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(updateTaskDocumentToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(updateTaskDocumentToolName, err.Error())
	}
	// An analysis document is subject to the same grounding gate whether it is
	// written for the first time or rewritten — otherwise the gate is one tool
	// call away from being bypassed.
	if task, ok := t.kit.findTask(ctx, taskID); ok && t.kit.requiresRepoGrounding(ctx, task.TaskType) {
		if msg := ungroundedAnalysisReason(ctx); msg != "" {
			return toolError(updateTaskDocumentToolName, msg)
		}
	}
	updater, ok := t.kit.Tasks.(DocumentUpdater)
	if !ok {
		return toolError(updateTaskDocumentToolName, "this build cannot update task documents")
	}
	doc, err := t.kit.findDocument(ctx, repositoryID, taskID, args.DocumentID, args.Title)
	if err != nil {
		return toolError(updateTaskDocumentToolName, err.Error())
	}
	req := domain.UpdateTaskDocumentRequest{Content: args.Content}
	if args.NewTitle != nil {
		trimmed := strings.TrimSpace(*args.NewTitle)
		if trimmed == "" {
			return toolError(updateTaskDocumentToolName, "new_title cannot be empty")
		}
		req.Title = &trimmed
	}
	updated, err := updater.UpdateDocument(ctx, repositoryID, taskID, doc.ID, req)
	if err != nil {
		return toolError(updateTaskDocumentToolName, err.Error())
	}
	return toolJSON(updateTaskDocumentToolName, documentResult(updated, ""))
}

// documentResult keeps the document's own fields at the top level of the tool
// result and hangs the extra flags off the side. The session ledger reads `id`
// and `title` straight off a board tool's result, so nesting the document under
// a "document" key would have cost every rewrite its ledger entry — the next
// turn would then have no record that the document was touched at all.
func documentResult(doc domain.TaskDocument, note string) map[string]interface{} {
	out := map[string]interface{}{}
	if raw, err := json.Marshal(doc); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	out["updated"] = true
	if note != "" {
		out["note"] = note
	}
	return out
}

// findDocument resolves the "which document" half of an update: an explicit id,
// an exact title, or — on a task carrying a single document — that one. Shared
// with add_task_document, which uses the title lookup to turn a repeat write of
// the same title into a rewrite instead of a duplicate.
func (kit *ToolKit) findDocument(ctx context.Context, repositoryID, taskID uuid.UUID, docID, title string) (domain.TaskDocument, error) {
	lister, ok := kit.Tasks.(DocumentLister)
	if !ok {
		return domain.TaskDocument{}, fmt.Errorf("this build cannot read task documents")
	}
	docs, err := lister.ListDocuments(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskDocument{}, err
	}
	if len(docs) == 0 {
		return domain.TaskDocument{}, fmt.Errorf("this task has no documents yet — use add_task_document to write the first one")
	}
	if id := strings.TrimSpace(docID); id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return domain.TaskDocument{}, fmt.Errorf("document_id is not a UUID: %s", id)
		}
		for _, doc := range docs {
			if doc.ID == parsed {
				return doc, nil
			}
		}
		return domain.TaskDocument{}, fmt.Errorf("no document %s on this task", id)
	}
	if wanted := strings.TrimSpace(title); wanted != "" {
		for _, doc := range docs {
			if strings.EqualFold(strings.TrimSpace(doc.Title), wanted) {
				return doc, nil
			}
		}
		return domain.TaskDocument{}, fmt.Errorf("no document titled %q on this task (titles: %s)", wanted, documentTitles(docs))
	}
	if len(docs) == 1 {
		return docs[0], nil
	}
	return domain.TaskDocument{}, fmt.Errorf("this task has %d documents — pass document_id or title (titles: %s)", len(docs), documentTitles(docs))
}

func documentTitles(docs []domain.TaskDocument) string {
	titles := make([]string, 0, len(docs))
	for _, doc := range docs {
		titles = append(titles, strconv.Quote(doc.Title))
	}
	return strings.Join(titles, ", ")
}
