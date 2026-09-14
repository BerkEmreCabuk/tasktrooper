package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listTaskDocumentsToolName = "list_task_documents"

// listDocumentsTool is the read half of task documents, and it exists for
// exactly the reason listCommentsTool does: the kit shipped add_task_document
// with no counterpart.
//
// That gap became load-bearing the moment an analysis stopped committing its
// spec to the repository. The architect attaches the spec and the plan to the
// analiz task; the implementation task it then opens names that analysis with a
// derived_from relation; and the developer picking the task up was told a
// document exists somewhere with no call that could return it. The run context
// puts the documents in front of that developer anyway — but a run that needs to
// re-read a long plan halfway through, or to look at an analysis it was not
// handed, needs a tool, and a model reaching for one and finding only
// add_task_document writes a document instead of reading one.
type listDocumentsTool struct {
	kit *ToolKit
}

type listDocumentsArgs struct {
	TaskID string `json:"task_id"`
}

// DocumentLister is the read side of TaskManager's documents, kept separate for
// the same reason CommentLister is: a build whose task service predates it
// registers no tool rather than failing to compile.
type DocumentLister interface {
	ListDocuments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskDocument, error)
}

func newListDocumentsTool(kit *ToolKit) port.ToolExecutor {
	return &listDocumentsTool{kit: kit}
}

func (t *listDocumentsTool) Name() string { return listTaskDocumentsToolName }

func (t *listDocumentsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listTaskDocumentsToolName,
			Description: "READ the documents attached to a board task, full content, in order. " +
				"This is where an analysis lives: the spec and the implementation plan an analiz task produced are documents on THAT task, never files in the repository, so pass the analiz task's key (e.g. \"A-12\") to read them. " +
				"Your own task's `relations` name it with relation_type \"derived_from\" — and the same documents are already in your run context, so use this to re-read a long plan or to look at an analysis you were not handed. " +
				"This tool only reads; add_task_document is what writes one.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *listDocumentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listDocumentsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listTaskDocumentsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	lister, ok := t.kit.Tasks.(DocumentLister)
	if !ok {
		return toolError(listTaskDocumentsToolName, "this build cannot read task documents")
	}
	docs, err := lister.ListDocuments(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	return toolJSON(listTaskDocumentsToolName, map[string]interface{}{
		"task_id":   taskID.String(),
		"count":     len(docs),
		"documents": docs,
	})
}
